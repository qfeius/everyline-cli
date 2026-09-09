#!/usr/bin/env node

const { randomUUID } = require("node:crypto");
const { execFileSync } = require("node:child_process");
const {
  chmodSync,
  existsSync,
  lstatSync,
  mkdirSync,
  readlinkSync,
  readFileSync,
  realpathSync,
  symlinkSync,
  unlinkSync,
} = require("node:fs");
const { homedir } = require("node:os");
const { basename, dirname, join, resolve } = require("node:path");
const { resolvePlatformTarget } = require("./platform");
const { buildDoubaoSkillPlans, inspectDoubaoSkillRegistration, installDoubaoSkill, finishDoubaoSkill } = require("./doubao-skills");

// skillNames 是同一份 npm 包向 Codex、WorkBuddy 和豆包发布的两项职责分离 Skill。
const skillNames = ["everyline-review", "everyline-review-config"];
const deprecatedSkillNames = ["everyline-shared"];
const installStateSchema = "everyline.install-state.v1";
// 安装提示与 Agent 事件共用文案；先说明可协助授权，用户要求登录后再选择 user/app。
const firstInstallMessage = "EveryLine CLI 已安装完成。目前支持合同审查，以及审查清单、规则和规则分组配置。使用前需要先完成账号授权，我现在可以为你打开授权页面或生成授权链接。";
const updateMessage = "EveryLine CLI 已更新完成。目前支持合同审查，以及审查清单、规则和规则分组配置。";
const authorizationRequiredMessage = "使用前需要先完成账号授权，我现在可以为你打开授权页面或生成授权链接。";
const authorizedMessage = "当前已存在生效授权，可直接调用cli能力；";

/**
 * shouldInstallCodexSkill 判断本次 npm 生命周期是否应登记 Codex Skill。
 * 入参：environment（NodeJS.ProcessEnv），当前进程环境变量。
 * 返回值：boolean，仅全局安装且未显式跳过时为 true。
 */
function shouldInstallCodexSkill(environment) {
  return environment.npm_config_global === "true" && environment.EVERYLINE_SKIP_SKILL_INSTALL !== "1";
}

/**
 * shouldInstallWorkBuddySkills 判断本次 npm 生命周期是否应登记 WorkBuddy Skills。
 * 入参：environment（NodeJS.ProcessEnv），当前进程环境变量。
 * 返回值：boolean，仅全局安装且未显式跳过全部 Skill 或 WorkBuddy Skill 时为 true。
 */
function shouldInstallWorkBuddySkills(environment) {
  return shouldInstallCodexSkill(environment) && environment.EVERYLINE_SKIP_WORKBUDDY_SKILL_INSTALL !== "1";
}

/**
 * isEverylineSkillSource 根据包内路径和 npm manifest 确认旧链接属于 EveryLine，避免接管用户同名 Skill。
 * 入参：source（string）为旧链接解析后的来源路径。
 * 返回值：boolean，仅当前或废弃的已知 Skill 且所属包声明正确的 CLI 入口时为 true；读取权限等异常向外抛出。
 */
function isEverylineSkillSource(source) {
  if ((!skillNames.includes(basename(source)) && !deprecatedSkillNames.includes(basename(source))) || basename(dirname(source)) !== "skills") {
    return false;
  }
  try {
    const manifest = JSON.parse(readFileSync(join(dirname(dirname(source)), "package.json"), "utf8"));
    return ["everyline-cli", "@qfeius/everyline-cli"].includes(manifest?.name) && manifest.bin?.["everyline-cli"] === "scripts/run.js";
  } catch (error) {
    if (error.code === "ENOENT" || error.code === "ENOTDIR" || error instanceof SyntaxError) {
      return false;
    }
    throw error;
  }
}

/**
 * inspectAgentSkillRegistration 只读校验 Skill 来源和目标，并生成后续登记计划。
 * 入参：source（string）为包内 Skill 路径；target（string）为宿主目标路径；hostName（string）为宿主名；metadata（object）为需透传的名称与分组。
 * 返回值：object，包含规范化来源、目标、预期状态；跨安装来源更新时保留原链接供失败回滚。
 */
