# AI 变更记录：移除 CLI 的 `--app-type` 参数

## 2026-08-20

- 移除 `review file upload`、`review subject extract`、`review task status/info/result` 的 `--app-type` 参数注册。
- CLI 内部默认使用 `THIRD_PARTY`，继续满足当前上传、主体提取、任务发起和任务查询的 API 契约。
- `visibility-scope=contractResult` 不再要求用户提供已删除的 flag；CLI 自动补齐默认 `appType`，仍要求 `--business-id`。
- 保留 `review run` JSON 输入中的 `appType` 字段，作为高级兼容输入；省略时默认使用 `THIRD_PARTY`。
- 旧 `--app-type` 参数现在返回 `unknown flag`，不做静默兼容。

验证：

- `go test ./internal/cli -run 'TestReviewHelpDoesNotExposeAppTypeFlag|TestReviewFileUploadRejectsRemovedAppTypeFlag|TestReviewSubjectExtractDryRun|TestReviewTaskResultPollsAndRendersInfo|TestHelpPlacesCommandFlagsBeforeHelpFlag' -count=1`
- `go test ./...`
- `go vet ./...`
- `go test -race ./internal/auth ./internal/cli`
- `go build -o /tmp/everyline-cli-remove-app-type-check ./cmd/everyline-cli`
