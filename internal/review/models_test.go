package review

import (
	"errors"
	"testing"
)

// TestStartInputMapsChineseReviewStrength 验证三个 CLI 中文强度稳定转换为后端数字枚举。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；校验或映射错误时通过 t.Fatal 报告。
func TestStartInputMapsChineseReviewStrength(t *testing.T) {
	for label, expected := range map[string]int{"弱势": 0, "中立": 1, "强势": 2} {
		t.Run(label, func(t *testing.T) {
			input := StartInput{
				BusinessID: "biz-1",
				FileID:     12,
				FileHash:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Config: map[string]any{
					"selectedPosition":             "xxx公司",
					"selectedAuditRole":            "xxx公司",
					"reviewStrength":               label,
					"matchContractTypeRulePackage": true,
				},
			}
			if err := input.Validate(); err != nil {
				t.Fatal(err)
			}
			request, err := input.ToRequest()
			if err != nil {
				t.Fatal(err)
			}
			if request.Config["reviewStrength"] != expected {
				t.Fatalf("config=%#v，期望 reviewStrength=%d", request.Config, expected)
			}
		})
	}
}

// TestStartInputRequiresAtLeastOneRuleSource 验证规则来源全缺失或显式 false 时返回可识别本地错误。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；错误类型不符合预期时通过 t.Fatal 报告。
func TestStartInputRequiresAtLeastOneRuleSource(t *testing.T) {
	input := StartInput{
		BusinessID: "biz-1",
		FileID:     12,
		FileHash:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Config: map[string]any{
			"selectedPosition":             "xxx公司",
			"selectedAuditRole":            "xxx公司",
			"reviewStrength":               "中立",
			"matchContractTypeRulePackage": false,
		},
	}
	if err := input.Validate(); !errors.Is(err, ErrReviewRuleSourceRequired) {
		t.Fatalf("err=%v", err)
	}
}
