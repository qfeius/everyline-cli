package config

import (
	"strings"
	"testing"
)

/*
TestResolveEnvironment 验证 blue 环境连接参数和平台专用 Device client。
入参：t *testing.T 为测试上下文。
返回值：无；失败通过 t.Fatal 报告。
*/
func TestResolveEnvironment(t *testing.T) {
	tests := map[string]struct {
		baseURL          string
		authURL          string
		tokenURL         string
		oauthMetadataURL string
		deviceClientID   string
	}{
		"blue": {
			oauthMetadataURL: "https://myaccount-b.qfei.cn/.well-known/oauth-authorization-server/contract-review",
			deviceClientID:   "zscli_bc60fee4de9913ae",
			baseURL:          "https://open-b.qfei.cn",
			authURL:          "https://contract-agent-b.qfei.cn",
			tokenURL:         "https://open-b.qfei.cn/open-apis/auth/v3/tenant_access_token/internal",
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
		if preset.OAuthBusinessType != "contract-review" || preset.OAuthRedirectURL != "http://127.0.0.1:8000/login" || len(preset.OAuthScopes) != 1 || preset.OAuthScopes[0] != "contract-review:full" {
			t.Fatalf("environment=%s 缺少 OAuth 动态注册参数: %#v", name, preset)
		}
	}
}

/*
TestResolveEnvironmentRejectsUnknown 验证已移除和未知环境返回错误，不回退到 blue。
入参：t *testing.T 为测试上下文。
返回值：无；错误文本或解析结果不符合预期时通过 t.Fatal 报告。
*/
func TestResolveEnvironmentRejectsUnknown(t *testing.T) {
	for _, name := range []string{"dev", "test", "prod", " DEV ", "TEST", "Prod", "staging", ""} {
		t.Run(name, func(t *testing.T) {
			preset, err := ResolveEnvironment(name)
			if err == nil || !strings.Contains(err.Error(), "可选环境为 blue") {
				t.Fatalf("environment=%q err=%v", name, err)
			}
			if preset.BaseURL != "" || preset.OAuthDeviceClientID != "" {
				t.Fatalf("拒绝环境时不应返回预设: %#v", preset)
			}
		})
	}
}
