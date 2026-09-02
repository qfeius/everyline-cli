#!/usr/bin/env sh
set -eu

output_dir=${1:-dist}

for skill_name in everyline-shared everyline-review everyline-review-config everyline-cli; do
  archive="$output_dir/$skill_name-skill.zip"
  test -s "$archive"
  unzip -Z1 "$archive" | grep -Fx 'SKILL.md' >/dev/null
  unzip -p "$archive" SKILL.md | grep -F "name: $skill_name" >/dev/null
done

# 三端共用的授权、沙箱附件和签名链接约束必须进入豆包最终制品。
unzip -p "$output_dir/everyline-shared-skill.zip" SKILL.md | grep -F 'auth init --profile <profile> --as user --output json' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" references/review-flow.md | grep -F -- '--stdin --name <filename>' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '包括 `token`' >/dev/null
unzip -p "$output_dir/everyline-review-config-skill.zip" references/management.md | grep -F '规则分组' >/dev/null
