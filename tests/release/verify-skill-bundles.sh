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
unzip -p "$output_dir/everyline-shared-skill.zip" SKILL.md | grep -F '不得执行 `auth login --profile <profile> --as user`' >/dev/null
unzip -p "$output_dir/everyline-cli-skill.zip" SKILL.md | grep -F '不得执行 `auth login --profile <profile> --as user`' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" references/review-flow.md | grep -F -- '--stdin --name <filename>' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '只包含以下三项' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '不得单独展示 `taskId`' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '不追加“已完成”' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '[审查结果详情](<REVIEW_DETAIL_URL>)' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" references/review-flow.md | grep -F '服务端原始终态对象' >/dev/null
unzip -p "$output_dir/everyline-cli-skill.zip" references/review-flow.md | grep -F '只向用户返回“审查结果概要”“审查结果链接”“有效期提示”三项' >/dev/null
unzip -p "$output_dir/everyline-review-config-skill.zip" references/management.md | grep -F '规则分组' >/dev/null
