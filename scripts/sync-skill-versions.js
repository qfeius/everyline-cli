"use strict";
const { readFileSync, writeFileSync } = require("node:fs");
const { join } = require("node:path");
const { normalizePackageVersion } = require("./package-version");

/**
 * syncSkillVersions 将安装包版本写入两项 Skill 的 metadata。
 * 入参：root string 为包根目录。
 * 返回值：无；版本或 frontmatter 无效时抛出错误，阻止发布。
 */
function syncSkillVersions(root) {
  const { version } = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
  if (version === "0.0.0-development" || normalizePackageVersion(version) !== version) {
    throw new Error(`无效发布版本: ${version}`);
  }
  // 先验证全部文件再写入，避免其中一项格式异常造成部分同步。
  const changes = ["everyline-review", "everyline-review-config"].map(name => {
    const path = join(root, "skills", name, "SKILL.md");
    const source = readFileSync(path, "utf8");
    const match = source.match(/^---\r?\n([\s\S]*?)\r?\n---/);
    if (!match || !/^metadata:$/m.test(match[1])) throw new Error(`缺少 metadata: ${name}`);
    const header = match[1].replace(/^  version:.*\n?/m, "").replace(/^metadata:$/m, `metadata:\n  version: "${version}"`);
    return { path, source, content: source.replace(match[0], `---\n${header}\n---`) };
  });
  for (const { path, source, content } of changes) {
    if (content !== source) writeFileSync(path, content);
  }
}
if (require.main === module) syncSkillVersions(join(__dirname, ".."));
module.exports = { syncSkillVersions };
