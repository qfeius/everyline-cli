"use strict";

const { createHash } = require("node:crypto");
const { cpSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, renameSync, rmSync } = require("node:fs");
const { dirname, isAbsolute, join, relative, resolve, sep } = require("node:path");

/**
 * buildDoubaoSkillPlans 定位豆包原生技能目录并生成普通文件夹同步计划。
 * 入参：packageRoot（string）为包根；skillNames（string[]）为技能名；environment（object）为环境；platform（string）为平台；userHome（string）为用户目录。
 * 返回值：object[]，未发现宿主或明确跳过时为空；显式目录适用于其他平台和自定义工作区。
 */
function buildDoubaoSkillPlans(packageRoot, skillNames, environment, platform, userHome) {
  if (environment.EVERYLINE_SKIP_DOUBAO_SKILL_INSTALL === "1") return [];
  const configured = String(environment.EVERYLINE_DOUBAO_SKILLS_DIR || "").trim();
  if (configured && !isAbsolute(configured)) throw new Error("EVERYLINE_DOUBAO_SKILLS_DIR 必须是豆包实际使用的绝对目录");
  // 路径来自 macOS 豆包工作自带的 skill-creator-for-work；未安装宿主时不创建默认目录。
  const detected = platform === "darwin"
    ? join(userHome, "Library", "Application Support", "DoubaoWork", "Default", ".doubaowork", "agent_mode", "workspace", ".user_skills") : "";
  const skillRoot = configured || (detected && existsSync(detected) ? detected : "");
  if (!skillRoot) return [];
  if (existsSync(skillRoot) && (!lstatSync(skillRoot).isDirectory() || lstatSync(skillRoot).isSymbolicLink())) {
    throw new Error(`豆包 Skill 根目录不是普通目录: ${skillRoot}`);
  }
  return skillNames.map((name) => ({
    name, hostName: "豆包", hostKey: "doubao", installMode: "directory",
    source: resolve(packageRoot, "skills", name), target: resolve(skillRoot, name),
  }));
}

/**
 * skillDigest 对整个技能目录生成稳定摘要，拒绝符号链接和特殊文件。
 * 入参：directory（string）为技能根目录。
 * 返回值：string，包含路径、内容和可执行权限的 SHA-256 摘要。
 */
function skillDigest(directory) {
  const digest = createHash("sha256");
  const visit = (path, prefix) => {
    const state = lstatSync(path);
    if (state.isSymbolicLink() || (!state.isDirectory() && !state.isFile())) throw new Error(`豆包 Skill 含符号链接或特殊文件: ${path}`);
    digest.update(JSON.stringify([prefix, state.isDirectory() ? "directory" : "file", state.mode & 0o111]));
    if (state.isDirectory()) {
      for (const name of readdirSync(path).sort()) visit(join(path, name), `${prefix}/${name}`);
    } else digest.update(readFileSync(path));
  };
  visit(directory, "");
  return digest.digest("hex");
}

/**
 * inspectDoubaoSkillRegistration 在任何宿主写入前校验技能身份、路径和旧副本。
 * 入参：plan（object）为文件夹同步计划。
 * 返回值：object，带 created/existing/updated 状态以及用于并发检查的摘要。
 */
function inspectDoubaoSkillRegistration(plan) {
  const nested = (parent, child) => {
    const path = relative(parent, child);
    return path === "" || (!isAbsolute(path) && path !== ".." && !path.startsWith(`..${sep}`));
  };
  if (nested(plan.source, plan.target) || nested(plan.target, plan.source)) throw new Error(`豆包 Skill 来源与目标不可互相包含: ${plan.target}`);
  let previousState;
  try { previousState = lstatSync(plan.target); } catch (error) { if (error.code !== "ENOENT") throw error; }
  const sourceDigest = skillDigest(plan.source);
  if (!previousState) return { ...plan, sourceDigest, status: "created" };
  // 只接管声明正式名称的 EveryLine 技能；用户同名目录未声明该身份时保留并报告冲突。
  if (!previousState.isDirectory() || previousState.isSymbolicLink()) throw new Error(`豆包 Skill 目标不是普通技能目录: ${plan.target}`);
  const file = join(plan.target, "SKILL.md");
  const text = existsSync(file) ? readFileSync(file, "utf8") : "";
  const header = text.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/)?.[1] || "";
  const names = [...header.matchAll(/^name:[ \t]*(?:"([^"\r\n]+)"|'([^'\r\n]+)'|([^\s#]+))[ \t]*(?:#.*)?\r?$/gm)];
  if (names.length !== 1 || (names[0][1] || names[0][2] || names[0][3]) !== plan.name) {
    throw new Error(`豆包 Skill 目标未声明对应 EveryLine 技能，请先保留或移走原目录: ${plan.target}`);
  }
  const previousDigest = skillDigest(plan.target);
  return { ...plan, sourceDigest, previousDigest, status: previousDigest === sourceDigest ? "existing" : "updated" };
}

/**
 * installDoubaoSkill 在扫描目录外准备完整新副本，再替换目标并保留旧副本。
 * 入参：registration（object）为已预检的同步计划。
 * 返回值：void，将 stagingPath、backupPath 写回计划供统一事务回滚或收尾。
 */
function installDoubaoSkill(registration) {
  const root = dirname(registration.target);
  mkdirSync(root, { recursive: true });
  // 备份与临时文件均位于扫描目录外，避免豆包同时加载新旧同名技能。
  registration.stagingPath = mkdtempSync(join(dirname(root), ".everyline-skill-update-"));
  const staged = join(registration.stagingPath, "new");
  cpSync(registration.source, staged, { recursive: true, errorOnExist: true, force: false });
  if (skillDigest(staged) !== registration.sourceDigest) throw new Error(`Skill 来源在安装期间变化: ${registration.source}`);
  if (registration.status === "updated") {
    if (skillDigest(registration.target) !== registration.previousDigest) throw new Error(`Skill 目标在安装期间变化: ${registration.target}`);
    const backup = join(registration.stagingPath, "previous");
    renameSync(registration.target, backup);
    registration.backupPath = backup;
  } else {
    // lstat 同样识别悬空链接；预检后出现的任何目标都视为冲突。
    try {
      lstatSync(registration.target);
      throw new Error(`Skill 目标在安装期间出现: ${registration.target}`);
    } catch (error) { if (error.code !== "ENOENT") throw error; }
  }
  renameSync(staged, registration.target);
  registration.directoryInstalled = true;
}

/**
 * finishDoubaoSkill 收尾或撤回本次文件夹替换，成功更新时保留旧副本。
 * 入参：registration（object）为同步计划；rollback（boolean）表示撤回写入。
 * 返回值：void；目标发生并发修改时保留现场并抛出错误。
 */
function finishDoubaoSkill(registration, rollback = false) {
  if (!registration.stagingPath) return;
  if (rollback) {
    if (registration.directoryInstalled) {
      if (skillDigest(registration.target) !== registration.sourceDigest) throw new Error(`Skill 已被并发修改，备份保留于 ${registration.stagingPath}`);
      rmSync(registration.target, { recursive: true });
    }
    if (registration.backupPath) renameSync(registration.backupPath, registration.target);
  }
  if (rollback || !registration.backupPath) rmSync(registration.stagingPath, { recursive: true, force: true });
}

module.exports = { buildDoubaoSkillPlans, inspectDoubaoSkillRegistration, installDoubaoSkill, finishDoubaoSkill };
