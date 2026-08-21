#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const { existsSync } = require("node:fs");
const { dirname, join } = require("node:path");
const { resolvePlatformTarget } = require("./platform");

/**
 * resolveBinary 根据 Node 平台和架构定位 npm 包内的原生二进制。
 * 入参：无，读取 process.platform/process.arch 和可选 EVERYLINE_CLI_BINARY。
 * 返回值：string，可执行文件路径；找不到时抛出 Error。
 */
function resolveBinary() {
  if (process.env.EVERYLINE_CLI_BINARY) {
    return process.env.EVERYLINE_CLI_BINARY;
  }
  const executable = process.platform === "win32" ? "everyline-cli.exe" : "everyline-cli";
  const packageRoot = join(dirname(__filename), "..");
  const binary = join(packageRoot, "bin", resolvePlatformTarget(), executable);
  if (!existsSync(binary)) {
    throw new Error(
      `当前 npm 包缺少 ${process.platform}/${process.arch} 二进制；请使用原生发布包，或设置 EVERYLINE_CLI_BINARY`,
    );
  }
  return binary;
}

try {
  const result = spawnSync(resolveBinary(), process.argv.slice(2), {
    stdio: "inherit",
    env: { ...process.env, EVERYLINE_CLI_WRAPPER: "1" },
  });
  if (result.error) {
    throw result.error;
  }
  process.exitCode = result.status ?? 1;
} catch (error) {
  process.stderr.write(`everyline-cli: ${error.message}\n`);
  process.exitCode = 1;
}
