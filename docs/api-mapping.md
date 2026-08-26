# API 映射

基础 URL 来自 Profile。下表路径均为 CLI HTTP Adapter 中的相对路径。

四个批量创建/更新操作属于非必需能力：`batchCreateReviewChecklists`、`batchUpdateReviewChecklists`、`batchCreateReviewRules`、`batchUpdateReviewRules`。对应命令支持 `--dry-run` 做本地校验，真实调用在 HTTP 请求前显式拒绝；单项写入和批量删除可正常使用。

| Operation ID | CLI 命令 | 方法与路径 |
|---|---|---|
| `tenantAccessTokenInternal` | `auth login` | `POST profile.token_url` |
| `uploadContractFileV3` | `review file upload` | `POST /open-apis/contract-review/v3/file/contract/upload` |
| `uploadContractFileByURLV3` | `review file upload-url` | `POST /open-apis/contract-review/v1/file/contract/uploadByUrl` |
| `smartAuditContractSubjects` | `review subject extract` | `POST /open-apis/contract-review/v3/smartAudit/contract/subjects` |
| `smartAuditTaskStartReview` | `review task start` | `POST /open-apis/contract-review/v3/smartAudit/task/startReview` |
| `smartAuditTaskStatus` | `review task status` | `GET /open-apis/contract-review/v3/smartAudit/task/status` |
| `smartAuditTaskInfo` | `review task info` | `GET /open-apis/contract-review/v3/smartAudit/task/info` |
| `createReviewChecklist` | `checklist create` | `POST /open-apis/review-rules/review-checklists` |
| `batchCreateReviewChecklists` | `checklist batch-create` | `POST /open-apis/review-rules/review-checklists/batch` |
| `listReviewChecklists` | `checklist list` | `GET /open-apis/review-rules/review-checklists` |
| `updateReviewChecklist` | `checklist update` | `PUT /open-apis/review-rules/review-checklists/{id}` |
| `batchUpdateReviewChecklists` | `checklist batch-update` | `PUT /open-apis/review-rules/review-checklists/batch` |
| `deleteReviewChecklist` | `checklist delete` | `DELETE /open-apis/review-rules/review-checklists/{id}` |
| `batchDeleteReviewChecklists` | `checklist batch-delete` | `DELETE /open-apis/review-rules/review-checklists/batch` |
| `createReviewRuleGroup` | `rule group create` | `POST /open-apis/review-rules/review-rule-groups` |
| `listReviewRuleGroups` | `rule group list` | `GET /open-apis/review-rules/review-rule-groups` |
| `updateReviewRuleGroup` | `rule group update` | `PUT /open-apis/review-rules/review-rule-groups/{id}` |
| `deleteReviewRuleGroup` | `rule group delete` | `DELETE /open-apis/review-rules/review-rule-groups/{id}` |
| `createReviewRule` | `rule create` | `POST /open-apis/review-rules/review-rule-groups/{groupId}/rules` |
| `batchCreateReviewRules` | `rule batch-create` | `POST /open-apis/review-rules/review-rule-groups/{groupId}/rules/batch` |
| `listReviewRules` | `rule list` | `GET /open-apis/review-rules/review-rule-groups/{groupId}/rules` |
| `updateReviewRule` | `rule update` | `PUT /open-apis/review-rules/review-rule-groups/{groupId}/rules/{ruleId}` |
| `batchUpdateReviewRules` | `rule batch-update` | `PUT /open-apis/review-rules/review-rule-groups/{groupId}/rules/batch` |
| `deleteReviewRule` | `rule delete` | `DELETE /open-apis/review-rules/review-rule-groups/{groupId}/rules/{ruleId}` |
| `batchDeleteReviewRules` | `rule batch-delete` | `DELETE /open-apis/review-rules/review-rule-groups/{groupId}/rules/batch` |

普通业务接口以 `code=200` 为成功；token 接口以 `code=0` 为成功，两者不会共用固定成功码。

`review task result` 复用 `smartAuditTaskStatus` 做 CLI 侧轮询，并在成功后调用 `smartAuditTaskInfo` 获取最终详情，不单独计为远端 operation。文件快照接口属于 V3 文档能力，但按当前范围不纳入 CLI 清单，也不参与一键工作流。

批量删除命令对外仍接受重复 `--id` 或 JSON ID 数组，但 HTTP 请求体统一包装为 `{"ids":[...]}`，以匹配平台批量删除接口契约。

任务 `status/info/result` 查询默认只要求 `taskId`；使用 `visibilityScope=contractResult` 时，当前服务端兼容契约只要求 `businessId`，不再要求 `appType`。CLI 已移除 `--app-type`，查询请求不会自动补入 appType。

`review task start` 与 `review run` 的调用方输入只保留 `businessId`、`fileId`、`fileHash` 和 `config`；`config` 必须包含 `selectedPosition`、`selectedAuditRole`、`reviewStrength`，其中 CLI 强度取值为“弱势/中立/强势”，HTTP 边界转换为 `0/1/2`。非空 `selectedCheckListIds` 或 `matchContractTypeRulePackage=true` 至少满足一项，两项同时提供时组合执行。在调用 `smartAuditTaskStartReview` 的 HTTP 边界，CLI 固定补入 `usageReportContext.reportBusinessCode=everyLine_100_openApi_cli`，该字段不接受输入或 Profile 覆盖。`fileHash` 使用上传接口返回的文件指纹；CLI 不接收文档中其他非必填字段，未知字段会在 JSON 解码阶段拒绝。`fileId` 本次仍保持 CLI 当前输入和内部类型行为，不纳入字符串化改造。`review subject extract` 的 `fileHash` 仍按文档作为可选链路追踪字段透传。

`review run` 的数据流为：上传 -> 可选主体提取 -> 发起 -> 可选轮询 -> 详情。文件上传响应提供 `businessId/fileId/fileHash`；标准 URL 上传接口只返回文件 ID，因此 URL 来源需要在 RunSpec 中额外提供 `businessId` 和上传接口返回的 `fileHash`。

清单创建与更新请求按公开接口说明要求 `name`、`reviewRuleIds`；`contractCategory`、`reviewStage` 为可选兼容字段。

规则分组删除的级联语义由接口固定，CLI 的 dry-run 只展示路径中的 `id`，不会虚构 `cascade` 请求参数。
