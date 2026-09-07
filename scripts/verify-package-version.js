"use strict";

const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const { normalizePackageVersion } = require("./package-version");

const packagePath = join(__dirname, "..", "package.json");
const packageData = JSON.parse(readFileSync(packagePath, "utf8"));
if (packageData.version === "0.0.0-development" || normalizePackageVersion(packageData.version) !== packageData.version) {
  throw new Error(`拒绝打包未同步的 npm 版本: ${packageData.version}`);
}
