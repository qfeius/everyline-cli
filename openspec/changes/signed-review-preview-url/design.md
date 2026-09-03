# 设计：透传并展示签名审查预览链接

## 现状

CLI 已复制 `task/info` 完整响应，并把有效的顶层 `url` 规范化为 `reviewDetailUrl`。Skill 同时存在“返回 `reviewDetailUrl`”和“不输出 token”两条规则，Agent 可能因链接包含 `token` 查询参数而隐藏或改写链接。

## 方案

- CLI 生产代码保持不变，通过回归测试锁定 `url` 与 `reviewDetailUrl` 的字节级等值关系。
- Skill 将 access token 与签名预览链接明确区分：前者继续保密，后者作为完整用户链接原样展示。
- 签名链接缺失时继续停止于真实结果，不在 Agent 层推导页面域名或查询参数。

## 兼容性

命令、JSON 字段、后端接口和授权流程均保持不变。已有调用方仍可读取原始 `url`，依赖稳定字段的 Agent 可读取 `reviewDetailUrl`。

## 风险与回滚

签名链接在有效期内具备访问能力，因此 Skill 只允许作为完整链接交付给当前用户，不允许拆解 token。若需要回滚，恢复 Skill 原文和对应测试断言即可，不涉及数据迁移。
