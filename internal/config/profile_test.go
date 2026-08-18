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
