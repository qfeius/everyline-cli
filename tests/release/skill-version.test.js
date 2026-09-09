"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const { syncSkillVersions } = require("../../scripts/sync-skill-versions");

// 验证连续发布会更新全部 Skill，且同步保留正文并拒绝非法版本。
test("skill versions follow package releases without changing body", () => {
  const root = mkdtempSync(join(tmpdir(), "everyline-skill-version-"));
  try {
    const names = ["everyline-review", "everyline-review-config"];
    for (const name of names) {
      mkdirSync(join(root, "skills", name), { recursive: true });
      writeFileSync(join(root, "skills", name, "SKILL.md"), '---\nname: '+name+'\nmetadata:\n  requires:\n    bins: ["everyline-cli"]\n---\nBody\n');
    }
    for (const version of ["1.2.0", "1.3.0-beta.1", "1.3.0-beta.1"]) {
      writeFileSync(join(root, "package.json"), JSON.stringify({ version }));
      syncSkillVersions(root);
      for (const name of names) {
        const content = readFileSync(join(root, "skills", name, "SKILL.md"), "utf8");
        assert.ok(content.includes(`  version: "${version}"`));
        assert.equal(content.match(/  version:/g).length, 1);
        assert.ok(content.endsWith('---\nBody\n'));
        assert.ok(content.includes('  requires:\n    bins: ["everyline-cli"]'));
      }
    }
    writeFileSync(join(root, "package.json"), JSON.stringify({ version: "invalid" }));
    assert.throws(() => syncSkillVersions(root), /无效发布版本/);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
