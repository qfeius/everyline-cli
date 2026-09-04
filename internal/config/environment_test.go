package config

import "testing"

// TestResolveEnvironment 验证 dev、test、blue、prod 预设环境的基础地址和 token 地址保持一致。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestResolveEnvironment(t *testing.T) {
	tests := map[string]struct {
		baseURL          string
		authURL          string
		tokenURL         string
		oauthMetadataURL string
	}{
		"dev": {
			baseURL:          "https://dev-open.qtech.cn",
			authURL:          "https://dev-contract-agent.qtech.cn",
			tokenURL:         "https://dev-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal",
			oauthMetadataURL: "https://dev-myaccount.qtech.cn/.well-known/oauth-authorization-server/contract-review",
		},
		"test": {
			baseURL:          "https://test-open.qtech.cn",
			authURL:          "https://test-contract-agent.qtech.cn",
			tokenURL:         "https://test-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal",
			oauthMetadataURL: "https://test-myaccount.qtech.cn/.well-known/oauth-authorization-server/contract-review",
		},
		"blue": {
			baseURL:  "https://blue-open.qtech.cn",
			authURL:  "https://blue-contract-agent.qtech.cn",
			tokenURL: "https://blue-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal",
		},
		"prod": {
			baseURL:          "https://open.qfei.cn",
			authURL:          "https://contract-agent.qfei.cn",
			tokenURL:         "https://open.qfei.cn/open-apis/auth/v3/tenant_access_token/internal",
			oauthMetadataURL: "https://myaccount.qfei.cn/.well-known/oauth-authorization-server/contract-review",
		},
	}
	for name, expected := range tests {
		preset, err := ResolveEnvironment(name)
		if err != nil {
			t.Fatalf("environment=%s err=%v", name, err)
		}
		if preset.BaseURL != expected.baseURL || preset.AuthURL != expected.authURL || preset.TokenURL != expected.tokenURL {
			t.Fatalf("environment=%s preset=%#v", name, preset)
		}
		if preset.OAuthMetadataURL != expected.oauthMetadataURL {
			t.Fatalf("environment=%s metadata=%q", name, preset.OAuthMetadataURL)
		}
		if expected.oauthMetadataURL != "" && (preset.OAuthBusinessType != "contract-review" || preset.OAuthRedirectURL == "" || len(preset.OAuthScopes) == 0) {
			t.Fatalf("environment=%s 缺少 OAuth 动态注册参数: %#v", name, preset)
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
