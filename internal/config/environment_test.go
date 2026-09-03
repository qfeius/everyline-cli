package config

import "testing"

// TestResolveEnvironment 验证 dev、test、blue、prod 预设环境的基础地址和 token 地址保持一致。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestResolveEnvironment(t *testing.T) {
	tests := map[string]struct {
		baseURL        string
		authURL        string
		tokenURL       string
		deviceClientID string
	}{
		"dev": {
			baseURL:        "https://dev-open.qtech.cn",
			authURL:        "https://dev-contract-agent.qtech.cn",
			tokenURL:       "https://dev-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal",
			deviceClientID: "zscli_c77221e810ce3977",
		},
		"test": {
			baseURL:        "https://test-open.qtech.cn",
			authURL:        "https://test-contract-agent.qtech.cn",
			tokenURL:       "https://test-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal",
			deviceClientID: "zscli_c77221e810ce3977",
		},
		"blue": {
			baseURL:  "https://blue-open.qtech.cn",
			authURL:  "https://blue-contract-agent.qtech.cn",
			tokenURL: "https://blue-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal",
		},
		"prod": {
			baseURL:  "https://open.qfei.cn",
			authURL:  "https://contract-agent.qfei.cn",
			tokenURL: "https://open.qfei.cn/open-apis/auth/v3/tenant_access_token/internal",
		},
	}
	for name, expected := range tests {
		preset, err := ResolveEnvironment(name)
		if err != nil {
			t.Fatalf("environment=%s err=%v", name, err)
		}
		if preset.BaseURL != expected.baseURL || preset.AuthURL != expected.authURL || preset.TokenURL != expected.tokenURL || preset.OAuthDeviceClientID != expected.deviceClientID {
			t.Fatalf("environment=%s preset=%#v", name, preset)
		}
	}
}

// TestResolveEnvironmentDeviceClientID 验证旧 dev/test Profile 可按标准 metadata URL 自动选择 EveryLine Device client。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；内置环境未命中或自定义环境被误匹配时通过 t.Fatal 报告。
func TestResolveEnvironmentDeviceClientID(t *testing.T) {
	if got := ResolveEnvironmentDeviceClientID("https://test-myaccount.qtech.cn/.well-known/oauth-authorization-server/contract-review"); got != "zscli_c77221e810ce3977" {
		t.Fatalf("deviceClientID=%q", got)
	}
	if got := ResolveEnvironmentDeviceClientID("https://auth.example.com/metadata"); got != "" {
		t.Fatalf("自定义 metadata 不应匹配内置 client: %q", got)
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
