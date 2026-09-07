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
output=$(EVERYLINE_CONFIG_DIR="$temporary_dir/local-config" "$temporary_dir/install/node_modules/.bin/everyline-cli" version --output json)
package_version=$(node -e 'process.stdout.write(require(process.argv[1]).version)' "$temporary_dir/install/node_modules/everyline-cli/package.json")

# 外部消费者应能从 npm 包中取得合并后的三项职责分离 Skill。
test -f "$temporary_dir/install/node_modules/everyline-cli/skills/everyline-cli/SKILL.md"
test ! -e "$temporary_dir/install/node_modules/everyline-cli/skills/everyline-shared"
test -f "$temporary_dir/install/node_modules/everyline-cli/skills/everyline-review/SKILL.md"
test -f "$temporary_dir/install/node_modules/everyline-cli/skills/everyline-review/references/review-flow.md"
test -f "$temporary_dir/install/node_modules/everyline-cli/skills/everyline-review-config/SKILL.md"
test -f "$temporary_dir/install/node_modules/everyline-cli/skills/everyline-review-config/references/management.md"
test -f "$temporary_dir/install/node_modules/everyline-cli/docs/everyline-cli-skill-guide.md"
test -f "$temporary_dir/install/node_modules/everyline-cli/docs/everyline-cli-skill-interaction-scenarios.md"
# 安装包必须保留主体展示项到后端 name/role 的映射，避免发布后回退为两个字段都填写公司名称。
grep -F '用户回复完整展示项 `猎聘123（乙方）`' "$temporary_dir/install/node_modules/everyline-cli/skills/everyline-review/references/review-flow.md" >/dev/null
grep -F '`selectedPosition=猎聘123`、`selectedAuditRole=乙方`' "$temporary_dir/install/node_modules/everyline-cli/skills/everyline-review/references/review-flow.md" >/dev/null

# 全局安装必须在同一次 npm 生命周期中登记 Codex 与 WorkBuddy Skills；两个宿主目录都显式隔离。
global_prefix="$temporary_dir/global"
codex_skills_dir="$temporary_dir/codex-skills"
workbuddy_skills_dir="$temporary_dir/workbuddy-skills"
doubao_skills_dir="$temporary_dir/doubao-workspace/.user_skills"
global_config_dir="$temporary_dir/global-config"
EVERYLINE_CONFIG_DIR="$global_config_dir" EVERYLINE_CODEX_SKILLS_DIR="$codex_skills_dir" EVERYLINE_WORKBUDDY_SKILLS_DIR="$workbuddy_skills_dir" EVERYLINE_DOUBAO_SKILLS_DIR="$doubao_skills_dir" npm install --silent --global --allow-scripts=everyline-cli --prefix "$global_prefix" "$temporary_dir/$package_file"
global_package_root=$(npm root --global --prefix "$global_prefix")
for skill_name in everyline-cli everyline-review everyline-review-config; do
  codex_skill_target="$codex_skills_dir/$skill_name"
  workbuddy_skill_target="$workbuddy_skills_dir/$skill_name"
  test -L "$codex_skill_target"
  test -f "$codex_skill_target/SKILL.md"
  test -L "$workbuddy_skill_target"
  test -f "$workbuddy_skill_target/SKILL.md"
  # 豆包读取普通技能文件夹；比较整个目录，覆盖 references 随包更新。
  test ! -L "$doubao_skills_dir/$skill_name"
  diff -r "$global_package_root/everyline-cli/skills/$skill_name" "$doubao_skills_dir/$skill_name"
  node - "$codex_skill_target" "$workbuddy_skill_target" "$global_package_root/everyline-cli/skills/$skill_name" <<'NODE'
const { realpathSync } = require("node:fs");

for (const target of [process.argv[2], process.argv[3]]) {
  if (realpathSync(target) !== realpathSync(process.argv[4])) {
    throw new Error("Agent Skill 未指向当前安装包中的 Skill");
  }
}
NODE
done
test -f "$global_config_dir/install-state.json"
grep -F '"authorizationRequired": true' "$global_config_dir/install-state.json" >/dev/null
global_output=$(EVERYLINE_CONFIG_DIR="$global_config_dir" "$global_prefix/bin/everyline-cli" version --output json)
printf '%s' "$global_output" | grep -F '"firstInstall": true' >/dev/null
printf '%s' "$global_output" | grep -F '"authorizationRequired": true' >/dev/null

