#!/usr/bin/env sh
set -eu

temporary_dir=$(mktemp -d)
manifest="$temporary_dir/manifest.json"
package_root=.
trap 'rm -rf "$temporary_dir"' EXIT HUP INT TERM

if [ -n "${EXPECTED_PACKAGE_VERSION:-}" ]; then
  package_root="$temporary_dir/package"
  mkdir -p "$package_root"
  cp package.json README.md "$package_root/"
  mkdir -p "$package_root/docs"
  cp docs/everyline-cli-skill-guide.md docs/everyline-cli-skill-interaction-scenarios.md "$package_root/docs/"
  # 发布校验使用临时包根目录时，也携带对外分发的 Agent Skill。
  cp -R bin scripts skills "$package_root/"
  node - "$package_root/package.json" "$EXPECTED_PACKAGE_VERSION" <<'NODE'
const { readFileSync, writeFileSync } = require("node:fs");

const path = process.argv[2];
const packageData = JSON.parse(readFileSync(path, "utf8"));
packageData.version = process.argv[3];
writeFileSync(path, `${JSON.stringify(packageData, null, 2)}\n`);
NODE
fi

node "$package_root/scripts/verify-package-version.js"
(cd "$package_root" && npm pack --dry-run --json) > "$manifest"

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
  return [
    "bin/checksums.txt",
    "docs/everyline-cli-skill-guide.md",
    "docs/everyline-cli-skill-interaction-scenarios.md",
    "scripts/install.js",
    "scripts/package-version.js",
    "scripts/platform.js",
    "scripts/run.js",
    "scripts/verify-package-version.js",
    "skills/everyline-cli/SKILL.md",
    "skills/everyline-cli/references/management.md",
    "skills/everyline-cli/references/review-flow.md",
    "skills/everyline-shared/SKILL.md",
    "skills/everyline-review/SKILL.md",
    "skills/everyline-review/references/review-flow.md",
    "skills/everyline-review-config/SKILL.md",
    "skills/everyline-review-config/references/management.md",
    ...binaries,
  ];
}

const manifest = JSON.parse(readFileSync(process.argv[2], "utf8"));
const expectedVersion = process.env.EXPECTED_PACKAGE_VERSION;
if (expectedVersion && manifest[0].version !== expectedVersion) {
  throw new Error(`npm 包版本=${manifest[0].version}，期望 ${expectedVersion}`);
}
const files = new Set(manifest[0].files.map((entry) => entry.path));
const missing = requiredPackageFiles().filter((file) => !files.has(file));
if (missing.length > 0) {
  throw new Error(`npm pack 缺少文件: ${missing.join(", ")}`);
}
if (files.has("bin/everyline-cli") || files.has("bin/everyline-cli.exe")) {
  throw new Error("npm pack 不得包含未标注平台架构的本机构建");
}
NODE
