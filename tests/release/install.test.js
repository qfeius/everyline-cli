"use strict";

const assert = require("node:assert/strict");
const { copyFileSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, readlinkSync, realpathSync, rmSync, symlinkSync, writeFileSync } = require("node:fs");
const { execFileSync } = require("node:child_process");
const { tmpdir } = require("node:os");
const { join, sep } = require("node:path");
const test = require("node:test");
const {
  restoreGlobalCommand,
  formatInstallOutput,
  installPackage,
  registerCodexSkill,
  removeDeprecatedSkillRegistrations,
  shouldInstallCodexSkill,
  shouldInstallWorkBuddySkills,
  skillNames,
} = require("../../scripts/install");

// 安装状态由真实 CLI 加锁写入；整组测试只构建一次，随后复制到隔离包中验证 Node/Go 接口。
let nativeFixtureRoot;
let nativeFixtureBinary;
test.before(() => {
  nativeFixtureRoot = mkdtempSync(join(tmpdir(), "everyline-installer-binary-"));
  nativeFixtureBinary = join(nativeFixtureRoot, process.platform === "win32" ? "everyline-cli.exe" : "everyline-cli");
  execFileSync("go", ["build", "-o", nativeFixtureBinary, "./cmd/everyline-cli"], { cwd: join(__dirname, "../..") });
});
test.after(() => rmSync(nativeFixtureRoot, { recursive: true, force: true }));

/**
 * createFixture 创建隔离的 Skill 源目录和 Codex 目标目录。
 * 入参：无。
 * 返回值：object，包含临时根目录 root、源目录 source 和目标路径 target。
 */
function createFixture() {
  const root = mkdtempSync(join(tmpdir(), "everyline-install-"));
  const source = join(root, "package", "skills", "everyline-review");
  const target = join(root, "codex", "skills", "everyline-review");
  mkdirSync(source, { recursive: true });
  writeFileSync(join(source, "SKILL.md"), "---\nname: everyline-review\n---\n");
  return { root, source, target };
}

/**
 * createPackageFixture 创建包含可执行 CLI 和两项 Skill 的最小 npm 包夹具。
 * 入参：无。
 * 返回值：object，包含临时根目录 root、模拟包根 packageRoot 和用户目录 userHome。
 */
function createPackageFixture() {
  const root = mkdtempSync(join(tmpdir(), "everyline-package-install-"));
  const packageRoot = join(root, "package");
  const userHome = join(root, "home");
  const binary = join(packageRoot, "bin", "linux-amd64", "everyline-cli");
  mkdirSync(join(packageRoot, "bin", "linux-amd64"), { recursive: true });
  copyFileSync(nativeFixtureBinary, binary);
  writeFileSync(join(packageRoot, "package.json"), '{"name":"everyline-cli","version":"9.8.7","bin":{"everyline-cli":"scripts/run.js"}}\n');
  for (const name of skillNames) {
    const source = join(packageRoot, "skills", name);
    mkdirSync(source, { recursive: true });
    writeFileSync(join(source, "SKILL.md"), `---\nname: ${name}\n---\n`);
  }
  return { root, packageRoot, userHome };
}

/**
 * failInstallStateCommit 在最终登记时注入目录故障，验证真实原生 CLI 的失败会传回安装器。
 * 入参：options（object）为隔离安装参数；failCall（number）为失败的登记调用序号，首次安装用 2、升级用 1。
 * 返回值：void；没有到达指定提交或安装器未报告失败时断言失败，故障目录始终恢复。
 */
function failInstallStateCommit(options, failCall) {
  execFileSync(process.execPath, ["-e", `
const fs = require('node:fs');
const child = require('node:child_process');
const assert = require('node:assert/strict');
const original = child.execFileSync;
const failCall = Number(process.argv[3]);
let calls = 0;
child.execFileSync = (file, args, options) => {
  if (args[0] !== '_record-install' || ++calls !== failCall) return original(file, args, options);
  const directory = options.env.EVERYLINE_CONFIG_DIR;
  const saved = directory + '-state-failure-fixture';
  fs.renameSync(directory, saved);
  fs.writeFileSync(directory, 'temporarily unavailable config directory');
  try { return original(file, args, options); }
  finally { fs.unlinkSync(directory); fs.renameSync(saved, directory); }
};
assert.throws(() => require(process.argv[1]).installPackage(JSON.parse(process.argv[2])));
assert.equal(calls, failCall);
`, join(__dirname, "../../scripts/install.js"), JSON.stringify(options), String(failCall)]);
}

test("Codex Skill 只在 npm 全局安装时自动登记", () => {
  assert.equal(shouldInstallCodexSkill({ npm_config_global: "true" }), true);
  assert.equal(shouldInstallCodexSkill({ npm_config_global: "false" }), false);
  assert.equal(shouldInstallCodexSkill({ npm_config_global: "true", EVERYLINE_SKIP_SKILL_INSTALL: "1" }), false);
});

test("WorkBuddy Skills 支持统一跳过和宿主级跳过", () => {
  assert.equal(shouldInstallWorkBuddySkills({ npm_config_global: "true" }), true);
  assert.equal(shouldInstallWorkBuddySkills({ npm_config_global: "true", EVERYLINE_SKIP_SKILL_INSTALL: "1" }), false);
  assert.equal(shouldInstallWorkBuddySkills({ npm_config_global: "true", EVERYLINE_SKIP_WORKBUDDY_SKILL_INSTALL: "1" }), false);
});

/**
 * 验证豆包原生文件夹随 npm 安装更新，包含引用文件，且旧副本留在扫描目录外。
 * 入参：t（TestContext），用于清理隔离目录。
 * 返回值：void，未同步、重复更新或污染其他技能时断言失败。
 */