# 回退隔离夹具为旧安装器留下的布局：没有状态文件，只有旧入口及两项业务 Skill，保留 app/user 凭证。
node - "$global_config_dir" "$codex_skills_dir" "$workbuddy_skills_dir" "$global_package_root/everyline-cli" <<'NODE'
const { join } = require("node:path");
const { unlinkSync, symlinkSync, writeFileSync } = require("node:fs");
const [configDirectory, codexRoot, workBuddyRoot, packageRoot] = process.argv.slice(2);
unlinkSync(join(configDirectory, "install-state.json"));
for (const skillRoot of [codexRoot, workBuddyRoot]) {
  unlinkSync(join(skillRoot, "everyline-cli"));
  symlinkSync(join(packageRoot, "skills", "everyline-shared"), join(skillRoot, "everyline-shared"), "dir");
}
writeFileSync(join(configDirectory, "config.json"), JSON.stringify({
  current_profile: "migration-test",
  profiles: {
    "migration-test": {
      name: "migration-test", base_url: "https://api.example.com", token_url: "https://api.example.com/token",
      app_id: "fixture-app", default_identity: "user", default_output: "json",
    },
  },
}));
writeFileSync(join(configDirectory, "tokens.json"), JSON.stringify({
  "migration-test": { access_token: "fixture-app-token", expires_at: new Date(Date.now() + 3600000).toISOString() },
  "migration-test::user": { access_token: "fixture-user-token", expires_at: new Date(Date.now() + 3600000).toISOString() },
}));
NODE
# 执行真实 postinstall，并由原生 CLI 读取其迁移状态，覆盖 Node 与 Go 的共享状态协议。
npm_config_global=true EVERYLINE_CONFIG_DIR="$global_config_dir" EVERYLINE_CODEX_SKILLS_DIR="$codex_skills_dir" EVERYLINE_WORKBUDDY_SKILLS_DIR="$workbuddy_skills_dir" EVERYLINE_DOUBAO_SKILLS_DIR="$doubao_skills_dir" node "$global_package_root/everyline-cli/scripts/install.js" > "$temporary_dir/upgrade.txt"
EVERYLINE_CONFIG_DIR="$global_config_dir" "$global_prefix/bin/everyline-cli" version --output json > "$temporary_dir/upgrade-version.json"
for identity in app user; do
  EVERYLINE_CONFIG_DIR="$global_config_dir" "$global_prefix/bin/everyline-cli" auth status --profile migration-test --as "$identity" --output json > "$temporary_dir/upgrade-$identity.json"
done
node - "$temporary_dir" <<'NODE'
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const directory = process.argv[2];
const event = JSON.parse(readFileSync(join(directory, "upgrade.txt"), "utf8").trim().split("\n").at(-1));
assert.equal(event.event, "updated");
assert.equal(event.nextAction, "auth_status");
const version = JSON.parse(readFileSync(join(directory, "upgrade-version.json"), "utf8"));
assert.equal(version.firstInstall, false);
assert.equal(version.authorizationRequired, false);
for (const identity of ["app", "user"]) {
  const status = JSON.parse(readFileSync(join(directory, `upgrade-${identity}.json`), "utf8"));
  assert.equal(status.authenticated, true);
  assert.equal(status.source, "cache");
}
NODE

# 可选的期望版本同时约束 manifest 和 ldflags，防止两个发布版本源漂移。
if [ -n "${EXPECTED_VERSION:-}" ]; then
  test "$package_version" = "$EXPECTED_VERSION"
  printf '%s' "$output" | grep -F '"version": "'"$EXPECTED_VERSION"'"' >/dev/null
  printf '%s' "$global_output" | grep -F '"version": "'"$EXPECTED_VERSION"'"' >/dev/null
fi
printf '%s\n' "$output"
