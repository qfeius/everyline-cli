# 命令参考

公共参数：

```text
--profile <name>
--output json|yaml|table|raw
--raw
--timeout 30s
--verbose
--no-color
```

## Profile 与鉴权

```text
config add <name> --base-url --token-url --app-id [--default-output]
config list
config use <name>
config show [name]

auth login [--app-secret-stdin]
auth status
auth logout
```

## 审查

```text
review file upload --file --name [--app-type] [--business-id] [--dry-run|--print-input]
review file upload-url --file-url --name [--dry-run|--print-input]
review file snapshot --file-id [--app-type] [--business-id]

review subject extract --business-id --app-type --file-id --file-hash [--dry-run|--print-input]

review task start (--input file.json | --data '{...}') [--dry-run|--print-input]
review task start-feishu (--input file.json | --data '{...}') [--dry-run|--print-input]
review task status --task-id [--business-id] [--app-type]
review task info --task-id [--business-id] [--app-type]
review task wait --task-id [--business-id] [--app-type] [--interval 2s] [--deadline 10m]

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

version
```

所有写操作的 `--dry-run` 和 `--print-input` 都只校验并输出规范化请求，不调用远端。`review task wait` 是 CLI 本地编排，不对应新的远端接口。

## 退出码

| 退出码 | 含义 |
|---:|---|
| 0 | 成功 |
| 2 | 参数或输入校验失败 |
| 3 | Profile 或鉴权失败 |
| 4 | 远端 API 业务错误 |
| 5 | 网络、取消、超时或轮询截止 |