function inspectAgentSkillRegistration(source, target, hostName = "Agent", metadata = {}) {
  const resolvedSource = resolve(source);
  if (!existsSync(join(resolvedSource, "SKILL.md"))) {
    throw new Error(`npm 包缺少 EveryLine Skill: ${resolvedSource}`);
  }

  // 先检查目标本身而非其指向内容，确保悬空链接也不会被静默覆盖。
  let targetState;
  try {
    targetState = lstatSync(target);
  } catch (error) {
    if (error.code !== "ENOENT") {
      throw error;
    }
  }

  if (targetState) {
    if (targetState.isSymbolicLink()) {
      const previousLink = readlinkSync(target);
      let previousSource = resolve(dirname(target), previousLink);
      try {
        previousSource = realpathSync(target);
      } catch (error) {
        // 旧包可能已移除 Skill 文件；只有仍可验证的 manifest 才允许迁移悬空链接。
        if (error.code !== "ENOENT" && error.code !== "ENOTDIR") {
          throw error;
        }
      }
      // realpath 同时兼容 POSIX 符号链接和 Windows 目录联接的路径表示差异。
      if (previousSource === realpathSync(resolvedSource)) {
        return { ...metadata, source: resolvedSource, target, hostName, status: "existing" };
      }
      if (basename(previousSource) === basename(resolvedSource) && isEverylineSkillSource(previousSource)) {
        return { ...metadata, source: resolvedSource, target, hostName, status: "updated", previousLink };
      }
    }
    throw new Error(`${hostName} Skill 目标已存在且未确认属于 EveryLine；请将需保留的内容移到 Skill 扫描目录之外再重试，不要仅在原目录内添加 .bak 后缀: ${target}`);
  }

  return { ...metadata, source: resolvedSource, target, hostName, status: "created" };
}

/**
 * registerAgentSkillPlans 预检全部宿主，登记当前技能并迁出旧入口，最终状态提交失败时撤回本轮变更。
 * 入参：plans（Array<object>）为登记或迁出计划；platform（string）为 Node 平台名；beforeRegister（Function，可选）在预检后写入首次安装意图；commit（Function，可选）接收预检结果，作为事务的最终状态提交。
 * 返回值：Array<object>，保留计划元数据及 created/existing/updated/removed/skipped 状态。
 */
function registerAgentSkillPlans(plans, platform = process.platform, beforeRegister, commit) {
  // 全量预检发生在任何写入前，常见的同名目录冲突不会留下半套登记结果。
  const inspected = plans.map((plan) => {
    if (plan.installMode === "deprecated-link") return plan;
    if (plan.installMode === "directory") return inspectDoubaoSkillRegistration(plan);
    return inspectAgentSkillRegistration(plan.source, plan.target, plan.hostName, plan);
  });
  // 首次安装意图必须先可靠落盘；预检冲突不会创建门禁，登记中断也不会丢失待授权状态。
  beforeRegister?.(inspected);
  const changed = [];
  try {
    for (const registration of inspected) {
      if (registration.status === "existing" || registration.status === "skipped") {
        continue;
      }
      if (registration.installMode === "directory") {
        changed.push(registration);
        installDoubaoSkill(registration);
        continue;
      }
      mkdirSync(dirname(registration.target), { recursive: true });
      if (registration.status === "updated" || registration.status === "removed") {
        // 写入前再次核对旧链接，避免预检之后出现的用户目录或其他来源被覆盖。
        if (!lstatSync(registration.target).isSymbolicLink() || readlinkSync(registration.target) !== registration.previousLink) {
          throw new Error(`Skill 目标在安装期间发生变化，请重试: ${registration.target}`);
        }
        unlinkSync(registration.target);
        // 在新链接创建前记入回滚列表，即使 symlink 失败也能恢复原链接。
        changed.push(registration);
        if (registration.status === "removed") continue;
      }
      symlinkSync(registration.source, registration.target, platform === "win32" ? "junction" : "dir");
      if (registration.status === "created") {
        changed.push(registration);
      }
    }
    for (const registration of changed) {
      if (registration.installMode === "directory") finishDoubaoSkill(registration);
    }
    // 旧目录备份与链接撤销信息仍在；状态提交是最后一个失败点，失败后统一恢复各宿主。
    commit?.(inspected);
    return inspected;
  } catch (error) {
    // 只撤回本轮仍指向新来源的链接；旧目标保留在内存中，不在扫描目录里创建 .bak 副本。
    const rollbackErrors = [];
    for (const registration of changed.reverse()) {
      try {
        if (registration.installMode === "directory") {
          finishDoubaoSkill(registration, true);
          continue;
        }
        let targetState;
        try {
          targetState = lstatSync(registration.target);
        } catch (error) {
          if (error.code !== "ENOENT") {
            throw error;
          }
        }
        if (targetState) {
          // 旧链接迁出后出现的新目标由其他进程拥有，回滚不覆盖它。
          if (registration.status === "removed") continue;
          if (!targetState.isSymbolicLink() || realpathSync(registration.target) !== realpathSync(registration.source)) {
            continue;
          }
          unlinkSync(registration.target);
        }
        if (registration.status === "updated" || registration.status === "removed") {
          symlinkSync(registration.previousLink, registration.target, platform === "win32" ? "junction" : "dir");
        }
      } catch (rollbackError) {
        // 继续恢复其他宿主，同时保留未恢复目录的备份位置供用户处理。
        rollbackErrors.push(rollbackError.message);
      }
    }
    if (rollbackErrors.length > 0) {
      throw new Error(`${error.message}；部分 Skill 回滚失败: ${rollbackErrors.join("；")}`, { cause: error });
    }
    throw error;
  }
}

