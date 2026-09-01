# 命令参考

公共参数：

```text
--profile <name>
--as app|user
--output json|yaml|table|raw
--raw                         # 等价于 --output raw
--timeout 30s
--verbose
--no-color
```

参数优先级：`--profile` 覆盖当前 Profile，仅对本次命令生效；身份选择优先级为 `--as` > Profile 默认身份 > `app`；输出格式优先级为 `--output` > Profile 默认输出 > `json`。用户显式配置的 `table/yaml/raw` 不会被覆盖。

## Profile 与鉴权

```text
config add <name> [--env dev|test|blue|prod] [--app-id ID] [--base-url URL --user-base-url URL --auth-url URL --token-url URL] [--oauth-metadata-url URL --oauth-business-type TYPE --oauth-client-id ID --oauth-redirect-url URL --oauth-scope SCOPE] [--default-identity app|user] [--default-output]
config list
config use <name>
config show [name]

auth login [--as app|user] [--app-id ID] [--app-secret SECRET|--app-secret-stdin] [--save-app-secret] [--no-open-browser]
auth status [--as app|user]
auth use [profile] --as app|user
auth logout [--as app|user]
```

`--env` 预设业务地址：`dev=https://dev-open.qtech.cn`、`test=https://test-open.qtech.cn`、`blue=https://blue-open.qtech.cn`、`prod=https://open.qfei.cn`；对应的自有用户认证页面分别为 `https://dev-contract-agent.qtech.cn`、`https://test-contract-agent.qtech.cn`、`https://blue-contract-agent.qtech.cn`、`https://contract-agent.qfei.cn`。dev/test 预设同时包含 `contract-review` OAuth metadata、public client 和 loopback 回调配置。使用 `--env` 时不需要再传 `--base-url`/`--token-url`；不使用 `--env` 时仍需同时传入这两个地址，可额外传 `--auth-url` 与 `--oauth-*` 参数。`app` 身份必须通过 `--app-id`、`EVERYLINE_APP_ID` 或 Profile 提供 app-id，并通过 `--app-secret`、`--app-secret-stdin`、环境变量或当前 Profile 的本地安全存储提供 app secret；macOS 优先使用 Keychain，失败时回退 `secrets.json`，其他系统使用该文件。只有显式 `--save-app-secret` 才会保存 secret。`user` 身份不需要 app-id，始终走 OAuth 浏览器登录，`--no-open-browser` 只打印授权链接。例如：`config add dev-user --env dev --default-identity user`。

`config use <name>` 会切换当前默认 Profile；`auth use [profile] --as app|user` 会修改指定 Profile 的默认身份。只对当前命令临时指定 Profile 或身份时，使用 `--profile` 或 `--as`。

`auth status` 对服务端提供过期时间的 token 输出 `expiresAt` 和 `expiresInSeconds`；对 app 环境变量交接的未知过期 token 输出 `expiresKnown=false`，不输出虚假时间。该命令默认读取本地缓存；业务请求收到服务端 `code=110004` 时，CLI 会删除当前 Profile 的 user token、返回鉴权退出码 3 并提示重新执行 `auth login --as user`，后续 status 将报告 `authenticated=false`。当前服务端 OAuth token endpoint 只支持 `authorization_code`，缓存的 `refresh_token` 不会被 CLI 自动使用。

app 身份的 status 只输出 `appSecretConfigured` 布尔值，不输出 secret 内容；`auth logout` 只删除 token 缓存，不删除已显式保存的 app secret。

## 审查

命令选择：

- 使用 `review run` 执行上传、发起、等待的一键审查流程。
- 使用 `review file` 只准备审查输入文件，不自动发起任务。
- 使用 `review subject` 提取已上传合同的主体信息。
- 使用 `review task` 分步发起、查询或获取审查任务结果。
- 使用 `checklist` 管理审查清单。
- 使用 `rule` 管理规则分组和审查规则。

```text
review file upload --file --name [--dry-run|--print-input]
review file upload-url --file-url --name [--dry-run|--print-input]
review subject extract --business-id --file-id [--file-hash] [--dry-run|--print-input]

review task start (--input file.json | --data '{...}') [--dry-run|--print-input]
review task status --task-id [--business-id] [--visibility-scope contractResult]
review task info --task-id [--business-id] [--visibility-scope contractResult]
review task result --task-id [--business-id] [--visibility-scope contractResult] [--interval 2s] [--deadline 10m]

review run (--input file.json | --data '{...}') [--interval 2s] [--deadline 10m]

checklist create (--input file.json | --data '{...}') [--dry-run|--print-input]
checklist batch-create (--input file.json | --data '[...]') [--dry-run|--print-input]
checklist list [--name] [--review-stage] [--contract-category] [--start-time] [--end-time] [--enabled] [--create-employee-id] [--update-employee-id] [--sort] [--page-index] [--page-size]
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
version [--manifest-url HTTPS_URL]
update [--manifest-url HTTPS_URL] [--dry-run]
```

