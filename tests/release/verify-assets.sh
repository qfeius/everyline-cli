#!/usr/bin/env sh
set -eu

# 六个平台目录、可执行文件和 checksum 必须同时存在。
for target in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64 windows-amd64 windows-arm64; do
  executable=everyline-cli
  case "$target" in
    windows-*) executable=everyline-cli.exe ;;
  esac
  test -s "bin/$target/$executable"
done

test -s bin/checksums.txt
test "$(wc -l < bin/checksums.txt | tr -d ' ')" -eq 6
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c bin/checksums.txt
elif command -v shasum >/dev/null 2>&1; then
  shasum -a 256 -c bin/checksums.txt
else
  echo "缺少 sha256sum 或 shasum，无法验证 checksum" >&2
  exit 1
fi
