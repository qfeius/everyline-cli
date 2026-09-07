# AI 变更记录：user Profile 无 app-id 授权

## 2026-08-20

- 将 Profile 校验拆为公共校验与身份感知校验；`user` 身份不再要求 `app-id`，`app` 身份继续要求 `app-id`。
- 移除 `config add` 对 `--app-id` 的无条件 Cobra 必填约束；默认身份为 `user` 时可直接使用环境预设和 OAuth 配置创建 Profile。
- 保持 `auth login --as user` 只走浏览器 OAuth/PKCE，不读取或提交 app 凭证；保持 app 登录的 app-id 校验。
- 更新命令帮助、README、命令参考、架构说明和测试计划，补充 user 无 app-id 的实践示例。

验证：

- `go test ./internal/config ./internal/cli ./internal/auth -count=1`
- `go test ./...`
- `go test -race ./internal/auth ./internal/cli ./internal/review`
- `go vet ./...`
- `go test ./internal/cli -run 'TestConfigAdd(UserProfileDoesNotRequireAppID|AppProfileStillRequiresAppID)|TestAuthUserLoginUsesBrowserOAuth' -count=1`