/**
 * registerAgentSkill 将 npm 包内单项 Skill 以目录链接登记到指定 Agent 宿主目录。
 * 入参：source（string）为包内 Skill 绝对路径；target（string）为宿主 Skill 目标路径；platform（string）为 Node 平台名；hostName（string）为错误提示中的宿主名。
 * 返回值："created" | "existing" | "updated"，分别表示新建、已存在同源链接或跨安装来源更新。
 */
function registerAgentSkill(source, target, platform = process.platform, hostName = "Agent") {
  const [registration] = registerAgentSkillPlans([{ source, target, hostName }], platform);
  return registration.status;
}

/**
 * buildSkillSetPlans 构造一个宿主下两项职责分离 Skill 的无副作用登记计划。
 * 入参：packageRoot（string）为 npm 包根目录；skillRoot（string）为宿主 Skill 根目录；hostName（string）为宿主名；hostKey（string）为返回结果分组键。
 * 返回值：Array<object>，每项包含来源、目标、Skill 名称和宿主分组。
 */
function buildSkillSetPlans(packageRoot, skillRoot, hostName, hostKey = "") {
  return skillNames.map((name) => ({
    name,
    hostKey,
    hostName,
    source: join(packageRoot, "skills", name),
    target: join(skillRoot, name),
  }));
}

/**
 * registerCodexSkill 保留既有单项登记接口，兼容安装器调用方和发布测试。
 * 入参：source（string）为包内 Skill 绝对路径；target（string）为 Codex Skill 目标路径；platform（string）为 Node 平台名。
 * 返回值："created" | "existing" | "updated"，含义与 registerAgentSkill 一致。
 */
function registerCodexSkill(source, target, platform = process.platform) {
  return registerAgentSkill(source, target, platform, "Codex");
}

/**
 * registerSkillSet 将职责分离的两项 EveryLine Skill 登记到一个宿主根目录。
 * 入参：packageRoot（string）为 npm 包根目录；skillRoot（string）为宿主 Skill 根目录；platform（string）为 Node 平台名；hostName（string）为宿主名。
 * 返回值：Array<object>，每项包含 name、target 和 created/existing/updated 状态。
 */
function registerSkillSet(packageRoot, skillRoot, platform, hostName) {
  return registerAgentSkillPlans(buildSkillSetPlans(packageRoot, skillRoot, hostName), platform)
    .map(({ name, target, status }) => ({ name, target, status }));
}

/**
 * inspectDeprecatedSkillRegistrations 只读识别本包同源或可验证旧包中的废弃 Skill 链接。
 * 入参：packageRoot（string）为 npm 包根目录；skillRoot（string）为宿主 Skill 根目录。
 * 返回值：Array<object>，包含 name、target 和 previousLink，供旧安装识别及清理前核对；不包含用户目录或未确认来源。
 */
