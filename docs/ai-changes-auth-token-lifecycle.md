# AI 变更记录：auth token 生命周期

## 2026-08-20

- Token 增加可选 `issued_at`，app token 使用服务端 `expire` 计算 `expires_at`，OAuth user token 使用服务端 `expires_in` 计算 `expires_at`，并保留 `token_type`、`scope`、`refresh_token`。
- 移除 `EVERYLINE_USER_ACCESS_TOKEN`、`EVERYLINE_ACCESS_TOKEN` 和 `--access-token-stdin` 的固定 24 小时假设；无服务端过期信息时按未知过期处理，不输出虚假时间。
- `auth status` 对未知过期 token 输出 `expiresKnown=false`，对已知过期 token 输出真实到期时间和剩余秒数；旧版 token JSON 缺失新增字段时仍按零值兼容。
- 已核对当前 `common-organization-v2` token handler 只支持 `authorization_code`，本次不猜测或发送 `refresh_token` grant；refresh token 仅作为本地协议元数据保存。

验证：

- `go test ./internal/auth -run 'TestProviderUserTokenFromEnvironment|TestProviderLoginForIdentityDoesNotAssumeUserTokenExpiry|TestLoginUserOAuthNoOpenBrowserPrintsLink' -count=1`
- `go test ./internal/cli -run 'TestAuthStatusUnknownExpiryDoesNotRenderFakeTime|TestAuthStatusTrimsEnvironmentToken|TestAuthLoginHonorsRootTimeout' -count=1`

