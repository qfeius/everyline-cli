# AI 变更记录：auth 用户 OAuth 授权

## 2026-09-04

- 豆包与 WorkBuddy user 恢复为固定的 OAuth Device Grant：dev/test 使用独立 Device client `zscli_c77221e810ce3977`，`auth init` 不再动态注册，也不复用浏览器 client。
- Codex 本地 user 保持 OAuth Authorization Code + PKCE；每次显式 `auth login` 从 metadata 的 `registration_endpoint` 动态注册浏览器 public client。
- prod、blue 和自定义环境不猜测 Device client；使用 Device Grant 时通过 `oauth_device_client_id` 显式配置平台确认值。

## 2026-08-20

- 为 Profile 增加非敏感 user OAuth 配置：metadata URL、business type、public client ID、loopback redirect URL 和 scopes。
- dev/test/prod 环境预设包含 `contract-review` OAuth metadata 和 `127.0.0.1:8000/login` 回调；Codex 每次显式 user 登录读取 metadata 的 `registration_endpoint`，动态注册并替换浏览器 client ID，blue 不猜测未确认的 endpoint。
- `auth login --as user` 在没有直接 token 时走 authorization code + PKCE（S256），输出授权链接并尝试打开浏览器，接收 loopback callback 后换取并缓存 user token。
- callback 校验 state、code 和 OAuth error；OAuth code、code verifier、token 不写入 Profile，不输出到 stdout/stderr 日志。
- 保留 `--no-open-browser` 手动复制链接模式；user 不再保留 raw access token 兼容路径。

协议依据：`contract-cli` OAuth 实现、`contract/common-organization-v2` authorization server、`contract/open-platform` PKCE 集成和当前 dev/test/prod 发布配置。

验证：

- `go test ./internal/auth ./internal/cli`
- `go test ./...`
- `go vet ./...`
- `go build -o /tmp/everyline-cli-auth-check ./cmd/everyline-cli`
