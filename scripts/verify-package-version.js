"use strict";

const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const { normalizePackageVersion, validateBlueRelease } = require("./package-version");

const packagePath = join(__dirname, "..", "package.json");
const packageData = JSON.parse(readFileSync(packagePath, "utf8"));
if (packageData.version === "0.0.0-development" || normalizePackageVersion(packageData.version) !== packageData.version) {
  throw new Error(`拒绝打包未同步的 npm 版本: ${packageData.version}`);
}
// 本地构建包保留 CI 版本；真正发布时必须使用 blue 的独立版本与渠道。
if (process.argv.includes("--publish")) validateBlueRelease(packageData.version, packageData.publishConfig?.tag);
