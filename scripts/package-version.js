"use strict";

const { execFileSync } = require("node:child_process");
const { existsSync, readFileSync } = require("node:fs");
const { basename, dirname, join } = require("node:path");

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

/**
 * validateRelease 校验环境对应的 npm 渠道和共享的正式版本格式。
 * 入参：version string 为标签版本；channel string 为包声明渠道；expectedChannel string 为分支指定渠道。
 * 返回值：void；渠道不符或版本包含后缀、构建元数据、非法格式时抛出错误。
 */
function validateRelease(version, channel, expectedChannel) {
  if (channel !== expectedChannel || normalizePackageVersion(version) !== version || !/^\d+\.\d+\.\d+$/.test(version)) {
    throw new Error(`${expectedChannel} 发布必须使用 x.y.z 正式版本和 ${expectedChannel} 渠道，不含预发布后缀或构建元数据`);
  }
}

/**
 * nextPackageVersion 从本地和 npm 全部已发布版本中选择最高正式基础版本，再递增 patch。
 * 入参：localVersion string 为本地包版本；publishedVersions string[] 或 string 为 npm versions JSON。
 * 返回值：string，纯 x.y.z 下一版本；任一版本或响应格式非法时抛出错误。
 */
function nextPackageVersion(localVersion, publishedVersions) {
  if (typeof publishedVersions === "string") publishedVersions = [publishedVersions];
  if (!Array.isArray(publishedVersions)) throw new Error("npm versions 响应必须为版本列表");
  let highest;
  for (const value of [localVersion, ...publishedVersions]) {
    if (typeof value !== "string" || normalizePackageVersion(value) !== value) {
      throw new Error(`无效包版本: ${value}`);
    }
    // 历史环境预发布也计入基础版本，避免新分支重新使用更低的版本序列。
    const base = value.split(/[+-]/)[0].split(".").map(BigInt);
    const difference = highest ? base.findIndex((number, index) => number !== highest[index]) : 0;
    if (!highest || (difference >= 0 && base[difference] > highest[difference])) highest = base;
  }
  highest[2] += 1n;
  return highest.join(".");
}

/**
 * bumpPackageVersion 查询官方 npm 全量版本并通过 npm version 更新本地包及锁文件，不创建 Git 标签。
 * 入参：root string 为包根目录，默认使用本脚本所属仓库。
 * 返回值：string，为写入的下一正式版本；查询或校验失败时在修改包文件前抛出错误。
 */
function bumpPackageVersion(root = join(__dirname, "..")) {
  const pkg = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
  let command = "npm";
  let prefix = [];
  if (process.platform === "win32") {
    // 与打包入口保持一致，通过 Node 执行 npm，避免 .cmd 和特殊字符路径被 shell 解释。
    let npmCLI = process.env.npm_execpath;
    if (!npmCLI || basename(npmCLI) !== "npm-cli.js" || !existsSync(npmCLI)) {
      const commands = execFileSync("where.exe", ["npm.cmd"], { encoding: "utf8" }).trim().split(/\r?\n/);
      npmCLI = commands.map(file => join(dirname(file), "node_modules", "npm", "bin", "npm-cli.js"))
        .find(file => existsSync(file));
    }
    if (!npmCLI) throw new Error("未找到 npm-cli.js，请确认 PATH 中的 npm 安装完整");
    command = process.execPath;
    prefix = [npmCLI];
  }
  const options = { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] };
  // 只查同一包的全量版本，不读取 dist-tag；网络或 JSON 失败直接停止，不回退本地选号。
  const published = JSON.parse(execFileSync(command, [...prefix, "view", pkg.name, "versions", "--json", "--registry", "https://registry.npmjs.org", "--prefer-online"], options));
  const next = nextPackageVersion(pkg.version, published);
  execFileSync(command, [...prefix, "version", next, "--no-git-tag-version"], options);
  return next;
}

if (require.main === module) {
  process.stdout.write(process.argv[2] === "--bump" ? bumpPackageVersion() + "\n" : normalizePackageVersion(process.argv[2]));
}

module.exports = { normalizePackageVersion, validateRelease, nextPackageVersion, bumpPackageVersion };