所有写操作的 `--dry-run` 和 `--print-input` 都只校验并输出规范化请求，不调用远端；两者当前行为一致。`review task result` 是 CLI 本地编排，不对应新的远端接口；它会轮询任务状态，默认把每次 `status` 输出到 stderr，成功后自动获取最终详情；后端提供顶层 `url` 时补充规范化的 `reviewDetailUrl`。飞书用户 OAuth 响应没有预览链接时仍返回完整成功详情。

`review run` 使用 URL 来源时，上传接口只返回 `fileId`；输入还需提供 `businessId` 和上传接口返回的 `fileHash`，工作流才会继续发起审查。需要完整闭环时在输入中显式设置 `"wait": true`；省略或设置为 `false` 时命令在发起任务后返回。

`review task start` 和 `review run` 的发起审查输入只保留必填审查字段：`businessId`、`fileId`、`fileHash`、`config.selectedPosition`、`config.selectedAuditRole`、`config.reviewStrength`；`selectedPosition` 填写合同主体精确名称，`selectedAuditRole` 填写同一主体在主体提取响应中的角色，例如“甲方”或“乙方”。审查强度推荐“弱势/中立/强势”，并兼容旧版 `0/1/2`；CLI 统一转换为后端 `0/1/2`。规则来源要求非空 `selectedCheckListIds` 或 `matchContractTypeRulePackage=true` 至少一项；同时提供时组合执行，没有覆盖或优先级。实际调用 `startReview` 时固定补入 `usageReportContext.reportBusinessCode=everyLine_100_openApi_cli`，调用方无需提供也不能覆盖。`fileHash` 使用上传接口返回值；`fileId` 本次保持当前 CLI 输入类型，不进行全局字符串化。未列出的字段会被严格 JSON 输入拒绝。`review file upload` 也不接受 `--business-id`。主体提取仍必须提供 `--business-id`；任务 status/info/result 默认只需 `--task-id`，使用 `--visibility-scope contractResult` 时仍必须提供 `--business-id`，不再要求 appType。

发起审查示例：

```bash
everyline-cli review task start --data '{"businessId":"biz-001","fileId":123,"fileHash":"<upload.fileHash>","config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":"中立","matchContractTypeRulePackage":true}}' --dry-run
everyline-cli review run --data '{"source":{"type":"file","path":"./contract.pdf","name":"合同.pdf"},"config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":"中立","matchContractTypeRulePackage":true},"wait":true}' --dry-run
```

`checklist batch-create`、`checklist batch-update`、`rule batch-create` 和 `rule batch-update` 均支持一次提交数组并执行真实 HTTP 调用。批量更新数组中的每项必须包含 `id`；`--dry-run`/`--print-input` 仍只做本地校验和请求预览。

`version` 返回当前构建信息及 `latestVersion/isLatest/updateCommand`；检查失败时 `isLatest=null` 并携带 `checkError`，退出码仍为 0。普通业务命令发现新版本时只在 stderr 提示，检查失败不阻断请求。更新地址按命令参数、`EVERYLINE_CLI_UPDATE_MANIFEST_URL`、发布构建内置值选择。

`update` 只支持独立二进制。命令从解析出的 HTTPS manifest 选择当前平台制品，比较 SemVer，下载后校验 SHA-256，再替换当前二进制。同步替换完成时输出 `updated=true, scheduled=false`；Windows 需要等待当前进程退出时输出 `updated=false, scheduled=true`，独立 helper 的最终成功或失败写入 stderr。`--dry-run` 只做 manifest、平台和版本校验。通过 npm/npx 薄包装启动时不会修改包内二进制，应使用 npm 更新包。

## 退出码

| 退出码 | 含义 |
|---:|---|
| 0 | 成功 |
| 2 | 参数或输入校验失败 |
| 3 | Profile 或鉴权失败 |
| 4 | 远端 API 或客户端契约/能力错误 |
| 5 | 网络、取消、超时或轮询截止 |
