# AI 变更记录：macOS Keychain 优先存储 app secret

## 变更内容

- macOS 默认使用用户 Keychain 保存和读取 app secret，service 为 `everyline-cli.app-secret`，Profile 名称作为 account。
- Keychain 不可用、条目不存在或命令失败时，回退到现有 `secrets.json`；非 macOS 继续使用文件存储。
- `--app-secret`、`--app-secret-stdin` 和环境变量仍优先于本地存储，满足 CI/Agent 场景。
- 使用 `security add-generic-password` 时将 `-w` 放在末尾，secret 通过 stdin 传递，不进入命令参数。
- 现有 token 文件、user OAuth、`auth logout` 和显式 `--save-app-secret` 语义保持不变；不自动删除旧文件或 Keychain 条目。

## 验证

- Keychain 单元测试和文件回退测试通过。
- 回归：`go test ./...` 通过。
- 真实用户 Keychain 未作为测试依赖；测试使用临时 `security` 替身。一次早期测试产生的精确条目已确认并删除。

## 变更记录

- 设计：`docs/design-auth-keychain.md`
- 实现：`internal/auth/secrets.go`、`internal/cli/runtime.go`
- 测试：`internal/auth/secrets_test.go`、`internal/cli/runtime_test.go`
