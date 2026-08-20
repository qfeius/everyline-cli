# AI 变更记录：移除本地上传命令的 `--business-id`

## 需求

修正 `everyline-cli review file upload -h` 展示不存在业务语义的 `--business-id` 参数，并核对其他 review 命令的参数边界。

## 变更内容

- 删除 `review file upload` 的 `businessID` CLI 局部变量、`--business-id` flag、上传前业务上下文校验和 CLI 请求透传。
- 保留上传 API Schema、领域层 `UploadFile` 方法的 `businessID` 能力，确保 `review run` 和 CLM 兼容链路不被误删。
- 保留 `review subject extract` 及任务查询在各自契约场景下需要的 `--business-id`：主体提取必需；任务查询仅 `visibility-scope=contractResult` 必需。
- 更新命令参考：移除上传语法中的 `--business-id`，补齐 `checklist list` 的时间、创建人、更新人和排序过滤参数，并补充任务查询条件说明。
- 增加帮助、旧 flag 拒绝、dry-run 请求和业务参数边界回归测试。

## 诊断与设计

- 根因包：`docs/diagnosis-remove-upload-business-id.json`
- 设计：`docs/design-remove-upload-business-id.md`
- 修复交接包：`docs/fix-remove-upload-business-id.json`

## 验证

- `go test ./...` 通过。
- `go vet ./...` 通过。
- `go test -race ./internal/auth ./internal/cli` 通过。
- `git diff --check` 通过。
- `make build` 通过，并已更新 `/Users/cuizongbao/.local/bin/everyline-cli`。
- 已用真实安装二进制核验：上传帮助不再显示 `--business-id`；旧 flag 返回 `unknown flag`；dry-run 输出 `appType=THIRD_PARTY` 且不含 `businessId`；主体提取和任务 status 帮助仍保留 `--business-id`。

## 影响与回滚

这是 CLI 参数面的兼容性收窄：依赖 `review file upload --business-id` 的脚本将收到 `unknown flag`。如需回滚，恢复上传命令的局部变量、flag 注册、校验、请求透传及对应文档/测试断言即可；无数据库、生产环境或服务端发布变更。
