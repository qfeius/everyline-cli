# 签名审查预览链接规格增量

## Requirement: CLI 必须原样保留后端签名预览链接

EveryLine CLI MUST 保留 `task/info.data.url` 的完整值，并在链接有效时提供值完全相同的 `reviewDetailUrl`。

### Scenario: 后端返回签名链接

- **GIVEN** `task/info.data.url` 包含后端签发的完整预览 URL
- **WHEN** `review task result` 获取成功详情
- **THEN** 输出中的 `url` 必须与后端值完全一致
- **AND** `reviewDetailUrl` 必须与 `url` 完全一致

## Requirement: Skill 必须完整展示签名预览链接

EveryLine Skill MUST 把 CLI 返回的 `reviewDetailUrl` 作为面向用户的完整免登录链接展示，不得解析、脱敏、改写或重新拼接。

### Scenario: 链接包含 token 查询参数

- **GIVEN** CLI 返回的 `reviewDetailUrl` 包含 `token` 查询参数
- **WHEN** Agent 返回审查成功结果
- **THEN** 必须完整原样展示该链接
- **AND** 必须提示链接默认两小时有效
- **AND** 不得单独提取或输出其中的 token

### Scenario: 后端未返回链接

- **GIVEN** 审查任务成功但 CLI 未返回 `reviewDetailUrl`
- **WHEN** Agent 返回审查结果
- **THEN** 必须说明任务成功但链接缺失
- **AND** 不得自行拼接详情地址