test("豆包本地 Skill 随安装同步并可重复更新", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const skillRoot = join(fixture.root, "workspace", ".user_skills");
  const reference = join(fixture.packageRoot, "skills", "everyline-review", "references", "review-config.md");
  mkdirSync(join(reference, ".."), { recursive: true });
  writeFileSync(reference, "最新审查流程");
  const oldSkill = join(skillRoot, "everyline-review");
  mkdirSync(oldSkill, { recursive: true });
  writeFileSync(join(oldSkill, "SKILL.md"), "---\nname: everyline-review\n---\n旧文案");
  const unrelated = join(skillRoot, "my-skill");
  mkdirSync(unrelated);
  writeFileSync(join(unrelated, "SKILL.md"), "用户技能");
  const options = {
    packageRoot: fixture.packageRoot, platform: "linux", architecture: "x64", userHome: fixture.userHome,
    environment: { npm_config_global: "true", EVERYLINE_DOUBAO_SKILLS_DIR: skillRoot },
  };
  const result = installPackage(options);
  assert.deepEqual(result.skills.doubao.map((skill) => skill.name), skillNames);
  assert.equal(result.doubaoSkillReloadRequired, true);
  assert.equal(result.skills.doubao[0].status, "updated");
  assert.equal(lstatSync(oldSkill).isSymbolicLink(), false);
  assert.equal(readFileSync(join(oldSkill, "SKILL.md"), "utf8"), readFileSync(join(fixture.packageRoot, "skills", "everyline-review", "SKILL.md"), "utf8"));
  assert.equal(readFileSync(join(skillRoot, "everyline-review", "references", "review-config.md"), "utf8"), "最新审查流程");
  assert.equal(readFileSync(join(unrelated, "SKILL.md"), "utf8"), "用户技能");
  assert.equal(result.skills.doubao[0].backupPath.startsWith(skillRoot + sep), false);
  assert.match(readFileSync(join(result.skills.doubao[0].backupPath, "SKILL.md"), "utf8"), /旧文案/);
  assert.match(formatInstallOutput(result), /豆包本地 Skill 已同步/);
  const repeated = installPackage(options);
  assert.equal(repeated.doubaoSkillReloadRequired, false);
  assert.equal(repeated.skills.doubao.every((skill) => skill.status === "existing"), true);
  // CLI 版本号相同也按技能内容更新，覆盖此前同版本重新打包却漏更 Skill 的场景。
  writeFileSync(reference, "同版本修订后的流程");
  const revised = installPackage(options);
  assert.equal(revised.updated, false);
  assert.equal(revised.doubaoSkillReloadRequired, true);
  assert.equal(readFileSync(join(skillRoot, "everyline-review", "references", "review-config.md"), "utf8"), "同版本修订后的流程");
  const events = formatInstallOutput(revised).split("\n").filter((line) => line.startsWith("{")).map((line) => JSON.parse(line));
  assert.equal(events.some((event) => event.event === "skills_updated" && event.nextAction === "reload_skills"), true);
});

/**
 * 验证仅探测真实存在的 macOS 豆包工作目录，其他环境由明确目录接入。
 * 入参：t（TestContext）为临时目录清理上下文。
 * 返回值：void，错误探测、相对路径或跳过配置失效时断言失败。
 */
test("豆包目录自动发现及显式跳过", (t) => {
  const { buildDoubaoSkillPlans } = require("../../scripts/doubao-skills");
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const nativeRoot = join(fixture.userHome, "Library", "Application Support", "DoubaoWork", "Default", ".doubaowork", "agent_mode", "workspace", ".user_skills");
  const plans = (environment, platform = "darwin") => buildDoubaoSkillPlans(fixture.packageRoot, skillNames, environment, platform, fixture.userHome);
  assert.deepEqual(plans({}), []);
  assert.equal(existsSync(nativeRoot), false);
  mkdirSync(nativeRoot, { recursive: true });
  assert.deepEqual(plans({}).map((plan) => plan.target), skillNames.map((name) => join(nativeRoot, name)));
  assert.deepEqual(plans({}, "linux"), []);
  assert.deepEqual(plans({ EVERYLINE_SKIP_DOUBAO_SKILL_INSTALL: "1" }), []);
  assert.throws(() => plans({ EVERYLINE_DOUBAO_SKILLS_DIR: "relative" }), /绝对目录/);
  const options = { packageRoot: fixture.packageRoot, platform: "linux", architecture: "x64", userHome: fixture.userHome };
  for (const environment of [
    { npm_config_global: "false" },
    { npm_config_global: "true", EVERYLINE_SKIP_SKILL_INSTALL: "1" },
    { npm_config_global: "true", EVERYLINE_SKIP_DOUBAO_SKILL_INSTALL: "1" },
  ]) {
    assert.deepEqual(installPackage({ ...options, environment: { ...environment, EVERYLINE_DOUBAO_SKILLS_DIR: nativeRoot } }).skills.doubao, []);
    assert.deepEqual(readdirSync(nativeRoot), []);
  }
});

/**
 * 验证豆包同名非 EveryLine 目录在全量预检阶段阻止写入，不留下其他宿主的部分更新。
 * 入参：t（TestContext）为临时目录清理上下文。
 * 返回值：void，用户文件被覆盖或预检前有写入时断言失败。
 */
test("豆包目录冲突保留用户文件和其他宿主", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const nativeRoot = join(fixture.root, "workspace", ".user_skills");
  const conflict = join(nativeRoot, "everyline-review", "SKILL.md");
  mkdirSync(join(conflict, ".."), { recursive: true });
  writeFileSync(conflict, "---\nname: my-custom-review\n---\n用户内容");
  assert.throws(() => installPackage({
    packageRoot: fixture.packageRoot, platform: "linux", architecture: "x64", userHome: fixture.userHome,
    environment: { npm_config_global: "true", EVERYLINE_DOUBAO_SKILLS_DIR: nativeRoot },
  }), /未声明对应 EveryLine/);
  assert.match(readFileSync(conflict, "utf8"), /用户内容/);
  assert.deepEqual(readdirSync(nativeRoot), ["everyline-review"]);
  assert.equal(existsSync(join(fixture.userHome, ".agents", "skills")), false);
});

/**
 * 验证第二项豆包技能替换失败时，第一项旧目录和其他宿主的链接均恢复。
 * 入参：t（TestContext）为子进程故障夹具的清理上下文。
 * 返回值：void，故障未报告、旧副本丢失或残留部分新技能时断言失败。
 */
test("豆包同步中途失败会回滚所有宿主的本轮变更", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const nativeRoot = join(fixture.root, "workspace", ".user_skills");
  const original = "---\nname: everyline-review\n---\n原始内容";
  mkdirSync(join(nativeRoot, "everyline-review"), { recursive: true });
  writeFileSync(join(nativeRoot, "everyline-review", "SKILL.md"), original);
  const script = `
    const fs = require('node:fs');
    const path = require('node:path');
    const originalRename = fs.renameSync;
    const options = JSON.parse(process.argv[1]);
    fs.renameSync = (source, target) => {
      if (path.basename(source) === 'new' && target === path.join(options.environment.EVERYLINE_DOUBAO_SKILLS_DIR, 'everyline-review')) {
        throw new Error('fixture-copy-failure');
      }
      return originalRename(source, target);
    };
    require('node:assert/strict').throws(() => require(process.argv[2]).installPackage(options), /fixture-copy-failure/);
  `;
  execFileSync(process.execPath, ["-e", script, JSON.stringify({
    packageRoot: fixture.packageRoot, platform: "linux", architecture: "x64", userHome: fixture.userHome,
    environment: { npm_config_global: "true", EVERYLINE_DOUBAO_SKILLS_DIR: nativeRoot },
  }), join(__dirname, "../../scripts/install.js")]);
  assert.equal(readFileSync(join(nativeRoot, "everyline-review", "SKILL.md"), "utf8"), original);
  assert.deepEqual(readdirSync(nativeRoot), ["everyline-review"]);
  assert.deepEqual(readdirSync(join(nativeRoot, "..")), [".user_skills"]);
  for (const host of [".agents", ".workbuddy"]) assert.deepEqual(readdirSync(join(fixture.userHome, host, "skills")), []);
});

