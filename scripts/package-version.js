"use strict";

/**
 * normalizePackageVersion 将 Git tag 或提交描述转换为 npm 可接受且可追溯的 SemVer。
 * 入参：value string，为发布 tag、CI 版本或 git describe 结果。
 * 返回值：string；合法 SemVer 保留并去掉前导 v，其他值映射为 0.0.0-build-* 预发布版本。
 */
function normalizePackageVersion(value) {
  const source = String(value || "").trim();
  const candidate = source.replace(/^v(?=\d)/, "");
  const semver = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;
  if (semver.test(candidate)) {
    return candidate;
  }
  const normalized = source
    .toLowerCase()
    .replace(/[^0-9a-z-]+/g, "-")
    .replace(/^-+|-+$/g, "") || "development";
  return `0.0.0-build-${normalized}`;
}

if (require.main === module) {
  process.stdout.write(normalizePackageVersion(process.argv[2]));
}

module.exports = { normalizePackageVersion };
