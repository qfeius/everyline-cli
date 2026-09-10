"use strict";
const { execFileSync } = require("node:child_process");
const { existsSync, mkdirSync, renameSync } = require("node:fs");
const { basename, dirname, join, resolve } = require("node:path");

/**
 * packRelease 生成 npm 安装包并统一交付文件名，不改变包内 scoped 名称。
 * 入参：destination string 为制品目录；默认 dist。
 * 返回值：string 为最终安装包的绝对路径；打包失败时抛出错误。
 */
function packRelease(destination = "dist") {
  const root = resolve(__dirname, "..");
  const directory = resolve(destination);
  mkdirSync(directory, { recursive: true });
  let command = "npm";
  const args = ["pack", "--json", "--pack-destination", directory];
  if (process.platform === "win32") {
    // npm 生命周期优先复用同一 npm；直接运行脚本时按 PATH 定位 npm.cmd 对应的 JS 入口。
    let npmCLI = process.env.npm_execpath;
    if (!npmCLI || basename(npmCLI) !== "npm-cli.js" || !existsSync(npmCLI)) {
      const commands = execFileSync("where.exe", ["npm.cmd"], { encoding: "utf8" }).trim().split(/\r?\n/);
      npmCLI = commands.map(file => join(dirname(file), "node_modules", "npm", "bin", "npm-cli.js"))
        .find(file => existsSync(file));
    }
    if (!npmCLI) throw new Error("未找到 npm-cli.js，请确认 PATH 中的 npm 安装完整");
    // 直接由 Node 执行 JS，避免 .cmd 启动失败或 shell 解释制品目录中的空格及特殊字符。
    command = process.execPath;
    args.unshift(npmCLI);
  }
  const output = execFileSync(command, args, {
    cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"],
  });
  const [artifact] = JSON.parse(output);
  const target = join(directory, `everyline-cli-${artifact.version}.tgz`);
  // npm 的自动文件名包含 scope；下载和安装入口统一使用不带 scope 的文件名。
  renameSync(join(directory, artifact.filename), target);
  return target;
}
if (require.main === module) console.log(packRelease(process.argv[2]));
module.exports = { packRelease };
