# AI 变更记录：CLI 帮助直属子命令显示

## 2026-08-20

- 修正自定义帮助模板的 `Available Commands` 展示逻辑：只显示当前命令的直属子命令名称。
- `everyline-cli review --help` 现在显示 `run`、`file`、`subject`、`task`，不再显示 `review run` 等重复父路径。
- `everyline-cli review task --help` 同样只显示 `start`、`result`、`status`、`info`。
- 保持完整 Usage、CommandPath、命令调用方式、参数解析和其他帮助区域不变。
- 新增 `review` 与 `review task` 的公开帮助输出回归测试。

验证：

- `go test ./internal/cli -run '^TestHelpListsDirectChildSyntaxOnly$' -count=1`
- `go test ./internal/cli -run 'TestHelpListsDirectChildSyntaxOnly|TestHelpUsesWorkflowCommandOrder|TestHelpGroupsTopLevelCommands|TestHelpRendersCommandSyntaxAndNotes' -count=1`
- `go test ./... -count=1`
- `go vet ./...`
- `go test -race ./internal/cli -count=1`
