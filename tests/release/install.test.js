"use strict";

const assert = require("node:assert/strict");
const { existsSync, lstatSync, mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const test = require("node:test");
const { registerCodexSkill, shouldInstallCodexSkill } = require("../../scripts/install");

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

test("Codex Skill 只在 npm 全局安装时自动登记", () => {
  assert.equal(shouldInstallCodexSkill({ npm_config_global: "true" }), true);
  assert.equal(shouldInstallCodexSkill({ npm_config_global: "false" }), false);
  assert.equal(shouldInstallCodexSkill({ npm_config_global: "true", EVERYLINE_SKIP_SKILL_INSTALL: "1" }), false);
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
