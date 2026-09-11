"use strict";

const assert = require("node:assert/strict");
const { execFileSync, spawnSync } = require("node:child_process");
const { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const test = require("node:test");
const { nextPackageVersion } = require("../../scripts/package-version");

/**
 * 验证两个环境共享已发布版本序列，旧分支跟上全局版本，且移除历史预发布及构建后缀。
 * 入参：无；场景包含本地版本、npm 全部版本和下一正式版本。
 * 返回值：void，选号回退、使用字符串排序或保留环境后缀时断言失败。
 */
test("下一正式版本高于本地及 npm 全部版本的 stable base", () => {
  for (const [local, published, expected] of [
    ["0.1.8", ["0.1.9", "0.1.11"], "0.1.12"],
    ["0.1.11", ["0.1.9", "0.1.12"], "0.1.13"],
    ["0.1.15", ["0.1.9", "0.1.12"], "0.1.16"],
    ["0.1.8", ["0.1.9", "0.1.10", "0.1.2"], "0.1.11"],
    ["0.1.10-blue.1", ["0.1.9"], "0.1.11"],
    ["0.1.8", ["0.1.9", "0.2.0-blue.3"], "0.2.1"],
    ["0.1.8+build.1", ["1.0.0-rc.1", "0.9.99"], "1.0.1"],
    ["0.1.8", "0.1.11", "0.1.12"],
    ["0.1.8", [], "0.1.9"],
  ]) {
    assert.equal(nextPackageVersion(local, published), expected);
  }
});

/**
 * 验证不完整注册表响应和非法本地版本严格失败，不以静默回退掩盖版本冲突。
 * 入参：无；场景包含无效 SemVer、非版本列表和列表中的无效项。
 * 返回值：void，非法输入产生可发布版本时断言失败。
 */
test("选号拒绝非法版本或 npm versions 响应", () => {
  for (const [local, published] of [
    ["dev", ["0.1.11"]],
    ["v0.1.8", ["0.1.11"]],
    ["0.01.8", ["0.1.11"]],
    ["0.1.8", ["0.1.11-blue.01"]],
    ["0.1.8", ["0.1.11", "broken"]],
    ["0.1.8", [null]],
    ["0.1.8", null],
    ["0.1.8", {}],
  ]) {
    assert.throws(() => nextPackageVersion(local, published), /版本|versions/);
  }
});

/**
 * createVersionFixture 创建真实 npm version 可更新的隔离包，记录 package.json 和 lockfile 初始内容。
 * 入参：t（TestContext）管理临时目录和文件清理。
 * 返回值：object，包含包根目录及原始包文件内容。
 */
function createVersionFixture(t) {
  const root = mkdtempSync(join(tmpdir(), "everyline-global-version-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const manifest = JSON.stringify({ name: "@qfeius/everyline-cli", version: "0.1.8", publishConfig: { tag: "latest" } });
  const lockfile = JSON.stringify({ name: "@qfeius/everyline-cli", version: "0.1.8", lockfileVersion: 3, packages: { "": { name: "@qfeius/everyline-cli", version: "0.1.8" } } });
  writeFileSync(join(root, "package.json"), manifest);
  writeFileSync(join(root, "package-lock.json"), lockfile);
  return { root, manifest, lockfile };
}

/**
 * runBumpFixture 仅替换 npm view 网络边界，保留真实 npm version 对包版本及 lockfile 的更新。
 * 入参：fixture（object）为隔离包；registry（string）为模拟注册表输出，null 表示网络失败。
 * 返回值：SpawnSyncReturns<string>，包含独立进程的输出和退出状态。
 */
function runBumpFixture(fixture, registry) {
  return spawnSync(process.execPath, ["-e", `
const assert = require('node:assert/strict');
const child = require('node:child_process');
const spec = JSON.parse(process.argv[1]);
const original = child.execFileSync;
child.execFileSync = (command, args, options) => {
  const npmArgs = command === process.execPath ? args.slice(1) : args;
  if (npmArgs[0] === 'view') {
    assert.deepEqual(npmArgs, ['view', '@qfeius/everyline-cli', 'versions', '--json', '--registry', 'https://registry.npmjs.org', '--prefer-online']);
    assert.equal(options.cwd, spec.root);
    if (spec.registry === null) throw new Error('fixture registry unavailable');
    return spec.registry;
  }
  if (npmArgs[0] === 'version') {
    assert.deepEqual(npmArgs, ['version', '0.1.12', '--no-git-tag-version']);
    assert.equal(options.cwd, spec.root);
  }
  return original(command, args, options);
};
process.stdout.write(require(spec.script).bumpPackageVersion(spec.root));
`, JSON.stringify({ root: fixture.root, registry, script: join(__dirname, "../../scripts/package-version.js") })], {
    encoding: "utf8",
    env: { ...process.env, npm_config_cache: join(fixture.root, "npm-cache") },
  });
}

/**
 * 验证全局选号后调用 npm version 同步更新 package.json 和 package-lock.json，并保留环境渠道。
 * 入参：t（TestContext）负责隔离夹具清理。
 * 返回值：void，更新文件不一致或修改发布渠道时断言失败。
 */
test("registry 选号后由真实 npm version 同步 lockfile", (t) => {
  const fixture = createVersionFixture(t);
  const result = runBumpFixture(fixture, JSON.stringify(["0.1.9", "0.1.11"]));
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.equal(result.stdout, "0.1.12");
  const manifest = JSON.parse(readFileSync(join(fixture.root, "package.json")));
  const lockfile = JSON.parse(readFileSync(join(fixture.root, "package-lock.json")));
  assert.equal(manifest.version, "0.1.12");
  assert.equal(manifest.publishConfig.tag, "latest");
  assert.equal(lockfile.version, "0.1.12");
  assert.equal(lockfile.packages[""].version, "0.1.12");
});

/**
 * 验证网络失败、损坏 JSON 和非法版本响应都在写盘前停止，原版本和 lockfile 保持逐字节不变。
 * 入参：t（TestContext）提供各失败场景的隔离包。
 * 返回值：Promise<void>，错误未传播或失败后修改包文件时断言失败。
 */
test("registry 失败时保留本地版本和 lockfile", async (t) => {
  for (const registry of [null, "invalid json", "{}", '["0.1.11", "invalid"]']) {
    await t.test(String(registry), (t) => {
      const fixture = createVersionFixture(t);
      const result = runBumpFixture(fixture, registry);
      assert.notEqual(result.status, 0);
      assert.match(result.stderr, /fixture registry unavailable|JSON|npm versions|无效包版本/);
      assert.equal(readFileSync(join(fixture.root, "package.json"), "utf8"), fixture.manifest);
      assert.equal(readFileSync(join(fixture.root, "package-lock.json"), "utf8"), fixture.lockfile);
    });
  }
});

/**
 * 验证 Windows 复用 npm 生命周期或 PATH 中的 JS 入口，特殊字符路径以参数传给 Node，不经过 shell。
 * 入参：t（TestContext）提供独立 npm 入口与包目录。
 * 返回值：Promise<void>，执行入口、查询/升版参数或缺失入口行为错误时断言失败。
 */
test("Windows 全局选号通过 Node 执行 npm CLI", async (t) => {
  for (const mode of ["environment", "path", "missing"]) {
    await t.test(mode, (t) => {
      const fixture = createVersionFixture(t);
      const npmRoot = join(fixture.root, "npm with spaces & symbols");
      const npmCLI = join(npmRoot, "node_modules", "npm", "bin", "npm-cli.js");
      mkdirSync(join(npmCLI, ".."), { recursive: true });
      if (mode !== "missing") {
        writeFileSync(npmCLI, `
const assert = require('node:assert/strict');
const args = process.argv.slice(2);
if (args[0] === 'view') {
  assert.deepEqual(args, ['view', '@qfeius/everyline-cli', 'versions', '--json', '--registry', 'https://registry.npmjs.org', '--prefer-online']);
  process.stdout.write('["0.1.9", "0.1.11"]');
} else {
  assert.deepEqual(args, ['version', '0.1.12', '--no-git-tag-version']);
  process.stdout.write('v0.1.12');
}
`);
      }
      const run = () => execFileSync(process.execPath, ["-e", `
const assert = require('node:assert/strict');
const child = require('node:child_process');
const spec = JSON.parse(process.argv[1]);
const original = child.execFileSync;
Object.defineProperty(process, 'platform', { value: 'win32' });
delete process.env.npm_execpath;
if (spec.mode === 'environment') process.env.npm_execpath = spec.npmCLI;
child.execFileSync = (command, args, options) => {
  if (command === 'where.exe') {
    assert.notEqual(spec.mode, 'environment');
    assert.deepEqual(args, ['npm.cmd']);
    return spec.npmCMD + '\\r\\n';
  }
  assert.equal(command, process.execPath);
  assert.equal(args[0], spec.npmCLI);
  assert.equal(options.cwd, spec.root);
  return original(command, args, options);
};
process.stdout.write(require(spec.script).bumpPackageVersion(spec.root));
`, JSON.stringify({ root: fixture.root, mode, npmCLI, npmCMD: join(npmRoot, "npm.cmd"), script: join(__dirname, "../../scripts/package-version.js") })], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
      if (mode === "missing") {
        assert.throws(run, /未找到 npm-cli\.js/);
      } else {
        assert.equal(run(), "0.1.12");
      }
    });
  }
});
