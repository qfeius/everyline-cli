#!/usr/bin/env node

const { randomUUID } = require("node:crypto");
const {
  chmodSync,
  existsSync,
  lstatSync,
  mkdirSync,
  readlinkSync,
  readFileSync,
  realpathSync,
  renameSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} = require("node:fs");
const { homedir } = require("node:os");
const { dirname, join, resolve } = require("node:path");
const { resolvePlatformTarget } = require("./platform");

// skillNames 是同一份 npm 包向 Codex、WorkBuddy 和豆包发布的三项职责分离 Skill。
const skillNames = ["everyline-cli", "everyline-review", "everyline-review-config"];
const deprecatedSkillNames = ["everyline-shared"];
const installStateSchema = "everyline.install-state.v1";
// 安装提示按首次安装、更新完成及更新后的真实授权状态拆分，供人类输出和 Agent 事件复用。
const firstInstallMessage = "EveryLine CLI 已安装完成。目前支持合同审查，以及审查清单、规则和规则分组配置。使用前需要先完成账号授权，我现在可以为你打开授权页面或生成授权链接。";
const updateMessage = "EveryLine CLI 已更新完成。目前支持合同审查，以及审查清单、规则和规则分组配置。";
const authorizationRequiredMessage = "使用前需要先完成账号授权，我现在可以为你打开授权页面或生成授权链接。";
const authorizedMessage = "当前已存在生效授权，可直接调用cli能力。";

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
 * removeDeprecatedSkillRegistrations 清理本包旧版本创建且仍指向同一包路径的 Skill 链接。
 * 入参：packageRoot（string）为 npm 包根目录；skillRoot（string）为宿主 Skill 根目录。
 * 返回值：string[]，为本次安全移除的旧 Skill 名称；用户目录或其他来源链接保持不变。
 */
function removeDeprecatedSkillRegistrations(packageRoot, skillRoot) {
  const removed = [];
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

    // 旧链接的源目录在升级后可以已经不存在，因此直接比较链接文本，不依赖 realpath。
    const linkedSource = resolve(dirname(target), readlinkSync(target));
    const expectedSource = resolve(packageRoot, "skills", name);
    if (linkedSource !== expectedSource) {
      continue;
    }
    rmSync(target, { force: true });
    removed.push(name);
  }
  return removed;
}

/**
 * resolveInstallStatePath 解析安装器与原生 CLI 共享的首次安装状态路径。
 * 入参：environment（NodeJS.ProcessEnv）为环境变量；userHome（string）为用户目录。
 * 返回值：string，为 install-state.json 的绝对路径。
 */
function resolveInstallStatePath(environment, userHome) {
  const configured = String(environment.EVERYLINE_CONFIG_DIR || "").trim();
  return join(configured ? resolve(configured) : join(userHome, ".everyline-cli"), "install-state.json");
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
 * saveInstallState 原子写入不含凭证的首次安装状态，并收紧目录与文件权限。
 * 入参：statePath（string）为目标路径；state（object）为完整状态；platform（string）为 Node 平台名。
 * 返回值：无；文件操作失败时抛出 Error。
 */
function saveInstallState(statePath, state, platform) {
  const stateDirectory = dirname(statePath);
  mkdirSync(stateDirectory, { recursive: true, mode: 0o700 });
  if (platform !== "win32") {
    chmodSync(stateDirectory, 0o700);
  }
  const temporaryPath = `${statePath}.${process.pid}.${randomUUID()}.tmp`;
  try {
    writeFileSync(temporaryPath, `${JSON.stringify(state, null, 2)}\n`, { mode: 0o600 });
    if (platform !== "win32") {
      chmodSync(temporaryPath, 0o600);
    }
    renameSync(temporaryPath, statePath);
  } finally {
    rmSync(temporaryPath, { force: true });
  }
}

/**
 * ensureFirstInstallState 在首次创建 Agent Skill 时建立授权门禁，并用统一包版本识别真实升级。
 * 入参：packageRoot（string）为包根；environment（NodeJS.ProcessEnv）为环境；userHome（string）为用户目录；platform（string）为平台；registrations（Array<object>）为 Skill 登记结果。
 * 返回值：object，包含状态路径及当前 firstInstall/authorizationRequired/nextAction/updated。
 */
function ensureFirstInstallState(packageRoot, environment, userHome, platform, registrations) {
  const statePath = resolveInstallStatePath(environment, userHome);
  const packageData = JSON.parse(readFileSync(join(packageRoot, "package.json"), "utf8"));
  const installedVersion = String(packageData.version || "");
  const existing = loadInstallState(statePath);
  if (existing) {
    // 只有统一包版本变化才视为更新；同版本重装不会重复触发更新提示。
    const updated = installedVersion !== "" && installedVersion !== String(existing.installedVersion || "");
    if (updated) {
      existing.installedVersion = installedVersion;
      saveInstallState(statePath, existing, platform);
    }
    return { path: statePath, ...existing, updated };
  }
  if (!registrations.some((registration) => registration.status === "created")) {
    return { path: statePath, firstInstall: false, authorizationRequired: false, nextAction: "", updated: false };
  }
  const state = {
    schema: installStateSchema,
    eventId: randomUUID(),
    installedVersion,
    firstInstall: true,
    authorizationRequired: true,
    nextAction: "authorize",
  };
  saveInstallState(statePath, state, platform);
  return { path: statePath, ...state, updated: false };
}

/**
 * installPackage 完成原生 CLI 校验，并在全局安装时同步登记 Codex 与 WorkBuddy Skills。
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
      skills: { codex: [], workBuddy: [] },
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
  const registrations = registerAgentSkillPlans(plans, platform);
  // 新三项 Skill 全部登记成功后，再移除本包遗留的 everyline-shared 链接。
  for (const skillRoot of installedSkillRoots) {
    removeDeprecatedSkillRegistrations(packageRoot, skillRoot);
  }
  const hostSkills = (hostKey) => registrations
    .filter((registration) => registration.hostKey === hostKey)
    .map(({ name, target, status }) => ({ name, target, status }));
  const codexSkills = hostSkills("codex");
  const workBuddySkills = hostSkills("workBuddy");
  const installState = ensureFirstInstallState(packageRoot, environment, userHome, platform, registrations);
  return {
    binary,
    skillTarget: codexSkills[0].target,
    skillStatus: codexSkills[0].status,
    skills: { codex: codexSkills, workBuddy: workBuddySkills },
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
  if (result.updated) {
    lines.push(JSON.stringify({
      schema: "everyline.skill-event.v1",
      event: "updated",
      authCheckRequired: true,
      nextAction: "auth_status",
      recommendedSkill: "everyline-cli",
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
      recommendedSkill: "everyline-cli",
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
