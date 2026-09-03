"use strict";

const assert = require("node:assert/strict");
const { existsSync, lstatSync, mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } = require("node:fs");
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
