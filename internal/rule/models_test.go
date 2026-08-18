package rule

import "testing"

// TestRuleValidationMatchesSchema 验证 Go 校验不会放过 Schema 已禁止的缺失风险等级和负数来源类型。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestRuleValidationMatchesSchema(t *testing.T) {
	negativeSourceType := -1
	validRiskLevel := int32(0)
	tests := []struct {
		name    string
		payload Rule
	}{
		{"missing riskLevel", Rule{Name: "规则", Content: "内容"}},
		{"negative sourceType", Rule{Name: "规则", Content: "内容", RiskLevel: &validRiskLevel, SourceType: &negativeSourceType}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.payload.Validate(false); err == nil {
				t.Fatal("期望校验失败")
			}
		})
	}
}

// TestGroupValidationMatchesSchema 验证规则分组的 sourceType 最小值与 Schema 一致。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestGroupValidationMatchesSchema(t *testing.T) {
	negativeSourceType := -1
	if err := (Group{Name: "分组", SourceType: &negativeSourceType}).Validate(false); err == nil {
		t.Fatal("期望负数 sourceType 校验失败")
	}
}
