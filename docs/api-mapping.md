# API 映射

基础 URL 来自 Profile。下表路径均为 CLI HTTP Adapter 中的相对路径。

当前接口清单尚未给出四个批量写操作的字段级请求体：`batchCreateReviewChecklists`、`batchUpdateReviewChecklists`、`batchCreateReviewRules`、`batchUpdateReviewRules`。对应命令支持 `--dry-run` 做本地校验，但真实调用会显式拒绝；补齐详情页并完成契约测试后方可解除保护。

| Operation ID | CLI 命令 | 方法与路径 |
|---|---|---|
| `tenantAccessTokenInternal` | `auth login` | `POST profile.token_url` |
| `uploadContractFileV3` | `review file upload` | `POST /open-apis/contract-review/v3/file/contract/upload` |
| `uploadContractFileByURLV3` | `review file upload-url` | `POST /open-apis/contract-review/v1/file/contract/uploadByUrl` |
| `smartAuditContractSubjects` | `review subject extract` | `POST /open-apis/contract-review/v3/smartAudit/contract/subjects` |
| `smartAuditTaskStartReview` | `review task start` | `POST /open-apis/contract-review/v3/smartAudit/task/startReview` |
| `feishuSmartAuditTaskStartReviewV3` | `review task start-feishu` | `POST /open-apis/contract-review/feishu/v1/smartAudit/init` |
| `smartAuditTaskStatus` | `review task status` | `GET /open-apis/contract-review/v3/smartAudit/task/status` |
| `smartAuditTaskInfo` | `review task info` | `GET /open-apis/contract-review/v3/smartAudit/task/info` |
| `feishuSmartAuditTaskInfo` | `review task info-feishu` | `GET /open-apis/contract-review/v1/smartAudit/info` |
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

`review task wait` 复用 `smartAuditTaskStatus` 做 CLI 侧轮询，不单独计为远端 operation。文件快照接口属于 V3 文档能力，但按当前范围不纳入 CLI 清单，也不参与一键工作流。

批量删除命令对外仍接受重复 `--id` 或 JSON ID 数组，但 HTTP 请求体统一包装为 `{"ids":[...]}`，以匹配平台批量删除接口契约。

任务 `status/info/wait` 查询默认只要求 `taskId`；使用 `visibilityScope=contractResult` 时，必须同时提供 `businessId` 与 `appType`，该范围当前只接受 `contractResult`。

`review run` 的数据流为：上传 -> 可选主体提取 -> 发起 -> 可选轮询 -> 详情。文件上传响应优先提供 `businessId/fileId/fileHash`；标准 URL 上传接口只返回文件对象，因此 URL 来源需要在 RunSpec 中额外提供 `businessId` 和文件内容 SHA-256 的 `fileHash`。

字段捷径入口的后端源路由为 `/open-api/feishu/v1/smartAudit/init`；开放平台网关对 `/open-apis/contract-review/(?<remaining>.*)` 执行 rewrite 到 `/open-api/${remaining}`，因此 CLI 的外部路径固定为 `/open-apis/contract-review/feishu/v1/smartAudit/init`。该映射与 `open-platform/src/main/java/com/bytedance/qfei/openplatform/config/GatewayFilterConfiguration.java` 的 `openapi-contract-review` 路由一致。

字段捷径入口返回 `smartAuditId` 和数值 `taskStatus`，不复用 V3 的 `taskId/status` 查询。`review task info-feishu` 和 `review task wait-feishu` 改用 `/open-apis/contract-review/v1/smartAudit/info`，将 `taskStatus=0/1/2/3/4` 解释为 running/success/fail/skipped/prepare，并保留后端原始详情。

清单创建与更新请求按公开接口说明要求 `name`、`reviewRuleIds`；`contractCategory`、`reviewStage` 为可选兼容字段。
