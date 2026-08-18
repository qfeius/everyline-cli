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

// TestCatalogValidatesRuntimeRequests 验证 27 项目录可校验固定路径和路径参数，并拒绝方法漂移。
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

// validContractInput 为目录中的每类 Schema 提供最小合法输入，确保 27 项都执行真实 Schema 编译与校验。
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
	case "review-file-snapshot.schema.json":
		return map[string]any{"fileId": 1, "businessId": "biz"}
	case "review-subject.schema.json":
		return map[string]any{"businessId": "biz", "appType": "THIRD_PARTY", "fileId": 1, "fileHash": "hash"}
	case "review-start.schema.json":
		return map[string]any{"businessId": "biz", "appType": "THIRD_PARTY", "fileId": 1, "fileHash": "hash", "config": map[string]any{}}
	case "review-start-feishu.schema.json":
		return map[string]any{"businessId": "biz", "appType": "THIRD_PARTY", "fileId": 1, "fileHash": "hash", "config": map[string]any{}, "hasQuota": true, "baseSignature": "signature", "packID": "pack"}
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
