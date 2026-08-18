#!/usr/bin/env node

const { chmodSync, existsSync } = require("node:fs");
const { join } = require("node:path");
const { resolvePlatformTarget } = require("./platform");

// npm 安装必须确认当前平台产物真实存在，防止留下“安装成功但无法运行”的包。
const executable = process.platform === "win32" ? "everyline-cli.exe" : "everyline-cli";
const binary = join(__dirname, "..", "bin", resolvePlatformTarget(), executable);
if (!existsSync(binary)) {
  throw new Error(`npm 包缺少当前平台二进制: ${binary}`);
}
if (process.platform !== "win32") {
  chmodSync(binary, 0o755);
}
