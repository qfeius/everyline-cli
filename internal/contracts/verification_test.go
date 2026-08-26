package contracts

import (
	"errors"
	"strings"
	"testing"
)

// TestVerificationStatus 验证仅四个缺失请求体详情的批量写操作被显式保护。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestVerificationStatus(t *testing.T) {
	unverified := map[string]bool{
		"batchCreateReviewChecklists": true,
		"batchUpdateReviewChecklists": true,
		"batchCreateReviewRules":      true,
		"batchUpdateReviewRules":      true,
	}
	for _, spec := range All() {
		if IsVerified(spec.OperationID) == unverified[spec.OperationID] {
			t.Errorf("operation=%s verified=%t", spec.OperationID, IsVerified(spec.OperationID))
		}
	}
	if IsVerified("unknownOperation") {
		t.Fatal("未知 operation 不得被视为已核验")
	}
	if err := RequireVerified("unknownOperation"); !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("err=%v，期望未知 operation 失败关闭", err)
	}
}

// TestCatalogValidatesRuntimeRequests 验证目录可校验固定路径和路径参数，并拒绝方法漂移。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestCatalogValidatesRuntimeRequests(t *testing.T) {
	for _, spec := range All() {
		path := strings.NewReplacer("{id}", "id-1", "{groupId}", "group-1", "{ruleId}", "rule-1").Replace(spec.Path)
		if err := ValidateRequest(spec.OperationID, spec.Method, path, validContractInput(spec.Schema)); err != nil {
			t.Errorf("operation=%s err=%v", spec.OperationID, err)
		}
	}
	if err := ValidateRequest("listReviewChecklists", "POST", "/open-apis/review-rules/review-checklists", map[string]any{}); !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("err=%v，期望 ErrContractMismatch", err)
	}
}

// validContractInput 为目录中的每类 Schema 提供最小合法输入，确保目录项都执行真实 Schema 编译与校验。
// 入参：schema string 为 catalog 绑定的 Schema 文件名。
// 返回值：any，为可通过对应 Schema 的逻辑请求输入；未知 Schema 返回 nil 以使测试失败关闭。
func validContractInput(schema string) any {
	checklist := map[string]any{"name": "清单", "reviewRuleIds": []string{"rule-1"}}
	rule := map[string]any{"name": "规则", "riskLevel": 1, "content": "内容"}
	switch schema {
	case "auth-login.schema.json":
		return map[string]any{"appId": "app", "appSecret": "secret"}
	case "review-upload.schema.json":
		return map[string]any{"file": "contract.pdf", "name": "contract.pdf", "appType": "THIRD_PARTY"}
	case "review-upload-url.schema.json":
		return map[string]any{"fileUrl": "https://open.qfei.cn/contract.pdf", "fileName": "contract.pdf"}
	case "review-subject.schema.json":
		return map[string]any{"businessId": "biz", "appType": "THIRD_PARTY", "fileId": "1"}
	case "review-start.schema.json":
		return map[string]any{"businessId": "biz", "fileId": "1", "fileHash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "config": map[string]any{"selectedPosition": "甲方", "selectedAuditRole": "甲方", "reviewStrength": 1, "matchContractTypeRulePackage": true}, "usageReportContext": map[string]any{"reportBusinessCode": "everyLine_100_openApi_cli"}}
	case "review-task-query.schema.json":
		return map[string]any{"taskId": 1, "businessId": "biz"}
	case "checklist.schema.json":
		return checklist
	case "checklist-batch-create.schema.json":
		return []any{checklist}
	case "checklist-batch-update.schema.json":
		checklist["id"] = "check-1"
		return []any{checklist}
	case "checklist-query.schema.json", "rule-query.schema.json":
		return map[string]any{}
	case "resource-id.schema.json":
		return map[string]any{"id": "id-1"}
	case "resource-ids.schema.json":
		return map[string]any{"ids": []string{"id-1"}}
	case "rule-group.schema.json":
		return map[string]any{"name": "分组"}
	case "rule.schema.json":
		return rule
	case "rule-batch-create.schema.json":
		return []any{rule}
	case "rule-batch-update.schema.json":
		rule["id"] = "rule-1"
		return []any{rule}
	default:
		return nil
	}
}

// TestRequestSchemaRejectsDrift 验证运行时契约层拒绝 Schema 声明为唯一的重复资源 ID。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestRequestSchemaRejectsDrift(t *testing.T) {
	err := ValidateRequest("batchDeleteReviewChecklists", "DELETE", "/open-apis/review-rules/review-checklists/batch", map[string]any{"ids": []string{"same", "same"}})
	if !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("err=%v，期望 ErrContractMismatch", err)
	}
}

