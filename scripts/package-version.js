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

/**
 * validateBlueRelease 校验 blue 发布的版本后缀与 npm 渠道，避免覆盖其他环境。
 * 入参：version string 为标签中的版本；channel string 为包声明的发布渠道。
 * 返回值：void；非 blue 版本、构建元数据或其他渠道时抛出错误。
 */
function validateBlueRelease(version, channel) {
  if (channel !== "blue" || normalizePackageVersion(version) !== version || !/^\d+\.\d+\.\d+-blue\.\d+$/.test(version)) {
    throw new Error("blue 发布必须使用 <版本>-blue.<序号> 和 blue 渠道");
  }
}

module.exports = { normalizePackageVersion, validateBlueRelease };
