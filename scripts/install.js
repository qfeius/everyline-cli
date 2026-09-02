#!/usr/bin/env node

const { chmodSync, existsSync, lstatSync, mkdirSync, realpathSync, symlinkSync } = require("node:fs");
const { homedir } = require("node:os");
const { dirname, join, resolve } = require("node:path");
const { resolvePlatformTarget } = require("./platform");

/**
 * shouldInstallCodexSkill 判断本次 npm 生命周期是否应登记 Codex Skill。
 * 入参：environment（NodeJS.ProcessEnv），当前进程环境变量。
 * 返回值：boolean，仅全局安装且未显式跳过时为 true。
 */
function shouldInstallCodexSkill(environment) {
  return environment.npm_config_global === "true" && environment.EVERYLINE_SKIP_SKILL_INSTALL !== "1";
}

/**
 * registerCodexSkill 将 npm 包内 Skill 以目录链接登记到 Codex 用户级目录。
 * 入参：source（string）为包内 Skill 绝对路径；target（string）为 Codex Skill 目标路径；platform（string）为 Node 平台名。
 * 返回值："created" | "existing"，分别表示新建链接或已存在同源链接。
 */
function registerCodexSkill(source, target, platform = process.platform) {
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
      // realpath 同时兼容 POSIX 符号链接和 Windows 目录联接的路径表示差异。
      if (realpathSync(target) === realpathSync(resolvedSource)) {
        return "existing";
      }
    }
    throw new Error(`Codex Skill 目标已存在，请先确认并移走原目录: ${target}`);
  }

  // 使用链接让 npm 原地升级后自动切换到同一包内的新 Skill，避免 CLI 与 Skill 版本漂移。
  mkdirSync(dirname(target), { recursive: true });
  symlinkSync(resolvedSource, target, platform === "win32" ? "junction" : "dir");
  return "created";
}

/**
 * installPackage 完成原生 CLI 校验，并在全局安装时同步登记 Codex Skill。
 * 入参：options（object，可选），可注入 packageRoot、platform、architecture、environment 和 userHome 供安装与测试使用。
 * 返回值：object，包含 binary、skillTarget 和 skillStatus；未登记 Skill 时后两项为空。
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
    return { binary, skillTarget: "", skillStatus: "" };
  }

  const skillRoot = environment.EVERYLINE_CODEX_SKILLS_DIR || join(userHome, ".agents", "skills");
  const skillTarget = join(skillRoot, "everyline-cli");
  const skillStatus = registerCodexSkill(join(packageRoot, "skills", "everyline-cli"), skillTarget, platform);
  return { binary, skillTarget, skillStatus };
}

if (require.main === module) {
  const result = installPackage();
  if (result.skillTarget) {
    process.stdout.write(`EveryLine CLI 与 Codex Skill 安装完成\nSkill: ${result.skillTarget} (${result.skillStatus})\n`);
  }
}

module.exports = { installPackage, registerCodexSkill, shouldInstallCodexSkill };
