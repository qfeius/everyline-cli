#!/usr/bin/env sh
set -eu

output_dir=${1:-dist}

for skill_name in everyline-review everyline-review-config; do
  archive="$output_dir/$skill_name-skill.zip"
  test -s "$archive"
  unzip -Z1 "$archive" | grep -Fx 'SKILL.md' >/dev/null
  unzip -p "$archive" SKILL.md | grep -F "name: $skill_name" >/dev/null
done
test ! -e "$output_dir/everyline-shared-skill.zip"

# 三端共用的授权、沙箱附件和签名链接约束必须进入豆包最终制品。
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F 'auth init --profile <profile> --as user --output json' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '不得执行 `auth login --profile <profile> --as user`' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '豆包的“本地电脑”模式仍属于豆包' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F 'SESSION_ID=<same-session-id> everyline-cli auth init' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F 'SESSION_ID=<same-session-id> everyline-cli auth complete' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F 'firstInstall=true 且 authorizationRequired=true' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F 'auth init --restart --profile <profile> --as user --output json' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '`auth init` 返回 `reused=true`' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '统一固定使用 `blue` 环境' >/dev/null
# 显式环境和配置管理入口同样受 blue 限制，避免发布包恢复旧的环境覆盖规则。
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '非 blue Profile 报告环境不匹配并停止' >/dev/null
unzip -p "$output_dir/everyline-review-config-skill.zip" SKILL.md | grep -F '显式指定其他环境或实际连接地址不属于 blue 的 Profile 时，报告环境不匹配并停止' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '当前 Profile 是 dev、test 或 prod 时不得继承它' >/dev/null
# 固定 blue 的 Device client 必须随 Skill 发布，避免授权引导要求额外配置或沿用 test client。
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F 'blue 使用预设的独立 EveryLine Device client `zscli_bc60fee4de9913ae`' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '发起任何授权事务前必须先固定 `user/app` 身份' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F 'WorkBuddy 固定调用 `AskUserQuestion` 并设置 `multiSelect=false`' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '[点击授权](<FULL_AUTHORIZATION_URL>)' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '[点击授权](<verification_uri_complete>)' >/dev/null
# 校验豆包导入包确实来自当前源码，包含 CLI 入口文案字段与宿主更新检查。
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | cmp - skills/everyline-review/SKILL.md
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '`verification_link_text`' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F 'EVERYLINE_DOUBAO_SKILLS_DIR' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F 'nextAction=reload_skills' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F -- '--stdin --name <filename>' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '0. 通用审查清单（系统内置）' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '不展示图表' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '不得单独展示 `taskId`' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '不追加“已完成”' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '[查看详情](<REVIEW_DETAIL_URL>)' >/dev/null
unzip -p "$output_dir/everyline-review-skill.zip" SKILL.md | grep -F '不展开原始终态对象' >/dev/null
unzip -p "$output_dir/everyline-review-config-skill.zip" SKILL.md | grep -F '规则分组' >/dev/null

test ! -e "$output_dir/everyline-cli-skill.zip"

# 两个平级 Skill 的原文都要进入制品，避免发布旧描述或遗留嵌套文件。
unzip -p "$output_dir/everyline-review-config-skill.zip" SKILL.md | cmp - skills/everyline-review-config/SKILL.md
for skill_name in everyline-review everyline-review-config; do
  test "$(unzip -Z1 "$output_dir/$skill_name-skill.zip")" = "SKILL.md"
done
