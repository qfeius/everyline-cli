# AI 变更记录：收敛 user OAuth 授权入口

## 2026-08-20

- 删除 `auth login --as user` 的 `--access-token-stdin`，不再接受 `EVERYLINE_USER_ACCESS_TOKEN`。
- 删除 user 原始 Bearer token 的 CLI/Provider 直传链路；user 登录统一使用 authorization code + PKCE、loopback callback 和浏览器授权。
- 保留 `--app-secret-stdin`，它仍是 app 身份安全输入 secret 的正式入口。
- `auth status --as user` 不再把原始 token 环境变量报告为已认证；缺少 OAuth 配置时直接返回配置错误。
- 更新帮助、README、命令参考、架构和测试计划，避免把已删除的 user token 入口作为当前用法。

验证：

- `go test ./internal/auth ./internal/cli -run 'TestProviderUserTokenFromEnvironment|TestAuthUserLoginIgnoresLegacyEnvironmentToken|TestAuthStatusIgnoresLegacyEnvironmentToken|TestHelpRendersCommandSyntaxAndNotes|TestAuthUserLoginUsesBrowserOAuth' -count=1`
