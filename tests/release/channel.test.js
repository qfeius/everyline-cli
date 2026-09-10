"use strict";

const assert = require("node:assert/strict");
const { execFileSync, spawnSync } = require("node:child_process");
const { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const test = require("node:test");
const { runInNewContext } = require("node:vm");
const versions = require("../../scripts/package-version");
const pkg = require("../../package.json");

/**
 * 验证实际 GitHub 发布前置脚本只接受与包一致的 blue 标签及渠道。
 * 入参：无；读取真实工作流并隔离文件写入与进程环境。
 * 返回值：void，错误渠道或版本通过发布检查、输出渠道错误时断言失败。
 */
test("发布前置检查只接受 blue 版本与渠道", () => {
  const workflow = readFileSync(join(__dirname, "../../.github/workflows/npm-publish.yml"), "utf8");
  const source = workflow.match(/node - "\$version" <<'NODE'\r?\n([\s\S]*?)\r?\n\s+NODE/)[1];
  assert.match(workflow, /printf 'dist_tag=blue\\n'/);
  const cases = [
    { tag: `v${pkg.version}`, version: pkg.version, channel: "blue", valid: true },
    { tag: "v0.1.10", version: "0.1.10", channel: "blue" },
    { tag: "v0.1.10-beta.0", version: "0.1.10-beta.0", channel: "blue" },
    { tag: `v${pkg.version}`, version: pkg.version, channel: "latest" },
    { tag: `v${pkg.version}`, version: pkg.version, channel: "beta" },
    { tag: `v${pkg.version}`, version: "0.1.11-blue.0", channel: "blue" },
    { tag: "v0.1.10-blue.0+build", version: "0.1.10-blue.0+build", channel: "blue" },
  ];
  for (const scenario of cases) {
    const run = () => runInNewContext(source, {
      process: { argv: ["node", "-", scenario.tag.slice(1)] },
      require: (name) => {
        if (name === "node:fs") return { readFileSync: () => JSON.stringify({ ...pkg, version: scenario.version, publishConfig: { tag: scenario.channel } }) };
        if (name === "./scripts/package-version") return versions;
        throw new Error(`未预期的依赖: ${name}`);
      },
    });
    if (scenario.valid) {
      assert.doesNotThrow(run);
    } else {
      assert.throws(run, /blue 发布|发布标签必须/);
    }
  }
});

/**
 * 验证 npm 源码发布生命周期拒绝其他环境，普通 CI 构建包仍可本地打包。
 * 入参：t（TestContext）负责隔离包根清理。
 * 返回值：void，发布绕过校验或 CI 制品被误拒绝时断言失败。
 */
test("npm 发布入口校验渠道且保留 CI 本地打包", (t) => {
  const root = mkdtempSync(join(tmpdir(), "everyline-channel-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "scripts"));
  for (const name of ["package-version.js", "verify-package-version.js"]) {
    copyFileSync(join(__dirname, "../../scripts", name), join(root, "scripts", name));
  }
  assert.equal(pkg.publishConfig.tag, "blue");
  assert.equal(pkg.scripts.prepublishOnly, "node scripts/verify-package-version.js --publish");
  for (const [version, tag, valid] of [[pkg.version, "blue", true], [pkg.version, "latest", false], ["0.1.10", "blue", false], ["0.0.0-build-abcdef", "blue", false]]) {
    writeFileSync(join(root, "package.json"), JSON.stringify({ version, publishConfig: { tag } }));
    const result = spawnSync(process.execPath, [join(root, "scripts/verify-package-version.js"), "--publish"], { encoding: "utf8" });
    assert.equal(result.status === 0, valid, result.stderr);
    // CI 本地打包不发布 npm，仍接受可追溯的 build 版本。
    assert.doesNotThrow(() => execFileSync(process.execPath, [join(root, "scripts/verify-package-version.js")]));
  }
});