function inspectDeprecatedSkillRegistrations(packageRoot, skillRoot) {
  const registrations = [];
  for (const name of deprecatedSkillNames) {
    const target = join(skillRoot, name);
    let targetState;
    try {
      targetState = lstatSync(target);
    } catch (error) {
      if (error.code === "ENOENT") {
        continue;
      }
      throw error;
    }
    if (!targetState.isSymbolicLink()) {
      continue;
    }

    // 同源升级允许源目录已删除；跨 Node 目录必须额外验证旧包 manifest，不接管未知来源。
    const previousLink = readlinkSync(target);
    const linkedSource = resolve(dirname(target), previousLink);
    const expectedSource = resolve(packageRoot, "skills", name);
    if (linkedSource !== expectedSource && (basename(linkedSource) !== name || !isEverylineSkillSource(linkedSource))) {
      continue;
    }
    registrations.push({ name, target, previousLink });
  }
  return registrations;
}

/**
 * removeDeprecatedSkillRegistrations 清理已确认属于 EveryLine 的废弃链接，包括其他 Node/npm 前缀。
 * 入参：packageRoot（string）为本次包根目录；skillRoot（string）为宿主 Skill 根目录。
 * 返回值：string[]，为已移除的旧 Skill 名称；目标发生变化时抛出错误并保留新目标。
 */
function removeDeprecatedSkillRegistrations(packageRoot, skillRoot) {
  const registrations = inspectDeprecatedSkillRegistrations(packageRoot, skillRoot);
  for (const { target, previousLink } of registrations) {
    if (!lstatSync(target).isSymbolicLink() || readlinkSync(target) !== previousLink) {
      throw new Error(`Skill 目标在安装期间发生变化，请重试: ${target}`);
    }
    unlinkSync(target);
  }
  return registrations.map(({ name }) => name);
}

/**
 * resolveInstallStatePath 解析安装器与原生 CLI 共享的首次安装状态路径。
 * 入参：environment（NodeJS.ProcessEnv）为环境变量；userHome（string）为用户目录。
 * 返回值：string，为 install-state.json 的绝对路径。
 */
function resolveInstallStatePath(environment, userHome) {
  const configured = String(environment.EVERYLINE_CONFIG_DIR || "").trim();
  return join(configured ? resolve(userHome, configured) : join(userHome, ".everyline-cli"), "install-state.json");
}

/**
 * loadInstallState 读取并校验已有首次安装状态，文件缺失时返回 null。
 * 入参：statePath（string）为状态文件路径。
 * 返回值：object|null，为已校验状态或文件缺失。
 */
function loadInstallState(statePath) {
  if (!existsSync(statePath)) {
    return null;
  }
  const state = JSON.parse(readFileSync(statePath, "utf8"));
  if (state.schema !== installStateSchema || typeof state.eventId !== "string" || state.eventId.length === 0) {
    throw new Error(`EveryLine 首次安装状态格式错误: ${statePath}`);
  }
  return state;
}

/**
 * ensureFirstInstallState 通过包内原生 CLI 登记安装，和授权完成流程共用文件锁，避免覆盖最新状态。
 * 入参：packageRoot（string）为包根；environment（NodeJS.ProcessEnv）为环境；userHome（string）为用户目录；platform（string）为平台；registrations（Array<object>）为 Skill 登记结果；hadDeprecatedRegistrations（boolean，可选）为已确认的旧链接；nativeBinary（string，可选）为本次安装的平台二进制。
 * 返回值：object，包含状态路径和原生 CLI 在锁内计算的安装及更新状态；登记失败时抛出 Error。
 */
