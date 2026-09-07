# 提案：透传并展示签名审查预览链接

## 用户价值

后端已经在 `task/info.data.url` 返回默认两小时有效的免登录预览链接。CLI 与 Agent 应直接交付这条可访问链接，避免重新拼接、脱敏或遗漏签名参数导致详情页无权限。

## 范围

- 固化 CLI 对后端 `url` 的原样保留，并让 `reviewDetailUrl` 与其完全一致。
- 明确 EveryLine Skill 可以完整展示后端签发的 `reviewDetailUrl`，即使链接包含 `token` 查询参数。
- 保留 access token、app secret、授权码等授权凭证的保密规则。

## 验收标准

- 后端返回签名 `url` 时，CLI 输出的 `url` 和 `reviewDetailUrl` 均与原值完全一致。
- Skill 完整展示 `reviewDetailUrl`，不解析、脱敏、改写或单独输出其中的 token，并提示默认两小时有效。
- 后端未返回链接时，Skill 仍不自行拼接地址。

## 非目标

- 不修改后端接口、Token 签发或有效期。
- 不新增 CLI 命令、参数或远端调用。
- 不把预览链接中的 token 当作可复用 access token 暴露。
