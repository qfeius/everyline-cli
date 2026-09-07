# AI 变更记录：`appType` 可选化与 result 轮询状态反馈

## 需求

后端接口已兼容 `appType` 非必填；同时 `review task result` 长时间轮询时需要在命令行给出明确运行状态。

## 变更内容

- 删除 `ValidateAppType` 及其调用，不再对 `appType` 做本地必填/枚举校验。
- 上传、主体提取、任务查询和一键审查不再自动补 `THIRD_PARTY`；未提供时不序列化 `appType`，显式提供的兼容字段仍透传。
- 更新 start、subject、upload、task-query、run Schema，使 `appType` 成为可选字符串；`contractResult` 查询只保留 `businessId` 条件。
- `review task result` 通过轮询状态 observer 默认向 stderr 输出：`waiting`、`status=running`、`status=success` 等；stdout 仍只输出最终业务结果。
- 同步 README、API 映射、命令参考、result 设计和测试计划。

## 诊断、设计与交接

- 根因包：`docs/diagnosis-optional-apptype-result-progress.json`
- 设计：`docs/design-app-type-optional-result-progress.md`
- 修复交接包：`docs/fix-optional-apptype-result-progress.json`

## 验证

- `go test ./...` 通过。
- `go vet ./...` 通过。
- `go test -race ./internal/auth ./internal/cli ./internal/review` 通过。
- `git diff --check` 通过。
- `make build` 通过，并已更新 `/Users/cuizongbao/.local/bin/everyline-cli`。
- 真实安装二进制核验：无 appType 的 start、upload、subject dry-run 均通过且不输出 appType；显式 `appType=LEGACY` 可兼容透传；result 帮助保持可用。

## 影响与回滚

未提供 appType 的请求不再由 CLI 补入 `THIRD_PARTY`，依赖该默认字段的外部脚本应改为显式提供或依赖后端默认。result 新增 stderr 进度，不改变 stdout 和退出码。回滚可恢复 appType 默认注入/校验、Schema required 条件及移除状态 observer；无数据库和生产环境操作。
