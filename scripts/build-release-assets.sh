#!/usr/bin/env sh
set -eu

# 为 npm/npx 薄包装生成 macOS、Linux、Windows 的 amd64/arm64 原生二进制。
version=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || true)}
commit=${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || true)}
build_date=${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
version=${version:-0.0.0-development}
commit=${commit:-uncommitted}

for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
  target_os=${target%/*}
  target_arch=${target#*/}
  output_dir="bin/${target_os}-${target_arch}"
  executable="everyline-cli"
  if [ "$target_os" = "windows" ]; then
    executable="everyline-cli.exe"
  fi
  mkdir -p "$output_dir"
  GOOS="$target_os" GOARCH="$target_arch" CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X git.qtech.cn/ai/everyline-cli/internal/build.Version=$version -X git.qtech.cn/ai/everyline-cli/internal/build.Commit=$commit -X git.qtech.cn/ai/everyline-cli/internal/build.Date=$build_date" \
    -o "$output_dir/$executable" \
    ./cmd/everyline-cli
done

# checksum 只覆盖六个平台目录，仓库里可能残留的本机构建不会进入发布清单。
: > bin/checksums.txt
if command -v sha256sum >/dev/null 2>&1; then
	for target in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64 windows-amd64 windows-arm64; do
		executable=everyline-cli
		case "$target" in
			windows-*) executable=everyline-cli.exe ;;
		esac
		sha256sum "bin/$target/$executable" >> bin/checksums.txt
	done
elif command -v shasum >/dev/null 2>&1; then
	for target in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64 windows-amd64 windows-arm64; do
		executable=everyline-cli
		case "$target" in
			windows-*) executable=everyline-cli.exe ;;
		esac
		shasum -a 256 "bin/$target/$executable" >> bin/checksums.txt
	done
else
  echo "缺少 sha256sum 或 shasum，无法生成 checksum" >&2
  exit 1
fi