/**
 * 验证已安装状态或旧版无状态升级的最终提交失败时，三个宿主和废弃入口都恢复原状。
 * 入参：t（TestContext）为授权状态子场景及临时目录清理上下文。
 * 返回值：Promise<void>，原链接、目录、token 或状态文件未恢复，或重试语义变化时断言失败。
 */
test("升级最终提交失败恢复三宿主和旧 shared 入口", async (t) => {
  for (const stateKind of ["authorized", "pending", "legacy"]) {
    await t.test(stateKind, (t) => {
      const previous = createPackageFixture();
      const current = createPackageFixture();
      t.after(() => {
        rmSync(previous.root, { recursive: true, force: true });
        rmSync(current.root, { recursive: true, force: true });
      });
      const manifestPath = join(previous.packageRoot, "package.json");
      const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
      manifest.version = "9.8.6";
      writeFileSync(manifestPath, JSON.stringify(manifest));
      for (const name of skillNames) {
        writeFileSync(join(current.packageRoot, "skills", name, "SKILL.md"), `---\nname: ${name}\n---\nnew version`);
      }
      const nativeRoot = join(previous.userHome, "workspace", ".user_skills");
      const options = { packageRoot: previous.packageRoot, platform: "linux", architecture: "x64", userHome: previous.userHome, environment: { npm_config_global: "true", EVERYLINE_DOUBAO_SKILLS_DIR: nativeRoot } };
      const original = installPackage(options);
      const state = JSON.parse(readFileSync(original.installStatePath, "utf8"));
      if (stateKind === "legacy") rmSync(original.installStatePath);
      else {
        if (stateKind === "authorized") Object.assign(state, { firstInstall: false, authorizationRequired: false, nextAction: "" });
        writeFileSync(original.installStatePath, JSON.stringify(state));
      }
      const originalState = stateKind === "legacy" ? null : readFileSync(original.installStatePath, "utf8");
      const tokenPath = join(previous.userHome, ".everyline-cli", "tokens.json");
      const token = '{"tokens":{"test::user":{"access_token":"fixture-old-token"}}}';
      writeFileSync(tokenPath, token);
      const sharedContent = "---\nname: everyline-shared\n---\nold public skill";
      mkdirSync(join(nativeRoot, "everyline-shared"));
      writeFileSync(join(nativeRoot, "everyline-shared", "SKILL.md"), sharedContent);
      for (const host of [".agents", ".workbuddy"]) {
        symlinkSync(join(previous.packageRoot, "skills", "everyline-shared"), join(previous.userHome, host, "skills", "everyline-shared"), "dir");
      }

      options.packageRoot = current.packageRoot;
      failInstallStateCommit(options, 1);

      for (const host of [".agents", ".workbuddy"]) {
        const root = join(previous.userHome, host, "skills");
        assert.deepEqual(readdirSync(root).sort(), [...skillNames, "everyline-shared"].sort());
        for (const name of skillNames) assert.equal(realpathSync(join(root, name)), realpathSync(join(previous.packageRoot, "skills", name)));
        assert.equal(lstatSync(join(root, "everyline-shared")).isSymbolicLink(), true);
      }
      for (const name of skillNames) {
        assert.equal(readFileSync(join(nativeRoot, name, "SKILL.md"), "utf8"), readFileSync(join(previous.packageRoot, "skills", name, "SKILL.md"), "utf8"));
      }
      assert.equal(readFileSync(join(nativeRoot, "everyline-shared", "SKILL.md"), "utf8"), sharedContent);
      assert.deepEqual(readdirSync(join(nativeRoot, "..")), [".user_skills"]);
      assert.equal(readFileSync(tokenPath, "utf8"), token);
      assert.equal(stateKind === "legacy" ? existsSync(original.installStatePath) : readFileSync(original.installStatePath, "utf8"), stateKind === "legacy" ? false : originalState);

      const retried = installPackage(options);
      assert.equal(retried.updated, true);
      assert.equal(retried.authorizationRequired, stateKind === "pending");
      assert.equal(JSON.parse(readFileSync(retried.installStatePath, "utf8")).installedVersion, "9.8.7");
      assert.deepEqual(readdirSync(nativeRoot).sort(), [...skillNames].sort());
    });
  }
});

/**
 * 验证首次安装的第二次状态提交失败时撤回宿主写入，但保留首次授权意图供重试。
 * 入参：t（TestContext）为临时目录清理上下文。
 * 返回值：void，新建目录/链接残留或重试跳过首次授权时断言失败。
 */
test("首次安装最终提交失败保留门禁并撤回三宿主写入", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const nativeRoot = join(fixture.userHome, "workspace", ".user_skills");
  const options = { packageRoot: fixture.packageRoot, platform: "linux", architecture: "x64", userHome: fixture.userHome, environment: { npm_config_global: "true", EVERYLINE_DOUBAO_SKILLS_DIR: nativeRoot } };
  failInstallStateCommit(options, 2);
  for (const root of [nativeRoot, join(fixture.userHome, ".agents", "skills"), join(fixture.userHome, ".workbuddy", "skills")]) {
    assert.deepEqual(readdirSync(root), []);
  }
  const statePath = join(fixture.userHome, ".everyline-cli", "install-state.json");
  const pending = JSON.parse(readFileSync(statePath, "utf8"));
  assert.equal(pending.authorizationRequired, true);
  const retried = installPackage(options);
  assert.equal(retried.firstInstall, true);
  assert.equal(retried.authorizationRequired, true);
  assert.equal(retried.updated, false);
  assert.equal(JSON.parse(readFileSync(statePath, "utf8")).eventId, pending.eventId);
});

/**
 * 验证旧 ZIP 的 shared 普通目录迁出后只保留两项当前技能，备份完整且重试不重复提示重载。
 * 入参：t（TestContext）为仅旧入口及旧两项布局子场景上下文。
 * 返回值：Promise<void>，残留旧入口、备份丢失或旧安装被误判为首次安装时断言失败。
 */
test("豆包旧 shared 目录迁出扫描范围并保留备份", async (t) => {
  for (const layout of ["shared-only", "legacy-three", "current-three"]) {
    await t.test(layout, (t) => {
      const fixture = createPackageFixture();
      t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
      const nativeRoot = join(fixture.userHome, "workspace", ".user_skills");
      const names = layout === "shared-only" ? ["everyline-shared"] : layout === "legacy-three" ? ["everyline-shared", "everyline-review", "everyline-review-config"] : ["everyline-shared", ...skillNames];
      for (const name of names) {
        mkdirSync(join(nativeRoot, name), { recursive: true });
        writeFileSync(join(nativeRoot, name, "SKILL.md"), name === "everyline-shared" ? "---\nname: everyline-shared\n---\nold public skill" : readFileSync(join(fixture.packageRoot, "skills", name, "SKILL.md")));
      }
      const options = { packageRoot: fixture.packageRoot, platform: "linux", architecture: "x64", userHome: fixture.userHome, environment: { npm_config_global: "true", EVERYLINE_DOUBAO_SKILLS_DIR: nativeRoot } };
      const result = installPackage(options);
      assert.equal(result.updated, true);
      assert.equal(result.authorizationRequired, false);
      assert.equal(result.doubaoSkillReloadRequired, true);
      assert.deepEqual(result.skills.doubao.map(({ name }) => name), skillNames);
      assert.deepEqual(readdirSync(nativeRoot).sort(), [...skillNames].sort());
      const backups = readdirSync(join(nativeRoot, "..")).filter((name) => name.startsWith(".everyline-skill-update-"));
      assert.equal(backups.length, 1);
      assert.match(readFileSync(join(nativeRoot, "..", backups[0], "previous", "SKILL.md"), "utf8"), /old public skill/);
      const repeated = installPackage(options);
      assert.equal(repeated.updated, false);
      assert.equal(repeated.doubaoSkillReloadRequired, false);
    });
  }
});

