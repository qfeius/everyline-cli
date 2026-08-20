# AI 变更记录：收敛发起审查 CLI 必填契约

## 需求来源

- API 文档：《发起智能审查》
- 链接：https://ysi13ckdb9.feishu.cn/docx/CAPVdW7fUoCF4Jx0p0bcOYZVnK8
- 读取版本：revision 195
- 用户确认：`review task start` 与 `review run` 一起修改；`fileId` 字符串化不在本次范围；`fileHash` 使用上传接口返回值。

## 变更内容

- `review task start` 与 `review run` 的发起审查契约保留 `businessId`、`fileId`、`fileHash`、`config`，并允许配置 `selectedCheckListIds`（`array<string>`）和 `matchContractTypeRulePackage`（`boolean`）。
- `config` 强制要求 `selectedPosition`、`selectedAuditRole`、`reviewStrength`，`reviewStrength` 仅接受 `0`、`1`、`2`。
- CLI 不再接受未纳入 CLI 契约的发起审查非必填字段；未知字段通过严格 JSON 解码拒绝。
- 保留当前 `fileId` CLI 输入类型和内部类型，不扩展到其他命令的字符串化改造。
- 帮助输出增加字段说明、必填约束、两个规则来源可选字段、`fileHash` 来源说明和包含可选字段的 `--data` 示例。

## 影响面

- 直接影响：`review task start`、`review run` 的 JSON 输入和请求 Schema。
- 回归影响：一键工作流的配置校验、发起请求、任务轮询前的输入校验。
- 不影响：上传、主体提取、任务 status/info/result 的公开参数；远端接口和部署配置。

## 验证

- `go test ./...`
- `go vet ./...`
- `go test -race ./internal/cli ./internal/review ./internal/contracts`
- `git diff --check`
- `make build`
- `go run ./cmd/everyline-cli review task start -h`
- `go run ./cmd/everyline-cli review run -h`
