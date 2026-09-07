# AI 变更记录：auth app secret 本地存储

## 2026-08-20

- 新增独立的 `secrets.json` 文件存储，只保存按 Profile 隔离的 app secret，不混入 Profile 配置或 token 缓存。
- 新增 `auth login --save-app-secret`，仅在 token endpoint 授权成功后保存本次使用的 secret；flag、stdin 和环境变量仍默认只驻留当前进程。
- app secret 解析顺序调整为显式 flag、stdin、Profile 专用环境变量、通用环境变量、本地保存值；token 过期后业务 Provider 可复用本地值重新获取 app token。
- secrets 目录/文件权限分别收紧为 `0700/0600`，使用跨进程文件锁和临时文件原子替换；授权失败不创建或改写 secret 文件。
- `auth status` 仅输出 `appSecretConfigured` 布尔值；`auth logout` 保持只删除 token 缓存，不删除已保存 secret；user 身份拒绝保存开关。

验证：

- `go test ./internal/cli -run 'TestAuthAppLoginExplicitlySavesSecret|TestAuthAppLoginUsesSavedSecret|TestAuthAppLoginFailureDoesNotSaveSecret|TestAuthUserLoginRejectsSaveAppSecret' -count=1`
- `go test ./internal/auth -run 'TestFileSecretStoreSavesProfilesWithPrivatePermissions|TestFileSecretStoreConcurrentInstancesPreserveProfiles|TestProviderUsesSavedSecretAfterExpiredToken' -count=1`
- `go test ./...`
- `go vet ./...`
- `go test -race ./internal/auth ./internal/cli`
- `go build -o /tmp/everyline-cli-auth-secret-storage-check ./cmd/everyline-cli`