// TestUploadSchemaAllowsCLMWithoutBusinessID 验证 appType 非必填时上传契约不追加业务对象校验。
func TestUploadSchemaAllowsCLMWithoutBusinessID(t *testing.T) {
	if err := ValidateRequest(
		"uploadContractFileV3",
		"POST",
		"/open-apis/contract-review/v3/file/contract/upload",
		map[string]any{"file": "contract.pdf", "name": "contract.pdf", "appType": "CLM"},
	); err != nil {
		t.Fatalf("err=%v，CLM 缺少 businessId 仍应通过契约校验", err)
	}
}

// TestTaskQuerySchemaAllowsOptionalContext 验证任务查询只依赖 taskId，符合 status/info 后端参数定义。
func TestTaskQuerySchemaAllowsOptionalContext(t *testing.T) {
	if err := ValidateRequest("smartAuditTaskStatus", "GET", "/open-apis/contract-review/v3/smartAudit/task/status", map[string]any{"taskId": 1}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequest("smartAuditTaskStatus", "GET", "/open-apis/contract-review/v3/smartAudit/task/status", map[string]any{"taskId": 1, "visibilityScope": "all"}); !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("err=%v，期望非法 visibilityScope 被拒绝", err)
	}
	if err := ValidateRequest("smartAuditTaskStatus", "GET", "/open-apis/contract-review/v3/smartAudit/task/status", map[string]any{"taskId": 1, "visibilityScope": "contractResult"}); !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("err=%v，contractResult 缺少业务上下文时应被拒绝", err)
	}
}

// TestStartSchemaRequiresReviewConfig 验证发起接口要求 config 及其三个必填字段。
func TestStartSchemaRequiresReviewConfig(t *testing.T) {
	input := map[string]any{
		"businessId":         "biz",
		"fileId":             "1",
		"fileHash":           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"usageReportContext": map[string]any{"reportBusinessCode": "everyLine_100_openApi_cli"},
		"config": map[string]any{
			"selectedPosition":             "甲方",
			"selectedAuditRole":            "甲方",
			"reviewStrength":               1,
			"matchContractTypeRulePackage": true,
		},
	}
	if err := ValidateRequest("smartAuditTaskStartReview", "POST", "/open-apis/contract-review/v3/smartAudit/task/startReview", input); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"selectedPosition", "selectedAuditRole", "reviewStrength"} {
		incomplete := map[string]any{
			"businessId":         "biz",
			"fileId":             "1",
			"fileHash":           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"config":             map[string]any{},
			"usageReportContext": map[string]any{"reportBusinessCode": "everyLine_100_openApi_cli"},
		}
		incomplete["config"].(map[string]any)[missing] = nil
		if err := ValidateRequest("smartAuditTaskStartReview", "POST", "/open-apis/contract-review/v3/smartAudit/task/startReview", incomplete); !errors.Is(err, ErrContractMismatch) {
			t.Fatalf("missing=%s err=%v，缺少 config 必填字段时应失败", missing, err)
		}
	}
}

// TestStartSchemaRejectsExcludedFields 验证 startReview CLI 契约不接受文档中的非必填字段。
func TestStartSchemaRejectsExcludedFields(t *testing.T) {
	input := map[string]any{
		"businessId":         "biz",
		"fileId":             "1",
		"fileHash":           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"usageReportContext": map[string]any{"reportBusinessCode": "everyLine_100_openApi_cli"},
		"config": map[string]any{
			"selectedPosition":             "甲方",
			"selectedAuditRole":            "甲方",
			"reviewStrength":               1,
			"matchContractTypeRulePackage": true,
		},
		"appType": "THIRD_PARTY",
	}
	if err := ValidateRequest("smartAuditTaskStartReview", "POST", "/open-apis/contract-review/v3/smartAudit/task/startReview", input); !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("err=%v，appType 不应进入 CLI 发起契约", err)
	}
}

