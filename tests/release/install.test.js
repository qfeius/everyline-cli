"use strict";

const assert = require("node:assert/strict");
const { existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const test = require("node:test");
const {
  installPackage,
  registerCodexSkill,
  shouldInstallCodexSkill,
  shouldInstallWorkBuddySkills,
  skillNames,
} = require("../../scripts/install");

/**
 * createFixture 创建隔离的 Skill 源目录和 Codex 目标目录。
 * 入参：无。
 * 返回值：object，包含临时根目录 root、源目录 source 和目标路径 target。
 */
function createFixture() {
  const root = mkdtempSync(join(tmpdir(), "everyline-install-"));
  const source = join(root, "package", "skills", "everyline-cli");
  const target = join(root, "codex", "skills", "everyline-cli");
  mkdirSync(source, { recursive: true });
  writeFileSync(join(source, "SKILL.md"), "---\nname: everyline-cli\n---\n");
  return { root, source, target };
}

/**
 * createPackageFixture 创建包含当前平台二进制和三项 Skill 的最小 npm 包夹具。
 * 入参：无。
 * 返回值：object，包含临时根目录 root、模拟包根 packageRoot 和用户目录 userHome。
 */
function createPackageFixture() {
  const root = mkdtempSync(join(tmpdir(), "everyline-package-install-"));
  const packageRoot = join(root, "package");
  const userHome = join(root, "home");
  const binary = join(packageRoot, "bin", "linux-amd64", "everyline-cli");
  mkdirSync(join(packageRoot, "bin", "linux-amd64"), { recursive: true });
  writeFileSync(binary, "fixture");
  writeFileSync(join(packageRoot, "package.json"), '{"name":"everyline-cli","version":"9.8.7"}\n');
  for (const name of skillNames) {
    const source = join(packageRoot, "skills", name);
    mkdirSync(source, { recursive: true });
    writeFileSync(join(source, "SKILL.md"), `---\nname: ${name}\n---\n`);
  }
  return { root, packageRoot, userHome };
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

test("全局安装同步登记 Codex 与 WorkBuddy 的三项 Skill", (t) => {
  const fixture = createPackageFixture();
  t.after(() => rmSync(fixture.root, { recursive: true, force: true }));

  const result = installPackage({
    packageRoot: fixture.packageRoot,
    platform: "linux",
    architecture: "x64",
    environment: { npm_config_global: "true" },
    userHome: fixture.userHome,
  });

  assert.deepEqual(result.skills.codex.map((skill) => skill.name), skillNames);
  assert.deepEqual(result.skills.workBuddy.map((skill) => skill.name), skillNames);
  for (const hostSkills of [result.skills.codex, result.skills.workBuddy]) {
    for (const skill of hostSkills) {
      assert.equal(lstatSync(skill.target).isSymbolicLink(), true);
      assert.equal(realpathSync(skill.target), realpathSync(join(fixture.packageRoot, "skills", skill.name)));
    }
  }
  assert.equal(result.firstInstall, true);
  assert.equal(result.authorizationRequired, true);
  assert.equal(result.nextAction, "authorize");
  const state = JSON.parse(readFileSync(result.installStatePath, "utf8"));
  assert.equal(state.schema, "everyline.install-state.v1");
  assert.equal(state.installedVersion, "9.8.7");
  assert.equal(state.authorizationRequired, true);
  assert.match(state.eventId, /^[0-9a-f-]+$/);
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
  assert.equal(second.authorizationRequired, false);
  assert.equal(second.nextAction, "");
  assert.equal(JSON.parse(readFileSync(second.installStatePath, "utf8")).eventId, completed.eventId);
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
  assert.equal(existsSync(join(workBuddySkillRoot, "everyline-shared")), false);
  assert.equal(existsSync(marker), true);
  assert.equal(existsSync(join(workBuddySkillRoot, "everyline-review-config")), false);
});
