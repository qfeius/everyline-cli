#!/usr/bin/env node

const { chmodSync, existsSync, lstatSync, mkdirSync, realpathSync, rmSync, symlinkSync } = require("node:fs");
const { homedir } = require("node:os");
const { dirname, join, resolve } = require("node:path");
const { resolvePlatformTarget } = require("./platform");

// skillNames 是同一份 npm 包向 Codex、WorkBuddy 和豆包发布的三项职责分离 Skill。
const skillNames = ["everyline-shared", "everyline-review", "everyline-review-config"];

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
 * inspectAgentSkillRegistration 只读校验 Skill 来源和目标，并生成后续登记计划。
 * 入参：source（string）为包内 Skill 路径；target（string）为宿主目标路径；hostName（string）为宿主名；metadata（object）为需透传的名称与分组。
 * 返回值：object，包含规范化来源、目标、预期状态以及调用方元数据。
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
      // realpath 同时兼容 POSIX 符号链接和 Windows 目录联接的路径表示差异。
      if (realpathSync(target) === realpathSync(resolvedSource)) {
        return { ...metadata, source: resolvedSource, target, hostName, status: "existing" };
      }
    }
    throw new Error(`${hostName} Skill 目标已存在，请先确认并移走原目录: ${target}`);
  }

  return { ...metadata, source: resolvedSource, target, hostName, status: "created" };
}

/**
 * registerAgentSkillPlans 先预检全部目标，再一次性登记并在异常时回滚本轮新建链接。
 * 入参：plans（Array<object>）为来源、目标、宿主和元数据列表；platform（string）为 Node 平台名。
 * 返回值：Array<object>，每项保留计划元数据并带有 created/existing 状态。
 */
function registerAgentSkillPlans(plans, platform = process.platform) {
  // 全量预检发生在任何写入前，常见的同名目录冲突不会留下半套登记结果。
  const inspected = plans.map((plan) => inspectAgentSkillRegistration(
    plan.source,
    plan.target,
    plan.hostName,
    plan,
  ));
  const created = [];
  try {
    for (const registration of inspected) {
      if (registration.status === "existing") {
        continue;
      }
      mkdirSync(dirname(registration.target), { recursive: true });
      symlinkSync(registration.source, registration.target, platform === "win32" ? "junction" : "dir");
      created.push(registration);
    }
    return inspected;
  } catch (error) {
    // 只删除本轮创建且仍指向同一来源的链接，保留并发出现的用户目录或其他来源。
    for (const registration of created.reverse()) {
      try {
        const targetState = lstatSync(registration.target);
        if (targetState.isSymbolicLink() && realpathSync(registration.target) === realpathSync(registration.source)) {
          rmSync(registration.target, { force: true });
        }
      } catch {
        // 回滚采用尽力而为策略，原始安装错误仍作为主错误返回。
      }
    }
    throw error;
  }
}

/**
 * registerAgentSkill 将 npm 包内单项 Skill 以目录链接登记到指定 Agent 宿主目录。
 * 入参：source（string）为包内 Skill 绝对路径；target（string）为宿主 Skill 目标路径；platform（string）为 Node 平台名；hostName（string）为错误提示中的宿主名。
 * 返回值："created" | "existing"，分别表示新建链接或已存在同源链接。
 */
function registerAgentSkill(source, target, platform = process.platform, hostName = "Agent") {
  const [registration] = registerAgentSkillPlans([{ source, target, hostName }], platform);
  return registration.status;
}

/**
 * buildSkillSetPlans 构造一个宿主下三项职责分离 Skill 的无副作用登记计划。
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
 * 返回值："created" | "existing"，含义与 registerAgentSkill 一致。
 */
function registerCodexSkill(source, target, platform = process.platform) {
  return registerAgentSkill(source, target, platform, "Codex");
}

/**
 * registerSkillSet 将职责分离的三项 EveryLine Skill 登记到一个宿主根目录。
 * 入参：packageRoot（string）为 npm 包根目录；skillRoot（string）为宿主 Skill 根目录；platform（string）为 Node 平台名；hostName（string）为宿主名。
 * 返回值：Array<object>，每项包含 name、target 和 created/existing 状态。
 */
function registerSkillSet(packageRoot, skillRoot, platform, hostName) {
  return registerAgentSkillPlans(buildSkillSetPlans(packageRoot, skillRoot, hostName), platform)
    .map(({ name, target, status }) => ({ name, target, status }));
}

/**
 * installPackage 完成原生 CLI 校验，并在全局安装时同步登记 Codex 与 WorkBuddy Skills。
 * 入参：options（object，可选），可注入 packageRoot、platform、architecture、environment 和 userHome 供安装与测试使用。
 * 返回值：object，包含 binary、兼容的首个 Codex skillTarget/skillStatus，以及按宿主分组的 skills。
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
    return { binary, skillTarget: "", skillStatus: "", skills: { codex: [], workBuddy: [] } };
  }

  const codexSkillRoot = environment.EVERYLINE_CODEX_SKILLS_DIR || join(userHome, ".agents", "skills");
  const plans = buildSkillSetPlans(packageRoot, codexSkillRoot, "Codex", "codex");
  if (shouldInstallWorkBuddySkills(environment)) {
    plans.push(...buildSkillSetPlans(
      packageRoot,
      environment.EVERYLINE_WORKBUDDY_SKILLS_DIR || join(userHome, ".workbuddy", "skills"),
      "WorkBuddy",
      "workBuddy",
    ));
  }
  const registrations = registerAgentSkillPlans(plans, platform);
  const hostSkills = (hostKey) => registrations
    .filter((registration) => registration.hostKey === hostKey)
    .map(({ name, target, status }) => ({ name, target, status }));
  const codexSkills = hostSkills("codex");
  const workBuddySkills = hostSkills("workBuddy");
  return {
    binary,
    skillTarget: codexSkills[0].target,
    skillStatus: codexSkills[0].status,
    skills: { codex: codexSkills, workBuddy: workBuddySkills },
  };
}

if (require.main === module) {
  const result = installPackage();
  if (result.skillTarget) {
    process.stdout.write("EveryLine CLI 与 Agent Skills 安装完成\n");
    for (const [hostName, skills] of Object.entries(result.skills)) {
      for (const skill of skills) {
        process.stdout.write(`${hostName}: ${skill.target} (${skill.status})\n`);
      }
    }
  }
}

module.exports = {
  installPackage,
  registerAgentSkill,
  registerCodexSkill,
  registerSkillSet,
  shouldInstallCodexSkill,
  shouldInstallWorkBuddySkills,
  skillNames,
};
