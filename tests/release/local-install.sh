#!/usr/bin/env sh
set -eu

temporary_dir=$(mktemp -d)
trap 'rm -rf "$temporary_dir"' EXIT HUP INT TERM
npm pack --json --ignore-scripts --pack-destination "$temporary_dir" > "$temporary_dir/pack.json"
package_file=$(node -e 'const value=require(process.argv[1]); process.stdout.write(value[0].filename)' "$temporary_dir/pack.json")
npm install --silent --prefix "$temporary_dir/install" "$temporary_dir/$package_file"
output=$("$temporary_dir/install/node_modules/.bin/everyline-cli" version --output json)

# 可选的期望版本用于确保 ldflags 元数据贯穿构建、打包、安装和启动。
if [ -n "${EXPECTED_VERSION:-}" ]; then
  printf '%s' "$output" | grep -F '"version": "'"$EXPECTED_VERSION"'"' >/dev/null
fi
printf '%s\n' "$output"
