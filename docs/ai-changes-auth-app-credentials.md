# AI 变更记录：auth 应用凭证输入

## 2026-08-19

- 增加 `auth login --app-id` 和 `--app-secret`，支持 Agent、CI 和本地工具直接提供 app 凭证。
- 增加 `EVERYLINE_APP_ID` 及 Profile 专用 app-id 环境变量，并保持 app secret 的既有专用环境变量兼容。
- 明确凭证来源优先级：flag > stdin/env > Profile；app secret 不写入 Profile 或 token 缓存。
- app 登录成功且 app-id 发生变化时，仅更新 Profile 中的非敏感 app-id；token 交换失败不修改原 app-id。
- user 授权流程未改变，浏览器 OAuth/PKCE 仍等待 everyline 自有认证服务协议确认。

验证：

- `go test ./internal/auth ./internal/cli`
- `go test ./...`
- `go vet ./...`
- `go build -o /tmp/everyline-cli-auth-check ./cmd/everyline-cli`
