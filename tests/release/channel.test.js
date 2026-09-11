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
 * 验证 GitHub 制品和 npm 发布的实际前置脚本只接受与包一致的 blue 标签及渠道。
 * 入参：无；读取真实工作流并隔离文件写入与进程环境。
 * 返回值：void，错误渠道或版本通过发布检查、输出渠道错误时断言失败。
 */
for (const workflowName of ["release.yml", "npm-publish.yml"]) {
test(`${workflowName} 发布前置检查只接受 blue 版本与渠道`, () => {
  const workflow = readFileSync(join(__dirname, "../../.github/workflows", workflowName), "utf8");
  const source = workflow.match(/node <<'NODE'\r?\n([\s\S]*?)\r?\n\s+NODE/)[1];
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
    let output = "";
    const run = () => runInNewContext(source, {
      process: { env: { GITHUB_REF_NAME: scenario.tag, GITHUB_OUTPUT: "fixture-output" } },
      require: (name) => {
        if (name === "node:fs") return { appendFileSync: (file, content) => { assert.equal(file, "fixture-output"); output += content; } };
        if (name === "./package.json") return { ...pkg, version: scenario.version, publishConfig: { tag: scenario.channel } };
        if (name === "./scripts/package-version") return versions;
        throw new Error(`未预期的依赖: ${name}`);
      },
    });
    if (scenario.valid) {
      run();
      assert.equal(output, `version=${pkg.version}\nchannel=blue\n`);
    } else {
      assert.throws(run, /blue 发布|发布标签必须/);
      assert.equal(output, "");
    }
  }
});
}

/**
 * 验证发布后校验执行真实工作流 shell，允许注册表短暂返回旧版或查询失败，并在重试耗尽后严格失败。
 * 入参：t（TestContext）负责清理临时桩脚本和调用记录；scenario 描述 npm 返回序列与预期重试次数。
 * 返回值：void，提前失败、无限重试、重复发布或等待次数错误时断言失败。
 */
for (const scenario of [
  { name: "先旧后新", responses: [{ version: "0.0.0-blue.0" }, { version: "0.0.0-blue.0" }, { version: pkg.version }], calls: 3, status: 0 },
  { name: "暂时查询失败后成功", responses: [{ status: 1 }, { version: pkg.version }], calls: 2, status: 0 },
  { name: "持续旧版最终失败", responses: [{ version: "0.0.0-blue.0" }], calls: 13, status: 1 },
]) {
test(`npm 发布后频道校验：${scenario.name}`, (t) => {
  const root = mkdtempSync(join(tmpdir(), "everyline-channel-retry-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const workflow = readFileSync(join(__dirname, "../../.github/workflows/npm-publish.yml"), "utf8");
  const block = workflow.match(/- name: Verify npm channel\r?\n[\s\S]*?\n        run: \|\r?\n((?:          [^\n]*(?:\n|$))*)/);
  assert.ok(block, "未找到真实频道校验脚本");
  const source = block[1].replace(/^          /gm, "");
  const stubPath = join(root, "commands.cjs");
  const logPath = join(root, "calls.ndjson");
  // shell 函数仅替换外部 npm/sleep，工作流循环、条件和退出码仍由真实 bash 执行。
  writeFileSync(stubPath, `
const { appendFileSync, existsSync, readFileSync } = require("node:fs");
const command = process.argv[2];
const calls = existsSync(process.env.STUB_LOG) ? readFileSync(process.env.STUB_LOG, "utf8").trim().split("\\n").filter(Boolean).map(JSON.parse) : [];
appendFileSync(process.env.STUB_LOG, JSON.stringify({ command, args: process.argv.slice(3) }) + "\\n");
if (command === "npm") {
  const responses = JSON.parse(process.env.STUB_RESPONSES);
  const index = calls.filter((call) => call.command === "npm").length;
  const response = responses[Math.min(index, responses.length - 1)];
  if (response.status) { process.stderr.write("fixture registry temporarily unavailable\\n"); process.exit(response.status); }
  process.stdout.write(response.version + "\\n");
} else if (command !== "sleep") { process.exit(99); }
`);
  const result = spawnSync("bash", ["--noprofile", "--norc", "-e", "-o", "pipefail", "-c", `
npm() { "$STUB_NODE" "$STUB_SCRIPT" npm "$@"; }
sleep() { "$STUB_NODE" "$STUB_SCRIPT" sleep "$@"; }
${source}`], {
    encoding: "utf8",
    timeout: 15000,
    env: { ...process.env, RELEASE_VERSION: pkg.version, NPM_DIST_TAG: "blue", STUB_NODE: process.execPath, STUB_SCRIPT: stubPath, STUB_LOG: logPath, STUB_RESPONSES: JSON.stringify(scenario.responses) },
  });
  assert.ifError(result.error);
  assert.equal(result.status, scenario.status, result.stdout + result.stderr);
  const calls = readFileSync(logPath, "utf8").trim().split("\n").map(JSON.parse);
  const queries = calls.filter((call) => call.command === "npm");
  const waits = calls.filter((call) => call.command === "sleep");
  assert.equal(queries.length, scenario.calls, "频道校验重试次数错误");
  assert.equal(waits.length, scenario.calls - 1, "首次查询和最终退出不应额外等待");
  assert.deepEqual(calls.map((call) => call.command), Array.from({ length: scenario.calls * 2 - 1 }, (_, index) => index % 2 ? "sleep" : "npm"));
  for (const query of queries) {
    assert.deepEqual(query.args, ["view", "@qfeius/everyline-cli@blue", "version", "--registry", "https://registry.npmjs.org", "--prefer-online"]);
  }
  for (const wait of waits) assert.deepEqual(wait.args, ["15"]);
  for (const response of scenario.responses) {
    if (response.version) assert.ok(result.stdout.includes(response.version), "应输出注册表实际返回的版本");
  }
});
}

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
