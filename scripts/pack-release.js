"use strict";
const { execFileSync } = require("node:child_process");
const { mkdirSync, renameSync } = require("node:fs");
const { join, resolve } = require("node:path");

/**
 * packRelease 生成 npm 安装包并统一交付文件名，不改变包内 scoped 名称。
 * 入参：destination string 为制品目录；默认 dist。
 * 返回值：string 为最终安装包的绝对路径；打包失败时抛出错误。
 */
function packRelease(destination = "dist") {
  const root = resolve(__dirname, "..");
  const directory = resolve(destination);
  mkdirSync(directory, { recursive: true });
  const output = execFileSync(process.platform === "win32" ? "npm.cmd" : "npm", ["pack", "--json", "--pack-destination", directory], {
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