/**
 * 验证未确认身份的豆包 shared 目录保持不动，也不充当升级证据。
 * 入参：t（TestContext）为缺失、重复或其他名称声明的子场景上下文。
 * 返回值：Promise<void>，用户内容被迁出或首次授权被跳过时断言失败。
 */
test("豆包旧入口迁移保留身份不明的用户目录", async (t) => {
  for (const content of ["user data", "---\nname: my-skill\n---\n", "---\nname: everyline-shared\nname: other\n---\n"]) {
    await t.test(content.split("\n").join(" "), (t) => {
      const fixture = createPackageFixture();
      t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
      const nativeRoot = join(fixture.userHome, "workspace", ".user_skills");
      const target = join(nativeRoot, "everyline-shared");
      mkdirSync(target, { recursive: true });
      writeFileSync(join(target, "SKILL.md"), content);
      const result = installPackage({ packageRoot: fixture.packageRoot, platform: "linux", architecture: "x64", userHome: fixture.userHome, environment: { npm_config_global: "true", EVERYLINE_DOUBAO_SKILLS_DIR: nativeRoot } });
      assert.equal(readFileSync(join(target, "SKILL.md"), "utf8"), content);
      assert.equal(result.firstInstall, true);
      assert.equal(result.authorizationRequired, true);
    });
  }
});

test("Codex Skill 登记可重复执行且始终指向包内同一来源", (t) => {
  const fixture = createFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));

  assert.equal(registerCodexSkill(fixture.source, fixture.target), "created");
  assert.equal(lstatSync(fixture.target).isSymbolicLink(), true);
  assert.equal(realpathSync(fixture.target), realpathSync(fixture.source));
  assert.equal(registerCodexSkill(fixture.source, fixture.target), "existing");
});

test("Codex Skill 登记保留用户已有的同名目录", (t) => {
  const fixture = createFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  mkdirSync(fixture.target, { recursive: true });
  const marker = join(fixture.target, "user-file.txt");
  writeFileSync(marker, "preserve");

  assert.throws(() => registerCodexSkill(fixture.source, fixture.target), /目标已存在/);
  assert.equal(existsSync(marker), true);
});

/**
 * 验证全局安装登记两项 Skill，并在文本和事件中原样提供首次安装与授权帮助文案。
 * 入参：t（TestContext），用于登记临时安装目录的清理操作。
 * 返回值：void，登记结果或授权提示不符合预期时断言失败。
 */
test("全局安装同步登记 Codex 与 WorkBuddy 的两项 Skill", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));

  const result = installPackage({
    packageRoot: fixture.packageRoot,
    platform: "linux",
    architecture: "x64",
    environment: { npm_config_global: "true", EVERYLINE_DOUBAO_SKILLS_DIR: join(fixture.root, "doubao-skills") },
    userHome: fixture.userHome,
  });

  assert.deepEqual(skillNames, ["everyline-review", "everyline-review-config"]);
  assert.deepEqual(result.skills.doubao.map((skill) => skill.name), skillNames);
  for (const root of [join(fixture.userHome, ".agents", "skills"), join(fixture.userHome, ".workbuddy", "skills"), join(fixture.root, "doubao-skills")]) {
    assert.deepEqual(readdirSync(root).sort(), [...skillNames].sort());
  }
  assert.deepEqual(result.skills.codex.map((skill) => skill.name), skillNames);
  assert.deepEqual(result.skills.workBuddy.map((skill) => skill.name), skillNames);
  for (const hostSkills of [result.skills.codex, result.skills.workBuddy]) {
    for (const skill of hostSkills) {
      assert.equal(lstatSync(skill.target).isSymbolicLink(), true);
      assert.equal(realpathSync(skill.target), realpathSync(join(fixture.packageRoot, "skills", skill.name)));
    }
  }
  assert.equal(result.firstInstall, true);
  assert.equal(result.updated, false);
  assert.equal(result.authorizationRequired, true);
  assert.equal(result.nextAction, "authorize");
  const state = JSON.parse(readFileSync(result.installStatePath, "utf8"));
  assert.equal(state.schema, "everyline.install-state.v1");
  assert.equal(state.installedVersion, "9.8.7");
  assert.equal(state.authorizationRequired, true);
  assert.match(state.eventId, /^[0-9a-f-]+$/);

  const output = formatInstallOutput(result);
  const expectedMessage = "EveryLine CLI 已安装完成。目前支持合同审查，以及审查清单、规则和规则分组配置。使用前需要先完成账号授权，我现在可以为你打开授权页面或生成授权链接。";
  const outputLines = output.trim().split("\n");
  assert.equal(outputLines[0], expectedMessage);
  assert.equal(JSON.parse(outputLines.at(-1)).message, expectedMessage);
  assert.equal(JSON.parse(outputLines.at(-1)).recommendedSkill, "everyline-review");
});

test("升级安装以 everyline-review 替换本包遗留的 everyline-shared 链接", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const codexSkillRoot = join(fixture.root, "codex-skills");
  const workBuddySkillRoot = join(fixture.root, "workbuddy-skills");
  const deprecatedSource = join(fixture.packageRoot, "skills", "everyline-shared");

  // 模拟旧版本留下的链接；升级包已经删除源目录，所以这里刻意创建悬空链接。
  for (const skillRoot of [codexSkillRoot, workBuddySkillRoot]) {
    mkdirSync(skillRoot, { recursive: true });
    symlinkSync(deprecatedSource, join(skillRoot, "everyline-shared"), "dir");
  }

  const result = installPackage({
    packageRoot: fixture.packageRoot,
    platform: "linux",
    architecture: "x64",
    environment: {
      npm_config_global: "true",
      EVERYLINE_CODEX_SKILLS_DIR: codexSkillRoot,
      EVERYLINE_WORKBUDDY_SKILLS_DIR: workBuddySkillRoot,
    },
    userHome: fixture.userHome,
  });

  assert.deepEqual(result.skills.codex.map((skill) => skill.name), skillNames);
  for (const skillRoot of [codexSkillRoot, workBuddySkillRoot]) {
    assert.throws(() => lstatSync(join(skillRoot, "everyline-shared")), /ENOENT/);
    assert.equal(lstatSync(join(skillRoot, "everyline-review")).isSymbolicLink(), true);
  }
  assert.equal(result.firstInstall, false);
  assert.equal(result.authorizationRequired, false);
  assert.equal(result.updated, true);
});

