"use strict";

const platformMap = Object.freeze({ darwin: "darwin", linux: "linux", win32: "windows" });
const architectureMap = Object.freeze({ x64: "amd64", arm64: "arm64" });

/**
 * resolvePlatformTarget 将 Node 平台名映射为 GoReleaser/npm 产物目录名。
 * 入参：platform string 为 Node process.platform；architecture string 为 Node process.arch。
 * 返回值：string，格式为 goos-goarch；不支持的平台或架构会抛出 Error。
 */
function resolvePlatformTarget(platform = process.platform, architecture = process.arch) {
  const targetPlatform = platformMap[platform];
  const targetArchitecture = architectureMap[architecture];
  if (!targetPlatform || !targetArchitecture) {
    throw new Error(`不支持的平台或架构: ${platform}/${architecture}`);
  }
  return `${targetPlatform}-${targetArchitecture}`;
}

module.exports = { resolvePlatformTarget };