function ensureFirstInstallState(packageRoot, environment, userHome, platform, registrations, hadDeprecatedRegistrations = false, nativeBinary) {
  const statePath = resolveInstallStatePath(environment, userHome);
  const packageData = JSON.parse(readFileSync(join(packageRoot, "package.json"), "utf8"));
  const installedVersion = String(packageData.version || "");
  // existing 来自写入前的同源预检；旧链接的清理结果同样证明此前已安装，单项 created 不代表首次安装。
  const previouslyInstalled = hadDeprecatedRegistrations || registrations.some((registration) => registration.status === "existing" || registration.status === "updated");
  if (!existsSync(statePath) && !previouslyInstalled && !registrations.some((registration) => registration.status === "created")) {
    return { path: statePath, firstInstall: false, authorizationRequired: false, nextAction: "", updated: false };
  }
  const binary = nativeBinary || join(packageRoot, "bin", resolvePlatformTarget(platform, process.arch), platform === "win32" ? "everyline-cli.exe" : "everyline-cli");
  // 直接运行包内二进制，不依赖 PATH 中可能仍为旧版的 CLI；绝对配置目录保证父子进程写入同一文件。
  const output = execFileSync(binary, [
    "_record-install", "--installed-version", installedVersion, "--event-id", randomUUID(),
    `--previously-installed=${previouslyInstalled}`,
  ], {
    encoding: "utf8",
    env: { ...process.env, ...environment, EVERYLINE_CONFIG_DIR: dirname(statePath) },
  });
  return { path: statePath, ...JSON.parse(output) };
}

/**
 * installPackage 保留首次安装意图，将三宿主同步、废弃入口迁出和最终安装状态提交放在同一恢复流程中。
 * 入参：options（object，可选），可注入 packageRoot、platform、architecture、environment 和 userHome 供安装与测试使用。
 * 返回值：object，包含 binary、Skill 登记结果及机器可读的首次安装、授权和更新状态。
 */
function installPackage(options = {}) {
  const packageRoot = options.packageRoot || join(__dirname, "..");
  const platform = options.platform || process.platform;
  const architecture = options.architecture || process.arch;
  const environment = options.environment || process.env;
  const userHome = options.userHome || homedir();
  const executable = platform === "win32" ? "everyline-cli.exe" : "everyline-cli";
  const binary = join(packageRoot, "bin", resolvePlatformTarget(platform, architecture), executable);

  // npm 安装必须确认当前平台产物真实存在，防止留下“安装成功但 CLI 不可运行”的包。
  if (!existsSync(binary)) {
    throw new Error(`npm 包缺少当前平台二进制: ${binary}`);
  }
  if (platform !== "win32") {
    chmodSync(binary, 0o755);
  }

  if (!shouldInstallCodexSkill(environment)) {
    return {
      binary,
      skillTarget: "",
      skillStatus: "",
      skills: { codex: [], workBuddy: [], doubao: [] },
      doubaoSkillReloadRequired: false,
      firstInstall: false,
      authorizationRequired: false,
      nextAction: "",
      updated: false,
    };
  }

  const codexSkillRoot = environment.EVERYLINE_CODEX_SKILLS_DIR || join(userHome, ".agents", "skills");
  const plans = buildSkillSetPlans(packageRoot, codexSkillRoot, "Codex", "codex");
  const installedSkillRoots = [codexSkillRoot];
  if (shouldInstallWorkBuddySkills(environment)) {
    const workBuddySkillRoot = environment.EVERYLINE_WORKBUDDY_SKILLS_DIR || join(userHome, ".workbuddy", "skills");
    plans.push(...buildSkillSetPlans(
      packageRoot,
      workBuddySkillRoot,
      "WorkBuddy",
      "workBuddy",
    ));
    installedSkillRoots.push(workBuddySkillRoot);
  }
  plans.push(...buildDoubaoSkillPlans(packageRoot, skillNames, environment, platform, userHome, deprecatedSkillNames));
  // 已校验的旧链接进入同一撤销列表，避免最终提交失败后丢失原来的公共授权入口。
  for (const skillRoot of installedSkillRoots) {
    plans.push(...inspectDeprecatedSkillRegistrations(packageRoot, skillRoot).map((registration) => ({
      ...registration, deprecated: true, installMode: "deprecated-link", status: "removed",
    })));
  }
  let hadDeprecatedRegistrations = false;
  let installState;
  const registrations = registerAgentSkillPlans(plans, platform, (inspected) => {
    // 仅已确认需要迁出的目录或链接证明旧安装存在，跳过的未知目录不影响首次授权判定。
    hadDeprecatedRegistrations = inspected.some((registration) => registration.deprecated && registration.status === "removed");
    const existingState = loadInstallState(resolveInstallStatePath(environment, userHome));
    if (!existingState && !hadDeprecatedRegistrations && inspected.filter((registration) => !registration.deprecated).every(({ status }) => status === "created")) {
      // 只预提交全新安装的待授权意图；失败或进程中断后，重试仍读取该门禁，旧安装的版本更新留到登记成功后。
      ensureFirstInstallState(packageRoot, environment, userHome, platform, inspected, false, binary);
    }
  }, (inspected) => {
    installState = ensureFirstInstallState(packageRoot, environment, userHome, platform, inspected, hadDeprecatedRegistrations, binary);
  });
  const hostSkills = (hostKey) => registrations
    .filter((registration) => registration.hostKey === hostKey && !registration.deprecated)
    .map(({ name, target, status, backupPath }) => ({ name, target, status, ...(backupPath ? { backupPath } : {}) }));
  const codexSkills = hostSkills("codex");
  const workBuddySkills = hostSkills("workBuddy");
  const doubaoSkills = hostSkills("doubao");
  return {
    binary,
    skillTarget: codexSkills[0].target,
    skillStatus: codexSkills[0].status,
    skills: { codex: codexSkills, workBuddy: workBuddySkills, doubao: doubaoSkills },
    doubaoSkillReloadRequired: registrations.some((skill) => skill.hostKey === "doubao" && skill.status !== "existing" && skill.status !== "skipped"),
    installStatePath: installState.path,
    firstInstall: installState.firstInstall === true,
    authorizationRequired: installState.authorizationRequired === true,
    nextAction: installState.nextAction || "",
    updated: installState.updated === true,
  };
}