test("无状态的旧安装迁移为更新，保留凭证且同版本重装不重复迁移", async (t) => {
  const legacySkills = ["everyline-shared", "everyline-review", "everyline-review-config"];
  const cases = [
    { name: "两个宿主均有旧版登记", hosts: [".agents", ".workbuddy"], names: legacySkills },
    { name: "仅 Codex 有旧版登记", hosts: [".agents"], names: legacySkills },
    { name: "仅 WorkBuddy 有旧版登记", hosts: [".workbuddy"], names: legacySkills },
    { name: "旧入口已移除但审查 Skill 仍在", hosts: [".agents"], names: ["everyline-review", "everyline-review-config"] },
    { name: "两项当前 Skill 均已存在", hosts: [".agents", ".workbuddy"], names: skillNames },
  ];
  for (const scenario of cases) {
    await t.test(scenario.name, (t) => {
      const fixture = createPackageFixture();
      t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
      // 旧包没有安装状态；已删除的 everyline-shared 源目录留下悬空链接。
      for (const host of scenario.hosts) {
        const skillRoot = join(fixture.userHome, host, "skills");
        mkdirSync(skillRoot, { recursive: true });
        for (const name of scenario.names) {
          symlinkSync(join(fixture.packageRoot, "skills", name), join(skillRoot, name), "dir");
        }
      }
      const configDirectory = join(fixture.userHome, ".everyline-cli");
      const tokenPath = join(configDirectory, "tokens.json");
      const tokenContent = '{"dev::user":{"access_token":"existing-user-token"}}\n';
      mkdirSync(configDirectory, { recursive: true });
      writeFileSync(tokenPath, tokenContent);
      const options = {
        packageRoot: fixture.packageRoot,
        platform: "linux",
        architecture: "x64",
        environment: { npm_config_global: "true" },
        userHome: fixture.userHome,
      };

      const result = installPackage(options);

      assert.equal(result.updated, true);
      assert.equal(result.firstInstall, false);
      assert.equal(result.authorizationRequired, false);
      assert.equal(result.nextAction, "");
      assert.equal(readFileSync(tokenPath, "utf8"), tokenContent);
      const state = JSON.parse(readFileSync(result.installStatePath, "utf8"));
      assert.equal(state.installedVersion, "9.8.7");
      assert.equal(state.firstInstall, false);
      assert.equal(state.authorizationRequired, false);
      assert.equal(state.nextAction, "");
      const event = JSON.parse(formatInstallOutput(result).trim().split("\n").at(-1));
      assert.equal(event.event, "updated");
      assert.equal(event.authCheckRequired, true);
      assert.equal(event.nextAction, "auth_status");

      const repeated = installPackage(options);
      assert.equal(repeated.updated, false);
      assert.equal(repeated.authorizationRequired, false);
      assert.equal(JSON.parse(readFileSync(repeated.installStatePath, "utf8")).eventId, state.eventId);
    });
  }
});

test("升级安装保留用户自建的 everyline-shared 目录", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const codexSkillRoot = join(fixture.root, "codex-skills");
  const userSkill = join(codexSkillRoot, "everyline-shared");
  const marker = join(userSkill, "user-file.txt");
  mkdirSync(userSkill, { recursive: true });
  writeFileSync(marker, "preserve");

  const result = installPackage({
    packageRoot: fixture.packageRoot,
    platform: "linux",
    architecture: "x64",
    environment: {
      npm_config_global: "true",
      EVERYLINE_CODEX_SKILLS_DIR: codexSkillRoot,
      EVERYLINE_SKIP_WORKBUDDY_SKILL_INSTALL: "1",
    },
    userHome: fixture.userHome,
  });

  assert.equal(readFileSync(marker, "utf8"), "preserve");
  assert.equal(result.firstInstall, true);
  assert.equal(result.authorizationRequired, true);
});

test("其他来源的旧 Skill 链接不作为本包升级依据", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const skillRoot = join(fixture.userHome, ".agents", "skills");
  mkdirSync(skillRoot, { recursive: true });
  const target = join(skillRoot, "everyline-shared");
  symlinkSync(join(fixture.root, "another-package", "skills", "everyline-shared"), target, "dir");

  const result = installPackage({
    packageRoot: fixture.packageRoot,
    platform: "linux",
    architecture: "x64",
    environment: { npm_config_global: "true" },
    userHome: fixture.userHome,
  });

  assert.equal(lstatSync(target).isSymbolicLink(), true);
  assert.equal(result.updated, false);
  assert.equal(result.firstInstall, true);
  assert.equal(result.authorizationRequired, true);
});

test("首次安装保留旧 token 但仍要求完成一次新授权", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const configDirectory = join(fixture.userHome, ".everyline-cli");
  const tokenPath = join(configDirectory, "tokens.json");
  mkdirSync(configDirectory, { recursive: true });
  writeFileSync(tokenPath, '{"tokens":{"dev::user":{"access_token":"old-dev-token"}}}\n');

  const result = installPackage({
    packageRoot: fixture.packageRoot,
    platform: "linux",
    architecture: "x64",
    environment: { npm_config_global: "true" },
    userHome: fixture.userHome,
  });

  assert.equal(result.authorizationRequired, true);
  assert.match(readFileSync(tokenPath, "utf8"), /old-dev-token/);
});

/**
 * 验证首次安装状态写入失败后，修复文件系统并重试仍要求新授权。
 * 入参：t（TestContext）为临时目录清理上下文；夹具使用真实原生 CLI 写入状态。
 * 返回值：void，失败后遗留链接或重试被误判为升级时断言失败。
 */
test("首次安装状态写入失败后重试仍要求新授权", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const configDirectory = join(fixture.userHome, ".everyline-cli");
  mkdirSync(fixture.userHome, { recursive: true });
  // 用普通文件占据配置目录，确定性触发原生状态登记失败，不依赖当前用户的权限。
  writeFileSync(configDirectory, "blocked");
  const options = { packageRoot: fixture.packageRoot, platform: "linux", architecture: "x64", environment: { npm_config_global: "true" }, userHome: fixture.userHome };
  assert.throws(() => installPackage(options));
  for (const host of [".agents", ".workbuddy"]) {
    for (const name of skillNames) {
      assert.throws(() => lstatSync(join(fixture.userHome, host, "skills", name)), /ENOENT/);
    }
  }
  rmSync(configDirectory);

  const retried = installPackage(options);
  assert.equal(retried.firstInstall, true);
  assert.equal(retried.authorizationRequired, true);
  assert.equal(retried.updated, false);
  assert.equal(retried.nextAction, "authorize");
});

/**
 * 验证写入部分 Skill 后进程中断，已持久化的首次安装意图仍约束后续重试。
 * 入参：t（TestContext）为隔离目录清理上下文；子进程在第二次写链接前退出以模拟无回滚中断。
 * 返回值：void，重试丢失原授权事件、解除门禁或未补齐链接时断言失败。
 */
