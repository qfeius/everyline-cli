package config

import "testing"

// TestResolveEnvironment 验证四套环境连接参数，并锁定 dev/test/prod 的平台专用 Device client。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestResolveEnvironment(t *testing.T) {
	tests := map[string]struct {
		baseURL          string
		authURL          string
		tokenURL         string
		oauthMetadataURL string
		deviceClientID   string
	}{
		"prod": {
			baseURL:          "https://open.qfei.cn",
			authURL:          "https://contract-agent.qfei.cn",
			tokenURL:         "https://open.qfei.cn/open-apis/auth/v3/tenant_access_token/internal",
			oauthMetadataURL: "https://myaccount.qfei.cn/.well-known/oauth-authorization-server/contract-review",
			deviceClientID:   "zscli_bc60fee4de9913ae",
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
		if preset.OAuthDeviceClientID != expected.deviceClientID {
			t.Fatalf("environment=%s deviceClientID=%q", name, preset.OAuthDeviceClientID)
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
	for _, name := range []string{"dev", "test", "blue", "staging"} {
		if _, err := ResolveEnvironment(name); err == nil {
			t.Fatalf("环境 %s 应返回错误", name)
		}
	}
}
