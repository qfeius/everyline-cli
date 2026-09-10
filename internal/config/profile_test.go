package config

import (
	"strings"
	"testing"
)

// TestProfileRequiresHTTPSForRemoteHosts 验证远端 Profile 不允许明文传输 app secret 或 token。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestProfileRequiresHTTPSForRemoteHosts(t *testing.T) {
	profile := Profile{
		Name:          "prod",
		BaseURL:       "http://api.example.com",
		TokenURL:      "https://api.example.com/token",
		AppID:         "cli-prod",
		DefaultOutput: "json",
	}
	if err := profile.Validate(); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("err=%v，期望拒绝远端 HTTP", err)
	}
}

// TestProfileAllowsLoopbackHTTP 验证 localhost 和回环 IP 可用于本地契约测试。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestProfileAllowsLoopbackHTTP(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "[::1]"} {
		profile := Profile{
			Name:          "local",
			BaseURL:       "http://" + host + ":8080",
			TokenURL:      "http://" + host + ":8080/token",
			AppID:         "cli-local",
			DefaultOutput: "json",
		}
		if err := profile.Validate(); err != nil {
			t.Fatalf("host=%s err=%v", host, err)
		}
	}
}

// TestProfileRejectsUnusableURLs 验证 Profile 在持久化前拒绝运行时无法安全拼接的 URL。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestProfileRejectsUnusableURLs(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		tokenURL string
	}{
		{name: "base URL 首尾空白", baseURL: " https://open.qfei.cn ", tokenURL: "https://open.qfei.cn/token"},
		{name: "token URL 首尾空白", baseURL: "https://open.qfei.cn", tokenURL: " https://open.qfei.cn/token "},
		{name: "base URL query", baseURL: "https://open.qfei.cn?tenant=test", tokenURL: "https://open.qfei.cn/token"},
		{name: "base URL fragment", baseURL: "https://open.qfei.cn#gateway", tokenURL: "https://open.qfei.cn/token"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := Profile{Name: "prod", BaseURL: test.baseURL, TokenURL: test.tokenURL, AppID: "cli-prod", DefaultOutput: "json"}
			if err := profile.Validate(); err == nil {
				t.Fatalf("profile=%#v，期望拒绝不可用 URL", profile)
			}
		})
	}
}

/*
TestProfileUsesConfiguredDeviceClient 验证显式 Device client 优先，只为 blue 自动补全，已移除环境不再补全。
入参：t *testing.T 为测试上下文。
返回值：无；预设失效、发生跨授权方式复用或显式值失效时通过 t.Fatal 报告。
*/
func TestProfileUsesConfiguredDeviceClient(t *testing.T) {
	profile := Profile{
		OAuthMetadataURL: "https://myaccount-b.qfei.cn/.well-known/oauth-authorization-server/contract-review",
		OAuthClientID:    "browser-client",
	}
	if got := profile.EffectiveOAuthDeviceClientID(); got != "zscli_bc60fee4de9913ae" {
		t.Fatalf("deviceClientID=%q", got)
	}
	profile.OAuthDeviceClientID = "explicit-device-client"
	if got := profile.EffectiveOAuthDeviceClientID(); got != "explicit-device-client" {
		t.Fatalf("显式 deviceClientID=%q", got)
	}
	for _, metadataURL := range []string{
		"https://dev-myaccount.qtech.cn/.well-known/oauth-authorization-server/contract-review",
		"https://test-myaccount.qtech.cn/.well-known/oauth-authorization-server/contract-review",
		"https://myaccount.qfei.cn/.well-known/oauth-authorization-server/contract-review",
		"https://account.example.com/.well-known/oauth-authorization-server/contract-review",
		"",
	} {
		profile.OAuthMetadataURL = metadataURL
		profile.OAuthDeviceClientID = "explicit-device-client"
		if got := profile.EffectiveOAuthDeviceClientID(); got != "explicit-device-client" {
			t.Fatalf("metadata=%q 显式 deviceClientID=%q", metadataURL, got)
		}
		profile.OAuthDeviceClientID = ""
		if got := profile.EffectiveOAuthDeviceClientID(); got != "" {
			t.Fatalf("metadata=%q 不应补全或复用浏览器 client: %q", metadataURL, got)
		}
	}
}