test("首次安装中断留下部分链接时重试保留原授权事件", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const options = { packageRoot: fixture.packageRoot, platform: "linux", architecture: "x64", environment: { npm_config_global: "true" }, userHome: fixture.userHome };
  assert.throws(() => execFileSync(process.execPath, ["-e", `
const fs = require('node:fs');
const original = fs.symlinkSync;
let calls = 0;
fs.symlinkSync = (...args) => {
  if (++calls === 2) process.exit(23);
  return original(...args);
};
require(process.argv[1]).installPackage(JSON.parse(process.argv[2]));
`, join(__dirname, "../../scripts/install.js"), JSON.stringify(options)]), (error) => error.status === 23);
  assert.equal(lstatSync(join(fixture.userHome, ".agents", "skills", skillNames[0])).isSymbolicLink(), true);
  const statePath = join(fixture.userHome, ".everyline-cli", "install-state.json");
  const pending = JSON.parse(readFileSync(statePath, "utf8"));
  assert.equal(pending.authorizationRequired, true);

  const retried = installPackage(options);
  assert.equal(retried.firstInstall, true);
  assert.equal(retried.authorizationRequired, true);
  assert.equal(retried.updated, false);
  assert.equal(JSON.parse(readFileSync(statePath, "utf8")).eventId, pending.eventId);
  for (const host of [".agents", ".workbuddy"]) {
    assert.deepEqual(readdirSync(join(fixture.userHome, host, "skills")).sort(), [...skillNames].sort());
  }
});

test("相对 EVERYLINE_CONFIG_DIR 始终按用户目录解析", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));

  const result = installPackage({
    packageRoot: fixture.packageRoot,
    platform: "linux",
    architecture: "x64",
    environment: {
      npm_config_global: "true",
      EVERYLINE_CONFIG_DIR: join("settings", "everyline"),
    },
    userHome: fixture.userHome,
  });

  assert.equal(result.installStatePath, join(fixture.userHome, "settings", "everyline", "install-state.json"));
  assert.equal(existsSync(result.installStatePath), true);
});

test("重复安装不会重新打开已经完成的首次授权门禁", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const options = {
    packageRoot: fixture.packageRoot,
    platform: "linux",
    architecture: "x64",
    environment: { npm_config_global: "true" },
    userHome: fixture.userHome,
  };
  const first = installPackage(options);
  const completed = JSON.parse(readFileSync(first.installStatePath, "utf8"));
  completed.firstInstall = false;
  completed.authorizationRequired = false;
  completed.nextAction = "";
  writeFileSync(first.installStatePath, `${JSON.stringify(completed, null, 2)}\n`);

  const second = installPackage(options);

  assert.equal(second.firstInstall, false);
  assert.equal(second.updated, false);
  assert.equal(second.authorizationRequired, false);
  assert.equal(second.nextAction, "");
  assert.equal(JSON.parse(readFileSync(second.installStatePath, "utf8")).eventId, completed.eventId);
});

test("已有待授权状态不被旧版登记迁移解除", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const options = {
    packageRoot: fixture.packageRoot,
    platform: "linux",
    architecture: "x64",
    environment: { npm_config_global: "true" },
    userHome: fixture.userHome,
  };
  const first = installPackage(options);
  const pending = readFileSync(first.installStatePath, "utf8");
  symlinkSync(
    join(fixture.packageRoot, "skills", "everyline-shared"),
    join(fixture.userHome, ".agents", "skills", "everyline-shared"),
    "dir",
  );

  const repeated = installPackage(options);

  assert.equal(repeated.updated, false);
  assert.equal(repeated.firstInstall, true);
  assert.equal(repeated.authorizationRequired, true);
  assert.equal(readFileSync(repeated.installStatePath, "utf8"), pending);
});

/**
 * 验证升级事件要求检查真实授权状态，并原样提供授权失效和有效时的提示文案。
 * 入参：t（TestContext），用于登记临时安装目录的清理操作。
 * 返回值：void，事件动作或状态分支文案不符合预期时断言失败。
 */
test("升级安装要求 Agent 按真实授权状态选择提示文案", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const options = {
    packageRoot: fixture.packageRoot,
    platform: "linux",
    architecture: "x64",
    environment: { npm_config_global: "true" },
    userHome: fixture.userHome,
  };
  const first = installPackage(options);
  const previous = JSON.parse(readFileSync(first.installStatePath, "utf8"));
  previous.installedVersion = "9.8.6";
  previous.firstInstall = false;
  previous.authorizationRequired = false;
  previous.nextAction = "";
  writeFileSync(first.installStatePath, `${JSON.stringify(previous, null, 2)}\n`);

  const updated = installPackage(options);

  assert.equal(updated.updated, true);
  assert.equal(JSON.parse(readFileSync(updated.installStatePath, "utf8")).installedVersion, "9.8.7");
  const lines = formatInstallOutput(updated).trim().split("\n");
  assert.equal(lines[0], "EveryLine CLI 已更新完成。目前支持合同审查，以及审查清单、规则和规则分组配置。");
  const event = JSON.parse(lines.at(-1));
  assert.equal(event.event, "updated");
  assert.equal(event.authCheckRequired, true);
  assert.equal(event.nextAction, "auth_status");
  assert.equal(event.authorizationRequiredMessage, "使用前需要先完成账号授权，我现在可以为你打开授权页面或生成授权链接。");
  assert.equal(event.authorizedMessage, "当前已存在生效授权，可直接调用cli能力；");
});

test("全局安装后置目标冲突时不留下部分 Skill 链接", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
  const codexSkillRoot = join(fixture.root, "codex-skills");
  const workBuddySkillRoot = join(fixture.root, "workbuddy-skills");
  const conflictTarget = join(workBuddySkillRoot, "everyline-review");
  const marker = join(conflictTarget, "user-file.txt");
  mkdirSync(conflictTarget, { recursive: true });
  writeFileSync(marker, "preserve");

  assert.throws(() => installPackage({
    packageRoot: fixture.packageRoot,
    platform: "linux",
    architecture: "x64",
    environment: {
      npm_config_global: "true",
      EVERYLINE_CODEX_SKILLS_DIR: codexSkillRoot,
      EVERYLINE_WORKBUDDY_SKILLS_DIR: workBuddySkillRoot,
    },
    userHome: fixture.userHome,
  }), /目标已存在/);

  for (const name of skillNames) {
    assert.equal(existsSync(join(codexSkillRoot, name)), false);
  }
  assert.equal(existsSync(join(workBuddySkillRoot, "everyline-cli")), false);
  assert.equal(existsSync(marker), true);
  assert.equal(existsSync(join(workBuddySkillRoot, "everyline-review-config")), false);
});

/**
 * 验证跨 Node/npm 来源安装直接迁移两项 Skill，保留授权状态且不创建可被扫描的备份。
 * 入参：t（TestContext）为隔离目录清理及子场景上下文。
 * 返回值：void，链接、升级标记或授权状态不符合预期时断言失败。
 */
