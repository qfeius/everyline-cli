#!/usr/bin/env sh
set -eu

manifest=$(mktemp)
trap 'rm -f "$manifest"' EXIT HUP INT TERM
npm pack --dry-run --json --ignore-scripts > "$manifest"

# 由 Node 解析 npm 的 JSON 清单，精确核对 wrapper、checksum 和六个平台产物。
node - "$manifest" <<'NODE'
const { readFileSync } = require("node:fs");

/**
 * requiredPackageFiles 返回 npm 包必须携带的发布文件。
 * 入参：无。
 * 返回值：string[]，相对于包根目录的文件名。
 */
function requiredPackageFiles() {
  const targets = ["darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64"];
  const binaries = targets.map((target) => `bin/${target}/everyline-cli${target.startsWith("windows-") ? ".exe" : ""}`);
  return ["bin/checksums.txt", "scripts/install.js", "scripts/platform.js", "scripts/run.js", ...binaries];
}

const manifest = JSON.parse(readFileSync(process.argv[2], "utf8"));
const files = new Set(manifest[0].files.map((entry) => entry.path));
const missing = requiredPackageFiles().filter((file) => !files.has(file));
if (missing.length > 0) {
  throw new Error(`npm pack 缺少文件: ${missing.join(", ")}`);
}
if (files.has("bin/everyline-cli") || files.has("bin/everyline-cli.exe")) {
  throw new Error("npm pack 不得包含未标注平台架构的本机构建");
}
NODE
