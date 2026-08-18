# API 映射

基础 URL 来自 Profile。下表路径均为 CLI HTTP Adapter 中的相对路径。

| Operation ID | CLI 命令 | 方法与路径 |
|---|---|---|
| `tenantAccessTokenInternal` | `auth login` | `POST profile.token_url` |
| `uploadContractFileV3` | `review file upload` | `POST /open-apis/contract-review/v3/file/contract/upload` |
| `uploadContractFileByURLV3` | `review file upload-url` | `POST /open-apis/contract-review/v3/file/contract/uploadByUrl` |
| `smartAuditFileSnapshot` | `review file snapshot` | `GET /open-apis/contract-review/v3/smartAudit/file/snapshot` |
| `smartAuditContractSubjects` | `review subject extract` | `POST /open-apis/contract-review/v3/smartAudit/contract/subjects` |
| `smartAuditTaskStartReview` | `review task start` | `POST /open-apis/contract-review/v3/smartAudit/task/startReview` |
| `feishuSmartAuditTaskStartReviewV3` | `review task start-feishu` | `POST /open-apis/contract-review/feishu/v3/smartAudit/task/startReview` |
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

`review task wait` 复用 `smartAuditTaskStatus` 做 CLI 侧轮询，不单独计为远端 operation。

`review run` 的数据流为：上传 -> 必要时读取快照 -> 可选主体提取 -> 发起 -> 可选轮询 -> 详情。后续接口使用上传响应中的 `businessId/fileId/fileHash`，不自行生成业务 ID 或文件指纹。