test("跨 Node 环境安装迁移两个宿主的链接且保持安装状态", async (t) => {
  for (const stateKind of ["legacy", "authorized", "pending"]) {
    await t.test(stateKind, (t) => {
      const previous = createPackageFixture();
      const current = createPackageFixture();
      t.after(() => {
        rmSync(previous.root, { recursive: true, force: true });
        rmSync(current.root, { recursive: true, force: true });
      });
      const options = { packageRoot: previous.packageRoot, platform: "linux", architecture: "x64", environment: { npm_config_global: "true" }, userHome: previous.userHome };
      const original = installPackage(options);
      const state = JSON.parse(readFileSync(original.installStatePath, "utf8"));
      if (stateKind === "legacy") {
        rmSync(original.installStatePath);
      } else if (stateKind === "authorized") {
        Object.assign(state, { firstInstall: false, authorizationRequired: false, nextAction: "" });
        writeFileSync(original.installStatePath, JSON.stringify(state));
      }
      const manifestPath = join(current.packageRoot, "package.json");
      const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
      manifest.version = "9.8.8";
      writeFileSync(manifestPath, JSON.stringify(manifest));
      options.packageRoot = current.packageRoot;
      const migrated = installPackage(options);
      assert.equal(migrated.updated, true);
      assert.equal(migrated.firstInstall, stateKind === "pending");
      assert.equal(migrated.authorizationRequired, stateKind === "pending");
      for (const hostSkills of Object.values(migrated.skills)) {
        for (const skill of hostSkills) {
          assert.equal(skill.status, "updated");
          assert.equal(realpathSync(skill.target), realpathSync(join(current.packageRoot, "skills", skill.name)));
        }
      }
      for (const root of [join(previous.userHome, ".agents", "skills"), join(previous.userHome, ".workbuddy", "skills")]) {
        assert.deepEqual(readdirSync(root).sort(), [...skillNames].sort());
      }
      const repeated = installPackage(options);
      assert.equal(repeated.updated, false);
      assert.equal(repeated.authorizationRequired, migrated.authorizationRequired);
      if (stateKind !== "legacy") {
        assert.equal(JSON.parse(readFileSync(repeated.installStatePath, "utf8")).eventId, state.eventId);
      }
    });
  }
});

/**
 * 验证旧两项布局跨 Node 前缀升级时清理 shared，且保留旧 token 和升级语义。
 * 入参：t（TestContext）为各旧版布局的子测试及临时目录清理上下文。
 * 返回值：Promise<void>，遗留废弃链接、强制重新授权或覆盖 token 时断言失败。
 */
test("跨 Node 升级清理旧 shared 布局并保留授权", async (t) => {
  for (const layout of ["legacy-three", "shared-only", "dangling-shared"]) {
    await t.test(layout, (t) => {
      const previous = createPackageFixture();
      const current = createPackageFixture();
      t.after(() => {
        rmSync(previous.root, { recursive: true, force: true });
        rmSync(current.root, { recursive: true, force: true });
      });
      const sharedSource = join(previous.packageRoot, "skills", "everyline-shared");
      if (layout !== "dangling-shared") {
        mkdirSync(sharedSource);
        writeFileSync(join(sharedSource, "SKILL.md"), "---\nname: everyline-shared\n---\n");
      }
      const legacyNames = layout === "shared-only" ? ["everyline-shared"] : ["everyline-shared", "everyline-review", "everyline-review-config"];
      for (const host of [".agents", ".workbuddy"]) {
        const skillRoot = join(previous.userHome, host, "skills");
        mkdirSync(skillRoot, { recursive: true });
        for (const name of legacyNames) {
          symlinkSync(join(previous.packageRoot, "skills", name), join(skillRoot, name), "dir");
        }
      }
      const configDirectory = join(previous.userHome, ".everyline-cli");
      mkdirSync(configDirectory, { recursive: true });
      const tokenPath = join(configDirectory, "tokens.json");
      const tokenContent = '{"tokens":{"test::user":{"access_token":"fixture-existing-token"}}}\n';
      writeFileSync(tokenPath, tokenContent);
      const result = installPackage({ packageRoot: current.packageRoot, platform: "linux", architecture: "x64", environment: { npm_config_global: "true" }, userHome: previous.userHome });
      assert.equal(result.updated, true);
      assert.equal(result.firstInstall, false);
      assert.equal(result.authorizationRequired, false);
      assert.equal(readFileSync(tokenPath, "utf8"), tokenContent);
      for (const host of [".agents", ".workbuddy"]) {
        const skillRoot = join(previous.userHome, host, "skills");
        assert.deepEqual(readdirSync(skillRoot).sort(), [...skillNames].sort());
        for (const name of skillNames) {
          assert.equal(realpathSync(join(skillRoot, name)), realpathSync(join(current.packageRoot, "skills", name)));
        }
      }
    });
  }
});

/**
 * 验证跨目录 shared 清理不会接管包归属不明或入口不匹配的链接。
 * 入参：t（TestContext）为 manifest 异常子场景及临时文件清理上下文。
 * 返回值：Promise<void>，未知来源链接被删除或替换时断言失败。
 */
test("旧 shared 清理保留未确认归属的链接", async (t) => {
  const manifests = {
    foreign: '{"name":"another-package","bin":{"everyline-cli":"scripts/run.js"}}',
    "wrong-entry": '{"name":"everyline-cli","bin":{"everyline-cli":"custom.js"}}',
    missing: undefined,
    invalid: '{',
    null: 'null',
  };
  for (const [kind, manifest] of Object.entries(manifests)) {
    await t.test(kind, (t) => {
      const fixture = createPackageFixture();
      t.after(() => rmSync(fixture.root, { recursive: true, force: true }));
      const oldPackage = join(fixture.root, "old-package");
      const source = join(oldPackage, "skills", "everyline-shared");
      const skillRoot = join(fixture.userHome, ".agents", "skills");
      mkdirSync(source, { recursive: true });
      mkdirSync(skillRoot, { recursive: true });
      if (manifest !== undefined) writeFileSync(join(oldPackage, "package.json"), manifest);
      const target = join(skillRoot, "everyline-shared");
      symlinkSync(source, target, "dir");
      const originalLink = readlinkSync(target);

      assert.deepEqual(removeDeprecatedSkillRegistrations(fixture.packageRoot, skillRoot), []);
      assert.equal(readlinkSync(target), originalLink);
      assert.equal(realpathSync(target), realpathSync(source));
    });
  }
});

/**
 * 验证悬空旧链接只在 npm 包归属可确认时迁移；其他来源保持原样并提示外部备份。
 * 入参：t（TestContext）为子场景及临时文件清理上下文。
 * 返回值：void，错误接管非 EveryLine 链接或遗漏可恢复旧链接时断言失败。
 */
