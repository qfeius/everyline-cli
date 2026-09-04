# AI 变更记录：auth 用户 OAuth 授权

## 2026-08-20

- 为 Profile 增加非敏感 user OAuth 配置：metadata URL、business type、public client ID、loopback redirect URL 和 scopes。
- dev/test/prod 环境预设包含 `contract-review` OAuth metadata 和 `127.0.0.1:8000/login` 回调，client ID 由首次 user 授权动态注册；blue 不猜测未确认的 endpoint。
- `auth login --as user` 在没有直接 token 时走 authorization code + PKCE（S256），输出授权链接并尝试打开浏览器，接收 loopback callback 后换取并缓存 user token。
- callback 校验 state、code 和 OAuth error；OAuth code、code verifier、token 不写入 Profile，不输出到 stdout/stderr 日志。
- 保留 `--no-open-browser` 手动复制链接模式；user 不再保留 raw access token 兼容路径。

协议依据：`contract-cli` OAuth 实现、`contract/common-organization-v2` authorization server、`contract/open-platform` PKCE 集成和当前 dev/test/prod 发布配置。

验证：

- `go test ./internal/auth ./internal/cli`
- `go test ./...`
- `go vet ./...`
- `go build -o /tmp/everyline-cli-auth-check ./cmd/everyline-cli`
