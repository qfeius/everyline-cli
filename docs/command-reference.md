# 命令参考

公共参数：

```text
--profile <name>
--as app|user
--output json|yaml|table|raw
--raw
--timeout 30s
--verbose
--no-color
```

## Profile 与鉴权

```text
config add <name> [--env dev|test|blue|prod] --app-id [--base-url URL --user-base-url URL --auth-url URL --token-url URL] [--default-identity app|user] [--default-output]
config list
config use <name>
config show [name]

auth login [--as app|user] [--app-secret-stdin|--access-token-stdin]
auth status [--as app|user]
auth use [profile] --as app|user
auth logout [--as app|user]
```

`--env` 预设业务地址：`dev=https://dev-open.qtech.cn`、`test=https://test-open.qtech.cn`、`blue=https://blue-open.qtech.cn`、`prod=https://open.qfei.cn`；对应的自有用户认证页面分别为 `https://dev-contract-agent.qtech.cn`、`https://test-contract-agent.qtech.cn`、`https://blue-contract-agent.qtech.cn`、`https://contract-agent.qfei.cn`。使用 `--env` 时不需要再传 `--base-url`/`--token-url`；不使用 `--env` 时仍需同时传入这两个地址，可额外传 `--auth-url`。每个环境仍需提供对应的 `--app-id`。`app` 身份通过 `auth login --app-secret-stdin` 或环境变量提供，`user` 身份通过自有页面完成认证后使用 `auth login --access-token-stdin` 或 `EVERYLINE_USER_ACCESS_TOKEN` 提供。

## 审查

```text
review file upload --file --name [--app-type] [--business-id] [--dry-run|--print-input]
review file upload-url --file-url --name [--dry-run|--print-input]
review subject extract --business-id --app-type --file-id [--file-hash] [--dry-run|--print-input]

review task start (--input file.json | --data '{...}') [--dry-run|--print-input]
review task status --task-id [--business-id] [--app-type] [--visibility-scope contractResult]
review task info --task-id [--business-id] [--app-type] [--visibility-scope contractResult]
review task wait --task-id [--business-id] [--app-type] [--visibility-scope contractResult] [--interval 2s] [--deadline 10m]

review run (--input file.json | --data '{...}') [--interval 2s] [--deadline 10m]

checklist create (--input file.json | --data '{...}') [--dry-run|--print-input]
checklist batch-create (--input file.json | --data '[...]') [--dry-run|--print-input]
checklist list [--name] [--review-stage] [--contract-category] [--enabled] [--page-index] [--page-size]
checklist update --id ID (--input file.json | --data '{...}') [--dry-run|--print-input]
checklist batch-update (--input file.json | --data '[...]') [--dry-run|--print-input]
checklist delete --id ID --yes [--dry-run|--print-input]
checklist batch-delete (--id ID... | --input ids.json | --data '[...]') --yes [--dry-run|--print-input]

rule group create (--input file.json | --data '{...}') [--dry-run|--print-input]
rule group list [--sort] [--page-index] [--page-size]
rule group update --id ID (--input file.json | --data '{...}') [--dry-run|--print-input]
rule group delete --id ID --yes [--dry-run|--print-input]

rule create --group-id ID (--input file.json | --data '{...}') [--dry-run|--print-input]
rule batch-create --group-id ID (--input file.json | --data '[...]') [--dry-run|--print-input]
rule list --group-id ID [--sort] [--page-index] [--page-size]
rule update --group-id ID --rule-id ID (--input file.json | --data '{...}') [--dry-run|--print-input]
rule batch-update --group-id ID (--input file.json | --data '[...]') [--dry-run|--print-input]
rule delete --group-id ID --rule-id ID --yes [--dry-run|--print-input]
rule batch-delete --group-id ID (--id ID... | --input ids.json | --data '[...]') --yes [--dry-run|--print-input]

completion bash|fish|powershell|zsh
version
```

所有写操作的 `--dry-run` 和 `--print-input` 都只校验并输出规范化请求，不调用远端。`review task wait` 是 CLI 本地编排，不对应新的远端接口。

`review run` 使用 URL 来源时，上传接口只返回 `fileId`；输入还需提供 `businessId` 和对应文件内容的 SHA-256 `fileHash`，工作流才会继续发起审查。需要完整闭环时在输入中显式设置 `"wait": true`；省略或设置为 `false` 时命令在发起任务后返回。

`appType=CLM` 的本地文件上传必须提供 `businessId`；`--dry-run` 同样会执行此校验。使用 `--visibility-scope contractResult` 查询任务时，必须同时提供 `--business-id` 和 `--app-type`。

`checklist batch-create`、`checklist batch-update`、`rule batch-create`、`rule batch-update` 的字段级详情页尚未发布，因此只允许 `--dry-run`/`--print-input`；真实调用会在发送 HTTP 请求前失败关闭。`update` 自更新命令属于技术方案 M5，需待制品下载地址和签名校验机制冻结后实现。

## 退出码

| 退出码 | 含义 |
|---:|---|
| 0 | 成功 |
| 2 | 参数或输入校验失败 |
| 3 | Profile 或鉴权失败 |
| 4 | 远端 API 或客户端契约/能力错误 |
| 5 | 网络、取消、超时或轮询截止 |