test("旧链接迁移校验包归属并兼容已移除的 Skill 文件", async (t) => {
  for (const kind of ["owned-dangling", "foreign", "missing-manifest", "wrong-entry", "invalid-manifest", "null-manifest"]) {
    await t.test(kind, (t) => {
      const previous = createPackageFixture();
      const current = createFixture();
      t.after(() => {
        rmSync(previous.root, { recursive: true, force: true });
        rmSync(current.root, { recursive: true, force: true });
      });
      const oldSource = join(previous.packageRoot, "skills", "everyline-review");
      const manifestPath = join(previous.packageRoot, "package.json");
      if (kind === "owned-dangling") rmSync(oldSource, { recursive: true });
      if (kind === "foreign") writeFileSync(manifestPath, '{"name":"another-package","bin":{"everyline-cli":"scripts/run.js"}}');
      if (kind === "missing-manifest") rmSync(manifestPath);
      if (kind === "wrong-entry") writeFileSync(manifestPath, '{"name":"everyline-cli","bin":{"everyline-cli":"custom.js"}}');
      if (kind === "invalid-manifest") writeFileSync(manifestPath, '{');
      if (kind === "null-manifest") writeFileSync(manifestPath, 'null');
      mkdirSync(join(current.root, "codex", "skills"), { recursive: true });
      symlinkSync(oldSource, current.target, "dir");
      if (kind === "owned-dangling") {
        assert.equal(registerCodexSkill(current.source, current.target), "updated");
        assert.equal(realpathSync(current.target), realpathSync(current.source));
      } else {
        assert.throws(() => registerCodexSkill(current.source, current.target), /Skill 扫描目录之外/);
        assert.equal(realpathSync(current.target), realpathSync(oldSource));
      }
    });
  }
});

/**
 * 验证跨来源迁移中途创建链接失败时，已更新的链接及失败项都恢复原来源。
 * 入参：t（TestContext）为临时文件清理上下文；子进程仅注入一次文件创建失败。
 * 返回值：void，失败后残留新链接、丢失旧链接或产生重复 Skill 时断言失败。
 */
test("跨来源迁移失败回滚两个宿主的旧链接", (t) => {
  const previous = createPackageFixture();
  const current = createPackageFixture();
  t.after(() => {
    rmSync(previous.root, { recursive: true, force: true });
    rmSync(current.root, { recursive: true, force: true });
  });
  const options = { packageRoot: previous.packageRoot, platform: "linux", architecture: "x64", environment: { npm_config_global: "true" }, userHome: previous.userHome };
  const original = installPackage(options);
  const originalState = readFileSync(original.installStatePath, "utf8");
  options.packageRoot = current.packageRoot;
  // 新进程在加载安装器前注入故障，不影响其他测试中已加载的 fs 函数。
  execFileSync(process.execPath, ["-e", `
const fs = require('node:fs');
const assert = require('node:assert/strict');
const original = fs.symlinkSync;
let calls = 0;
let failed = false;
fs.symlinkSync = (...args) => {
  if (++calls === 4) { failed = true; throw new Error('fixture link failure'); }
  return original(...args);
};
const { installPackage } = require(process.argv[1]);
assert.throws(() => installPackage(JSON.parse(process.argv[2])), /fixture link failure/);
assert.equal(failed, true);
`, join(__dirname, "../../scripts/install.js"), JSON.stringify(options)]);
  for (const hostSkills of Object.values(original.skills)) {
    for (const skill of hostSkills) {
      assert.equal(realpathSync(skill.target), realpathSync(join(previous.packageRoot, "skills", skill.name)));
    }
  }
  for (const root of [join(previous.userHome, ".agents", "skills"), join(previous.userHome, ".workbuddy", "skills")]) {
    assert.deepEqual(readdirSync(root).sort(), [...skillNames].sort());
  }
  assert.equal(readFileSync(original.installStatePath, "utf8"), originalState);
});

/**
 * 验证带 scope 的安装包能迁移旧包或旧安装目录的 Skill 链接，并保留授权状态。
 * 入参：t（TestContext）提供子场景与临时目录清理。
 * 返回值：void；包名改动导致来源误判、链接未更新或授权状态丢失时断言失败。
 */
test("scoped package migrates legacy and scoped Skill registrations", async (t) => {
  for (const previousName of ["everyline-cli", "@qfeius/everyline-cli"]) {
    await t.test(previousName, (t) => {
      const previous = createPackageFixture();
      const current = createPackageFixture();
      t.after(() => {
        rmSync(previous.root, { recursive: true, force: true });
        rmSync(current.root, { recursive: true, force: true });
      });
      for (const [fixture, name] of [[previous, previousName], [current, "@qfeius/everyline-cli"]]) {
        const manifestPath = join(fixture.packageRoot, "package.json");
        const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
        manifest.name = name;
        writeFileSync(manifestPath, JSON.stringify(manifest));
      }
      const options = { platform: "linux", architecture: "x64", environment: { npm_config_global: "true" }, userHome: previous.userHome };
      const before = installPackage({ ...options, packageRoot: previous.packageRoot });
      const state = JSON.parse(readFileSync(before.installStatePath, "utf8"));
      state.authorizationRequired = false;
      writeFileSync(before.installStatePath, JSON.stringify(state));
      const after = installPackage({ ...options, packageRoot: current.packageRoot });
      assert.equal(after.authorizationRequired, false);
      for (const registrations of Object.values(after.skills)) {
        for (const skill of registrations) {
          assert.equal(realpathSync(skill.target), realpathSync(join(current.packageRoot, "skills", skill.name)));
        }
      }
    });
  }
});

/**
 * 验证命令恢复遵守 bin-links 配置，且已有文件、局部安装和 Windows 不受影响。
 * 入参：t（TestContext）负责隔离目录的清理；返回值：void，行为偏离预期时断言失败。
 */
test("全局命令缺失时恢复入口且不覆盖已有文件", (t) => {
  const prefix = mkdtempSync(join(tmpdir(), "everyline-command-"));
  t.after(() => rmSync(prefix, { recursive: true, force: true }));
  const root = join(prefix, "lib", "node_modules", "@qfeius", "everyline-cli");
  const source = join(root, "scripts", "run.js");
  const target = join(prefix, "bin", "everyline-cli");
  mkdirSync(join(root, "scripts"), { recursive: true });
  writeFileSync(source, "#!/usr/bin/env node\nconsole.log('entry-ok');\n");
  restoreGlobalCommand(root, "linux", { npm_config_global: "true", npm_config_bin_links: "false" });
  assert.equal(existsSync(target), false);
  assert.equal(existsSync(join(prefix, "bin")), false);
  restoreGlobalCommand(root, "linux", {});
  restoreGlobalCommand(root, "win32", { npm_config_global: "true" });
  assert.equal(existsSync(target), false);
  restoreGlobalCommand(root, "linux", { npm_config_global: "true" });
  assert.equal(realpathSync(target), realpathSync(source));
  assert.equal(execFileSync(process.execPath, [target], { encoding: "utf8" }).trim(), "entry-ok");
  restoreGlobalCommand(root, "darwin", { npm_config_global: "true", npm_config_bin_links: "true" });
  assert.equal(realpathSync(target), realpathSync(source));
  rmSync(target);
  writeFileSync(target, "user-owned");
  restoreGlobalCommand(root, "linux", { npm_config_global: "true" });
  assert.equal(readFileSync(target, "utf8"), "user-owned");
});
