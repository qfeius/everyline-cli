package checklist

import "testing"

// TestChecklistValidationMatchesPublishedFields 验证公开清单创建说明中的 name 和 reviewRuleIds 必填约束。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestChecklistValidationMatchesPublishedFields(t *testing.T) {
	if err := (Checklist{Name: "清单"}).Validate(false); err == nil {
		t.Fatal("缺少 reviewRuleIds 时应校验失败")
	}
	if err := (Checklist{Name: "清单", ReviewRuleIDs: []string{"rule-1"}}).Validate(false); err != nil {
		t.Fatalf("已提供公开必填字段时不应失败: %v", err)
	}
	if err := (Checklist{Name: "清单", ReviewRuleIDs: []string{" "}}).Validate(false); err == nil {
		t.Fatal("空白 reviewRuleId 应校验失败")
	}
}
