"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");
const { resolvePlatformTarget } = require("../../scripts/platform");

/**
 * platformCases 返回 npm 支持矩阵及对应 Go 产物目录。
 * 入参：无。
 * 返回值：Array<object>，每项包含 platform、architecture 和 expected。
 */
function platformCases() {
  return [
    { platform: "darwin", architecture: "x64", expected: "darwin-amd64" },
    { platform: "darwin", architecture: "arm64", expected: "darwin-arm64" },
    { platform: "linux", architecture: "x64", expected: "linux-amd64" },
    { platform: "linux", architecture: "arm64", expected: "linux-arm64" },
    { platform: "win32", architecture: "x64", expected: "windows-amd64" },
    { platform: "win32", architecture: "arm64", expected: "windows-arm64" },
  ];
}

for (const item of platformCases()) {
  test(`${item.platform}/${item.architecture} maps to ${item.expected}`, () => {
    assert.equal(resolvePlatformTarget(item.platform, item.architecture), item.expected);
  });
}

test("unsupported architecture fails closed", () => {
  assert.throws(() => resolvePlatformTarget("linux", "ia32"), /不支持的平台或架构/);
});
