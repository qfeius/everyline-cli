# AI 变更记录：CLI 帮助 flags 顺序

## 2026-08-20

- 调整帮助模板的本地 flags 渲染：命令业务 flags 保持字母序并连续显示，自动生成的 `-h/--help` 统一放在本地 flags 末尾。
- 保持 flag 名称、短名称、默认值、解析行为和 `Global Flags` 区域不变。
- 新增 review upload、review task start、config add、auth login 的跨命令帮助顺序回归测试。

## 2026-08-20（补充）

- 根据反馈继续调整分组：业务参数位于 `Flags` 顶部，`--dry-run`/`--print-input` 等通用操作参数随后，`-h/--help` 最后。
- `review file upload --help` 现在按 `--app-type`、`--business-id`、`--file`、`--name`、`--dry-run`、`--print-input`、`--help` 展示。

后续需求 `CLI-REMOVE-APP-TYPE-001` 已移除 review 相关命令的 `--app-type` 参数；上面的输出记录保留为本次帮助顺序调整当时的历史证据。

验证：

- `go test ./internal/cli -run '^TestHelpPlacesCommandFlagsBeforeHelpFlag$' -count=1`
- `go test ./...`
- `go vet ./...`
- `go test -race ./internal/auth ./internal/cli`
- `go build -o /tmp/everyline-cli-help-flag-order-check ./cmd/everyline-cli`