/**
 * formatInstallOutput 生成全局安装成功后的用户可见输出和机器可读首次安装或更新事件。
 * 入参：result（object）为 installPackage 返回的安装结果。
 * 返回值：string，为可直接写入 stdout 的完整文本；未登记 Skill 时为空字符串。
 */
function formatInstallOutput(result) {
  if (!result.skillTarget) {
    return "";
  }
  const lines = [result.updated
    ? updateMessage
    : (result.authorizationRequired ? firstInstallMessage : "EveryLine CLI 与 Agent Skills 安装完成")];
  for (const [hostName, skills] of Object.entries(result.skills)) {
    for (const skill of skills) {
      lines.push(`${hostName}: ${skill.target} (${skill.status})`);
    }
  }
  if (result.doubaoSkillReloadRequired) {
    lines.push("豆包本地 Skill 已同步；请重新读取两项 SKILL.md，或新建任务加载新版。历史对话不会自动重载。");
    lines.push(JSON.stringify({
      schema: "everyline.skill-event.v1", event: "skills_updated", host: "doubao",
      reloadRequired: true, nextAction: "reload_skills", skills: result.skills.doubao,
    }));
  }
  if (result.updated) {
    lines.push(JSON.stringify({
      schema: "everyline.skill-event.v1",
      event: "updated",
      authCheckRequired: true,
      nextAction: "auth_status",
      recommendedSkill: "everyline-review",
      message: updateMessage,
      authorizationRequiredMessage,
      authorizedMessage,
    }));
  } else if (result.authorizationRequired) {
    lines.push(JSON.stringify({
      schema: "everyline.skill-event.v1",
      event: "first_install",
      authorizationRequired: true,
      nextAction: "authorize",
      recommendedSkill: "everyline-review",
      message: firstInstallMessage,
    }));
  }
  return `${lines.join("\n")}\n`;
}

if (require.main === module) {
  const result = installPackage();
  const output = formatInstallOutput(result);
  if (output) {
    process.stdout.write(output);
  }
}

module.exports = {
  formatInstallOutput,
  installPackage,
  ensureFirstInstallState,
  loadInstallState,
  registerAgentSkill,
  registerCodexSkill,
  registerSkillSet,
  removeDeprecatedSkillRegistrations,
  shouldInstallCodexSkill,
  shouldInstallWorkBuddySkills,
  skillNames,
};
