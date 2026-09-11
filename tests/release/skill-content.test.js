"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { readFileSync, readdirSync, existsSync } = require("node:fs");
const { join, dirname, resolve } = require("node:path");

/**
 * 验证安装入口统一使用 latest，同时保留 blue 业务环境校验，避免渠道迁移改变请求目标。
 * 入参：无；读取发布文档和两个 Skill。返回值：void，出现旧安装渠道或环境校验缺失时断言失败。
 */
test("skill installation uses latest while business profiles remain blue", () => {
  const root = resolve(__dirname, "../..");
  const review = readFileSync(join(root, "skills/everyline-review/SKILL.md"), "utf8");
  const config = readFileSync(join(root, "skills/everyline-review-config/SKILL.md"), "utf8");
  // 渠道只负责安装和更新；两项技能仍必须验证实际 blue 连接地址。
  for (const content of [review, config]) {
    assert.ok(content.includes("@qfeius/everyline-cli@latest"));
    assert.ok(content.includes("固定使用 `blue` 环境"));
    assert.ok(content.includes("实际连接地址"));
    assert.ok(content.includes("npm 渠道和版本号不表示业务环境"));
  }
  for (const path of ["README.md", "docs/npm-release.md", "docs/everyline-cli-skill-guide.md", "docs/everyline-cli-skill-interaction-scenarios.md", "skills/everyline-review/SKILL.md", "skills/everyline-review-config/SKILL.md"]) {
    const content = readFileSync(join(root, path), "utf8");
    assert.ok(!content.includes("@qfeius/everyline-cli@blue"), `${path} 残留 blue 安装命令`);
    assert.ok(!content.includes("<版本>-blue.<序号>"), `${path} 残留 blue 发布版本格式`);
    assert.ok(!content.includes("--tag blue"), `${path} 残留 blue 发布渠道`);
  }
});

/**
 * 验证发布 Skill 的完整描述、依赖与本地链接，防止替换正文后残留失效引用。
 * 入参：无；读取仓库 Skill 源文件。返回值：void，结构或引用不完整时断言失败。
 */
test("two skills preserve review triggers and resolve local references", () => {
  const root = resolve(__dirname, "../../skills");
  const version = require("../../package.json").version;
  assert.deepEqual(readdirSync(root).filter(name => !name.startsWith(".")).sort(), ["everyline-review", "everyline-review-config"]);
  const review = readFileSync(join(root, "everyline-review/SKILL.md"), "utf8");
  const config = readFileSync(join(root, "everyline-review-config/SKILL.md"), "utf8");
  const description = review.match(/^description: (.+)$/m)[1];
  for (const phrase of ["面向 Codex / 豆包 / WorkBuddy 用户", "用户上传合同", "帮我审查合同", "这份合同有没有风险", "这份合同能不能签", "这份合同有没有问题", "必须使用且优先使用本 Skill", "买卖、采购、服务、委托、租赁、保密", "转交 everyline-review-config"]) {
    assert.ok(description.includes(phrase), `description 缺少 ${phrase}`);
  }
  assert.ok(config.includes('skills: ["everyline-review"]'));
  assert.ok(!review.includes("    skills:"), "review 不应形成反向依赖");
  for (const content of [review, config]) {
    assert.ok(content.includes(`version: "${version}"`));
    assert.ok(content.includes('bins: ["everyline-cli"]'));
    assert.ok(!content.includes("everyline-shared"));
    assert.ok(!content.includes("EVERYLINE_SKIP_SKILL_INSTALL=1 npm"), "默认安装示例应登记 Skill");
  }
  const paths = ["everyline-review/SKILL.md", "everyline-review-config/SKILL.md"];
  for (const name of ["everyline-review", "everyline-review-config"]) {
    assert.deepEqual(readdirSync(join(root, name)), ["SKILL.md"], "两个 Skill 各自仅保留平级入口文件");
  }
  // 配置入口由宿主加载独立 Skill，文内锚点继续由下方通用链接检查覆盖。
  assert.ok(review.includes('通过宿主技能加载能力读取 `everyline-review-config`'));
  for (const name of paths) {
    const path = join(root, name);
    const content = readFileSync(path, "utf8");
    for (const match of content.matchAll(/\]\(([^)]+)\)/g)) {
      const target = match[1];
      if (target.startsWith("<") || /^[a-z]+:/i.test(target)) continue;
      const [file, anchor] = target.split("#");
      const destination = file ? resolve(dirname(path), file) : path;
      assert.ok(existsSync(destination), `${name} 引用缺失 ${target}`);
      if (anchor) assert.ok(readFileSync(destination, "utf8").includes(`id="${anchor}"`), `${name} 锚点缺失 ${target}`);
    }
  }
  assert.ok(!existsSync(join(root, "everyline-review/references/review-flow.md")));
  assert.ok(!existsSync(join(root, "everyline-review-config/references/management.md")));
});
