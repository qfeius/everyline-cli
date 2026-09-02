#!/usr/bin/env sh
set -eu

temporary_dir=$(mktemp -d)
trap 'rm -rf "$temporary_dir"' EXIT HUP INT TERM
package_root=.

# 有期望版本时在临时包根同步 manifest，保持工作区 package.json 不变。
if [ -n "${EXPECTED_VERSION:-}" ]; then
  package_root="$temporary_dir/package"
  mkdir -p "$package_root"
  cp package.json README.md "$package_root/"
  mkdir -p "$package_root/docs"
  cp docs/everyline-cli-skill-guide.md docs/everyline-cli-skill-interaction-scenarios.md "$package_root/docs/"
  # 临时安装包与正式发布包保持一致，包含可独立导入的 Agent Skill。
  cp -R bin scripts skills "$package_root/"
  node - "$package_root/package.json" "$EXPECTED_VERSION" <<'NODE'
const { readFileSync, writeFileSync } = require("node:fs");

const path = process.argv[2];
const packageData = JSON.parse(readFileSync(path, "utf8"));
packageData.version = process.argv[3];
writeFileSync(path, `${JSON.stringify(packageData, null, 2)}\n`);
NODE
fi

(cd "$package_root" && npm pack --json --pack-destination "$temporary_dir") > "$temporary_dir/pack.json"
package_file=$(node -e 'const value=require(process.argv[1]); process.stdout.write(value[0].filename)' "$temporary_dir/pack.json")
npm install --silent --prefix "$temporary_dir/install" "$temporary_dir/$package_file"
output=$("$temporary_dir/install/node_modules/.bin/everyline-cli" version --output json)
package_version=$(node -e 'process.stdout.write(require(process.argv[1]).version)' "$temporary_dir/install/node_modules/everyline-cli/package.json")

# 外部消费者应能从 npm 包中取得完整 Skill，而无需 postinstall 写入用户目录。
test -f "$temporary_dir/install/node_modules/everyline-cli/skills/everyline-cli/SKILL.md"
test -f "$temporary_dir/install/node_modules/everyline-cli/docs/everyline-cli-skill-guide.md"
test -f "$temporary_dir/install/node_modules/everyline-cli/docs/everyline-cli-skill-interaction-scenarios.md"
# 安装包必须保留主体展示项到后端 name/role 的映射，避免发布后回退为两个字段都填写公司名称。
grep -F '用户回复完整展示项 `猎聘123（乙方）`' "$temporary_dir/install/node_modules/everyline-cli/skills/everyline-cli/references/review-flow.md" >/dev/null
grep -F '`selectedPosition=猎聘123`、`selectedAuditRole=乙方`' "$temporary_dir/install/node_modules/everyline-cli/skills/everyline-cli/references/review-flow.md" >/dev/null

# 全局安装必须在同一次 npm 生命周期中登记 Codex Skill；测试目录显式隔离，避免写入执行者的真实用户目录。
global_prefix="$temporary_dir/global"
codex_skills_dir="$temporary_dir/codex-skills"
EVERYLINE_CODEX_SKILLS_DIR="$codex_skills_dir" npm install --silent --global --allow-scripts=everyline-cli --prefix "$global_prefix" "$temporary_dir/$package_file"
global_package_root=$(npm root --global --prefix "$global_prefix")
skill_target="$codex_skills_dir/everyline-cli"
test -L "$skill_target"
test -f "$skill_target/SKILL.md"
node - "$skill_target" "$global_package_root/everyline-cli/skills/everyline-cli" <<'NODE'
const { realpathSync } = require("node:fs");

if (realpathSync(process.argv[2]) !== realpathSync(process.argv[3])) {
  throw new Error("Codex Skill 未指向当前安装包中的 Skill");
}
NODE
global_output=$("$global_prefix/bin/everyline-cli" version --output json)

# 可选的期望版本同时约束 manifest 和 ldflags，防止两个发布版本源漂移。
if [ -n "${EXPECTED_VERSION:-}" ]; then
  test "$package_version" = "$EXPECTED_VERSION"
  printf '%s' "$output" | grep -F '"version": "'"$EXPECTED_VERSION"'"' >/dev/null
  printf '%s' "$global_output" | grep -F '"version": "'"$EXPECTED_VERSION"'"' >/dev/null
fi
printf '%s\n' "$output"
