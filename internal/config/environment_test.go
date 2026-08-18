package config

import "testing"

// TestResolveEnvironment 验证 dev、test、blue、prod 预设环境的基础地址和 token 地址保持一致。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestResolveEnvironment(t *testing.T) {
	tests := map[string]struct {
		baseURL  string
		tokenURL string
	}{
		"dev": {
			baseURL:  "https://dev-open.qtech.cn",
			tokenURL: "https://dev-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal",
		},
		"test": {
			baseURL:  "https://test-open.qtech.cn",
			tokenURL: "https://test-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal",
		},
		"blue": {
			baseURL:  "https://blue-open.qtech.cn",
			tokenURL: "https://blue-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal",
		},
		"prod": {
			baseURL:  "https://open.qfei.cn",
			tokenURL: "https://open.qfei.cn/open-apis/auth/v3/tenant_access_token/internal",
		},
	}
	for name, expected := range tests {
		preset, err := ResolveEnvironment(name)
		if err != nil {
			t.Fatalf("environment=%s err=%v", name, err)
		}
		if preset.BaseURL != expected.baseURL || preset.TokenURL != expected.tokenURL {
			t.Fatalf("environment=%s preset=%#v", name, preset)
		}
	}
}

// TestResolveEnvironmentRejectsUnknown 验证未知环境不会静默生成错误地址。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestResolveEnvironmentRejectsUnknown(t *testing.T) {
	if _, err := ResolveEnvironment("staging"); err == nil {
		t.Fatal("未知环境应返回错误")
	}
}
