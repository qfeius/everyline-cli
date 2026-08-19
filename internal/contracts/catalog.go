package contracts

// Spec 把一个远端 operation 与 CLI、HTTP 和 JSON Schema 契约绑定在一起。
type Spec struct {
	OperationID string
	Command     string
	Method      string
	Path        string
	Schema      string
}

var catalog = []Spec{
	{"tenantAccessTokenInternal", "auth login", "POST", "profile.token_url", "auth-login.schema.json"},
	{"uploadContractFileV3", "review file upload", "POST", "/open-apis/contract-review/v3/file/contract/upload", "review-upload.schema.json"},
	{"uploadContractFileByURLV3", "review file upload-url", "POST", "/open-apis/contract-review/v1/file/contract/uploadByUrl", "review-upload-url.schema.json"},
	{"smartAuditContractSubjects", "review subject extract", "POST", "/open-apis/contract-review/v3/smartAudit/contract/subjects", "review-subject.schema.json"},
	{"smartAuditTaskStartReview", "review task start", "POST", "/open-apis/contract-review/v3/smartAudit/task/startReview", "review-start.schema.json"},
	{"feishuSmartAuditTaskStartReviewV3", "review task start-feishu", "POST", "/open-apis/contract-review/feishu/v1/smartAudit/init", "review-start-feishu.schema.json"},
	{"smartAuditTaskStatus", "review task status", "GET", "/open-apis/contract-review/v3/smartAudit/task/status", "review-task-query.schema.json"},
	{"smartAuditTaskInfo", "review task info", "GET", "/open-apis/contract-review/v3/smartAudit/task/info", "review-task-query.schema.json"},
	{"feishuSmartAuditTaskInfo", "review task info-feishu", "GET", "/open-apis/contract-review/v1/smartAudit/info", "review-feishu-task-query.schema.json"},

	{"createReviewChecklist", "checklist create", "POST", "/open-apis/review-rules/review-checklists", "checklist.schema.json"},
	{"batchCreateReviewChecklists", "checklist batch-create", "POST", "/open-apis/review-rules/review-checklists/batch", "checklist-batch-create.schema.json"},
	{"listReviewChecklists", "checklist list", "GET", "/open-apis/review-rules/review-checklists", "checklist-query.schema.json"},
	{"updateReviewChecklist", "checklist update", "PUT", "/open-apis/review-rules/review-checklists/{id}", "checklist.schema.json"},
	{"batchUpdateReviewChecklists", "checklist batch-update", "PUT", "/open-apis/review-rules/review-checklists/batch", "checklist-batch-update.schema.json"},
	{"deleteReviewChecklist", "checklist delete", "DELETE", "/open-apis/review-rules/review-checklists/{id}", "resource-id.schema.json"},
	{"batchDeleteReviewChecklists", "checklist batch-delete", "DELETE", "/open-apis/review-rules/review-checklists/batch", "resource-ids.schema.json"},

	{"createReviewRuleGroup", "rule group create", "POST", "/open-apis/review-rules/review-rule-groups", "rule-group.schema.json"},
	{"listReviewRuleGroups", "rule group list", "GET", "/open-apis/review-rules/review-rule-groups", "rule-query.schema.json"},
	{"updateReviewRuleGroup", "rule group update", "PUT", "/open-apis/review-rules/review-rule-groups/{id}", "rule-group.schema.json"},
	{"deleteReviewRuleGroup", "rule group delete", "DELETE", "/open-apis/review-rules/review-rule-groups/{id}", "resource-id.schema.json"},

	{"createReviewRule", "rule create", "POST", "/open-apis/review-rules/review-rule-groups/{groupId}/rules", "rule.schema.json"},
	{"batchCreateReviewRules", "rule batch-create", "POST", "/open-apis/review-rules/review-rule-groups/{groupId}/rules/batch", "rule-batch-create.schema.json"},
	{"listReviewRules", "rule list", "GET", "/open-apis/review-rules/review-rule-groups/{groupId}/rules", "rule-query.schema.json"},
	{"updateReviewRule", "rule update", "PUT", "/open-apis/review-rules/review-rule-groups/{groupId}/rules/{ruleId}", "rule.schema.json"},
	{"batchUpdateReviewRules", "rule batch-update", "PUT", "/open-apis/review-rules/review-rule-groups/{groupId}/rules/batch", "rule-batch-update.schema.json"},
	{"deleteReviewRule", "rule delete", "DELETE", "/open-apis/review-rules/review-rule-groups/{groupId}/rules/{ruleId}", "resource-id.schema.json"},
	{"batchDeleteReviewRules", "rule batch-delete", "DELETE", "/open-apis/review-rules/review-rule-groups/{groupId}/rules/batch", "resource-ids.schema.json"},
}

// All 返回当前 CLI 同步的 27 项契约副本，调用方不能修改包内清单。
// 入参：无。
// 返回值：[]Spec，按技术方案接口序号稳定排序。
func All() []Spec {
	return append([]Spec(nil), catalog...)
}
