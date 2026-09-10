"use strict";

const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const test = require("node:test");

/**
 * createPackFixture 创建隔离源码和含空格、shell 特殊字符的交付目录。
 * 入参：t（TestContext）负责本次临时目录清理。
 * 返回值：object，包含根目录、脚本路径和制品目录。
 */
function createPackFixture(t) {
  const root = mkdtempSync(join(tmpdir(), "everyline-pack-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const packageRoot = join(root, "package source");
  const script = join(packageRoot, "scripts", "pack-release.js");
  mkdirSync(join(packageRoot, "scripts"), { recursive: true });
  copyFileSync(join(__dirname, "../../scripts/pack-release.js"), script);
  writeFileSync(join(packageRoot, "package.json"), JSON.stringify({ name: "@qfeius/everyline-cli", version: "1.2.3" }));
  writeFileSync(join(packageRoot, "README.md"), "isolated package fixture");
  return { root, script, destination: join(root, "output with spaces & symbols (draft)") };
}

/**
 * runWindowsPack 在独立进程中走 Windows 分支，仅替换 where.exe 边界，真实执行 Node 子进程。
 * 入参：fixture（object）为隔离包；mode（string）选择 npm 生命周期、PATH、其他包管理器或缺失入口。
 * 返回值：string，为打包后的文件路径；定位或子进程执行失败时抛出错误。
 */
function runWindowsPack(fixture, mode) {
  const npmRoot = join(fixture.root, "npm with spaces & symbols");
  const npmCLI = join(npmRoot, "node_modules", "npm", "bin", "npm-cli.js");
  mkdirSync(join(npmCLI, ".."), { recursive: true });
  if (mode !== "missing") {
    // 验证传给真实 Node 子进程的每个参数，特别是制品目录不会被 shell 拆分或解释。
    writeFileSync(npmCLI, `
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const destination = process.env.PACK_FIXTURE_DESTINATION;
assert.deepEqual(process.argv.slice(2), ['pack', '--json', '--pack-destination', destination]);
const artifact = { version: '1.2.3', filename: 'qfeius-everyline-cli-1.2.3.tgz' };
fs.writeFileSync(path.join(destination, artifact.filename), 'packed fixture');
process.stdout.write(JSON.stringify([artifact]));
`);
  }
  const otherCLI = join(fixture.root, "yarn.js");
  writeFileSync(otherCLI, "throw new Error('不应执行其他包管理器');");
  return execFileSync(process.execPath, ["-e", `
const assert = require('node:assert/strict');
const child = require('node:child_process');
const spec = JSON.parse(process.argv[1]);
const original = child.execFileSync;
Object.defineProperty(process, 'platform', { value: 'win32' });
delete process.env.npm_execpath;
if (spec.mode === 'environment') process.env.npm_execpath = spec.npmCLI;
if (spec.mode === 'other-manager') process.env.npm_execpath = spec.otherCLI;
process.env.PACK_FIXTURE_DESTINATION = spec.destination;
child.execFileSync = (command, args, options) => {
  if (command === 'where.exe') {
    assert.notEqual(spec.mode, 'environment', 'npm 生命周期应直接复用已知入口');
    assert.deepEqual(args, ['npm.cmd']);
    return spec.commands.join('\\r\\n') + '\\r\\n';
  }
  assert.equal(command, process.execPath, 'Windows 应直接启动 Node');
  assert.equal(args[0], spec.npmCLI);
  return original(command, args, options);
};
process.stdout.write(require(spec.script).packRelease(spec.destination));
`, JSON.stringify({
    ...fixture, mode, npmCLI, otherCLI,
    commands: [join(fixture.root, "missing shim", "npm.cmd"), join(npmRoot, "npm.cmd")],
  })], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
}

/**
 * 验证真实 npm pack 在隔离包中保留 scope，并统一对外交付文件名。
 * 入参：t（TestContext）负责临时制品清理；返回值：void，打包或改名失败时断言失败。
 */
test("packRelease 使用真实 npm 生成交付包", (t) => {
  const fixture = createPackFixture(t);
  const target = require(fixture.script).packRelease(fixture.destination);
  assert.equal(target, join(fixture.destination, "everyline-cli-1.2.3.tgz"));
  assert.ok(readFileSync(target).length > 0);
  assert.equal(existsSync(join(fixture.destination, "qfeius-everyline-cli-1.2.3.tgz")), false);
});

/**
 * 验证 Windows 的 npm 定位、Node 启动、特殊字符路径和交付文件改名。
 * 入参：t（TestContext）提供各定位场景；返回值：Promise<void>，入口或参数变化时断言失败。
 */
test("Windows 打包通过 Node 执行 npm CLI 并保留完整参数", async (t) => {
  for (const mode of ["environment", "path", "other-manager"]) {
    await t.test(mode, (t) => {
      const fixture = createPackFixture(t);
      const target = runWindowsPack(fixture, mode);
      assert.equal(target, join(fixture.destination, "everyline-cli-1.2.3.tgz"));
      assert.equal(readFileSync(target, "utf8"), "packed fixture");
      assert.equal(existsSync(join(fixture.destination, "qfeius-everyline-cli-1.2.3.tgz")), false);
    });
  }
});

/**
 * 验证 npm 安装不完整时明确报错，不通过 shell 猜测执行入口。
 * 入参：t（TestContext）负责夹具清理；返回值：void，缺失入口未报错时断言失败。
 */
test("Windows 缺少 npm JS 入口时停止打包", (t) => {
  const fixture = createPackFixture(t);
  assert.throws(() => runWindowsPack(fixture, "missing"), /未找到 npm-cli\.js/);
  assert.equal(existsSync(join(fixture.destination, "everyline-cli-1.2.3.tgz")), false);
});
