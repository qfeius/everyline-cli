#!/usr/bin/env sh
set -eu

# repository_root 是包含 skills/ 和 dist/ 的源码根目录，避免从其他工作目录执行时写错位置。
repository_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
output_dir=${1:-"$repository_root/dist"}
skill_names="everyline-cli everyline-review everyline-review-config"

node "$repository_root/scripts/sync-skill-versions.js"

mkdir -p "$output_dir"
# 合并后不再发布 everyline-shared；清理同一输出目录中的旧 ZIP，避免上传四项 Skill。
rm -f "$output_dir/everyline-shared-skill.zip"

for skill_name in $skill_names; do
  skill_source="$repository_root/skills/$skill_name"
  archive="$output_dir/$skill_name-skill.zip"
  test -f "$skill_source/SKILL.md"

  # 豆包导入要求 ZIP 根目录直接出现 SKILL.md，因此只在 Skill 源目录内执行打包。
  rm -f "$archive"
  if [ -d "$skill_source/references" ]; then
    (cd "$skill_source" && zip -qr "$archive" SKILL.md references)
  else
    (cd "$skill_source" && zip -qr "$archive" SKILL.md)
  fi
  printf '%s\n' "$archive"
done
