# AI 变更记录：自更新与能力边界收口

## 变更内容

- 新增 `everyline-cli update --manifest-url <HTTPS_URL>`，支持独立二进制按平台检查和更新。
- manifest 使用 `version`、`platforms`、制品 `url` 和 `sha256` 字段；CLI 校验 HTTPS、SemVer、平台匹配和 SHA-256。
- 制品先下载到目标文件同目录临时文件，校验成功后原子替换并保留原文件权限；校验、下载或替换失败不修改原二进制。
- `--dry-run` 只校验 manifest、平台和版本，不下载或替换；npm/npx 薄包装启动时提示通过 npm 更新，不修改包内二进制。
- Windows 使用等待父进程退出的内部替换助手，支持正在运行的可执行文件完成更新。
- `review task result` 的状态边界明确为 `running`、`success`、`fail`；未知状态保留快照并报错，避免把未确认状态静默当作可等待状态。
- checklist/rule 四个批量创建/更新操作明确为非必需能力，保留 dry-run，真实请求继续在 HTTP 前失败关闭。
- 删除 appType 对上传业务上下文的本地强制校验；后端兼容字段仍可在内部请求边界按需透传，但不暴露为 CLI flag。
- README、命令参考、架构、API 映射和 result 行为契约改为当前行为表述，移除阶段性实现说明。

## 验证

- `go test ./...`
- `go test -race ./internal/update ./internal/review ./internal/cli`
- `go vet ./...`
- `npm test`
- `GOOS=windows GOARCH=amd64 go test -c ./internal/update`
- `GOOS=windows GOARCH=amd64 go build ./cmd/everyline-cli`