// TestStartSchemaRequiresFixedUsageContext 验证 startReview 契约要求 CLI 固定生成用量上报业务编码。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；Schema 接受缺失或被覆盖的固定业务编码时通过 t.Fatal 报告。
func TestStartSchemaRequiresFixedUsageContext(t *testing.T) {
	missing := validContractInput("review-start.schema.json").(map[string]any)
	delete(missing, "usageReportContext")
	if err := ValidateRequest("smartAuditTaskStartReview", "POST", "/open-apis/contract-review/v3/smartAudit/task/startReview", missing); !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("缺少 usageReportContext 时 err=%v", err)
	}

	overridden := validContractInput("review-start.schema.json").(map[string]any)
	overridden["usageReportContext"] = map[string]any{"reportBusinessCode": "other-client"}
	if err := ValidateRequest("smartAuditTaskStartReview", "POST", "/open-apis/contract-review/v3/smartAudit/task/startReview", overridden); !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("覆盖 reportBusinessCode 时 err=%v", err)
	}
}

// TestReviewRunSchemaIsExecutable 验证本地复合工作流也通过同一套嵌入式 JSON Schema 执行运行时校验。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestReviewRunSchemaIsExecutable(t *testing.T) {
	valid := map[string]any{
		"source":     map[string]any{"type": "url", "fileUrl": "https://files.example.com/contract.pdf", "name": "合同.pdf"},
		"businessId": "biz-url",
		"fileHash":   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"config": map[string]any{
			"selectedPosition":             "甲方",
			"selectedAuditRole":            "甲方",
			"reviewStrength":               "中立",
			"selectedCheckListIds":         []string{"2001001"},
			"matchContractTypeRulePackage": true,
		},
		"wait": true,
	}
	if err := ValidateSchema("review-run.schema.json", valid); err != nil {
		t.Fatal(err)
	}
	withAppType := map[string]any{}
	for key, value := range valid {
		withAppType[key] = value
	}
	withAppType["appType"] = "THIRD_PARTY"
	if err := ValidateSchema("review-run.schema.json", withAppType); !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("err=%v，review run 不应接受 appType", err)
	}
	invalid := map[string]any{
		"source": map[string]any{"type": "file", "path": "contract.pdf", "name": "合同.pdf"},
	}
	if err := ValidateSchema("review-run.schema.json", invalid); !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("err=%v，review run 缺少必填 config 应被拒绝", err)
	}
}

// TestReviewStartSchemaAcceptsRuleSelectionOptions 验证审查规则来源参数可在 CLI 契约中传递。
func TestReviewStartSchemaAcceptsRuleSelectionOptions(t *testing.T) {
	input := map[string]any{
		"businessId":         "biz",
		"fileId":             "1",
		"fileHash":           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"usageReportContext": map[string]any{"reportBusinessCode": "everyLine_100_openApi_cli"},
		"config": map[string]any{
			"selectedPosition":             "甲方",
			"selectedAuditRole":            "甲方",
			"reviewStrength":               1,
			"selectedCheckListIds":         []string{"2001001"},
			"matchContractTypeRulePackage": true,
		},
	}
	if err := ValidateSchema("review-start.schema.json", input); err != nil {
		t.Fatalf("err=%v，规则来源参数应被 CLI 契约接受", err)
	}
}

// TestReviewSchemasRequireAtLeastOneRuleSource 验证 start HTTP 和 run CLI Schema 都拒绝规则来源全部缺失。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；任一 Schema 接受空规则来源时通过 t.Fatal 报告。
func TestReviewSchemasRequireAtLeastOneRuleSource(t *testing.T) {
	start := validContractInput("review-start.schema.json").(map[string]any)
	delete(start["config"].(map[string]any), "matchContractTypeRulePackage")
	if err := ValidateSchema("review-start.schema.json", start); !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("start err=%v，后端请求 Schema 应拒绝空规则来源", err)
	}

	run := map[string]any{
		"source": map[string]any{"type": "file", "path": "contract.pdf", "name": "合同.pdf"},
		"config": map[string]any{
			"selectedPosition":  "甲方",
			"selectedAuditRole": "甲方",
			"reviewStrength":    "中立",
		},
	}
	if err := ValidateSchema("review-run.schema.json", run); !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("run err=%v，CLI Schema 应拒绝空规则来源", err)
	}
}
