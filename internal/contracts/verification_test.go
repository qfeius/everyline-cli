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
		if err := ValidateRequest(spec.OperationID, spec.Method, path); err != nil {
			t.Errorf("operation=%s err=%v", spec.OperationID, err)
		}
	}
	if err := ValidateRequest("listReviewChecklists", "POST", "/open-apis/review-rules/review-checklists"); !errors.Is(err, ErrContractMismatch) {
		t.Fatalf("err=%v，期望 ErrContractMismatch", err)
	}
}
