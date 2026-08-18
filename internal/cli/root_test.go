package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/config"
)

// testRuntime 创建隔离文件仓库和内存 I/O 的 CLI 运行时。
// 入参：t *testing.T 为测试上下文。
// 返回值：*Runtime 为隔离依赖；*bytes.Buffer 为 stdout；*bytes.Buffer 为 stderr。
func testRuntime(t *testing.T) (*Runtime, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	directory := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runtime := NewRuntime(directory, strings.NewReader(""), stdout, stderr)
	runtime.Profiles = config.NewFileStore(filepath.Join(directory, "config.json"))
	runtime.Tokens = auth.NewFileTokenStore(filepath.Join(directory, "tokens.json"))
	return runtime, stdout, stderr
}

// TestConfigCommands 验证 config add/use/show 共享同一持久化 Store。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestConfigCommands(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	err := Execute(context.Background(), runtime, []string{
		"config", "add", "dev",
		"--base-url", "https://api.example.com",
		"--token-url", "https://api.example.com/token",
		"--app-id", "cli-dev",
		"--output", "json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"name": "dev"`) || strings.Contains(stdout.String(), "secret") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	profile, err := runtime.Profiles.Current()
	if err != nil || profile.Name != "dev" {
		t.Fatalf("profile=%#v err=%v", profile, err)
	}
}

// TestConfigOverwriteClearsStaleToken 验证同名 Profile 改变环境或 app ID 时不会复用旧 token。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestConfigOverwriteClearsStaleToken(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	profile := config.Profile{Name: "dev", BaseURL: "https://old.example.com", TokenURL: "https://old.example.com/token", AppID: "old-app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Tokens.Save("dev", auth.Token{AccessToken: "old-token", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	err := Execute(context.Background(), runtime, []string{
		"config", "add", "dev",
		"--base-url", "https://new.example.com",
		"--token-url", "https://new.example.com/token",
		"--app-id", "new-app",
		"--output", "json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Tokens.Load("dev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token 未清除: %v", err)
	}
}

// TestAuthStatusTrimsEnvironmentToken 验证纯空白环境变量不会被误报为已登录。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestAuthStatusTrimsEnvironmentToken(t *testing.T) {
	t.Setenv("EVERYLINE_ACCESS_TOKEN", "  \t\n")
	runtime, stdout, _ := testRuntime(t)
	profile := config.Profile{Name: "dev", BaseURL: "https://api.example.com", TokenURL: "https://api.example.com/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), runtime, []string{"auth", "status", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"authenticated": false`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestAuthLoginHonorsRootTimeout 验证 auth login 的网络请求受 --timeout 约束。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestAuthLoginHonorsRootTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case <-time.After(time.Second):
			_, _ = writer.Write([]byte(`{"code":0,"tenant_access_token":"late","expire":7200}`))
		case <-request.Context().Done():
		}
	}))
	defer server.Close()

	t.Setenv("EVERYLINE_APP_SECRET", "secret")
	runtime, _, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	profile := config.Profile{Name: "local", BaseURL: server.URL, TokenURL: server.URL + "/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err := Execute(context.Background(), runtime, []string{"auth", "login", "--timeout", "30ms"})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v，期望 context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("登录超时未及时生效: %s", elapsed)
	}
}

// TestReviewStartDryRun 验证严格 JSON 在无 Profile 时也能完成 dry-run，且不会调用远端。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestReviewStartDryRun(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	payload := `{"businessId":"biz-1","appType":"THIRD_PARTY","fileId":12,"fileHash":"hash","config":{}}`
	err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--data", payload, "--dry-run", "--output", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"fileId": 12`) || !strings.Contains(stdout.String(), `"config": {}`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestReviewStartRejectsUnknownField 验证命令处理器不会猜测未声明字段。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestReviewStartRejectsUnknownField(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	payload := `{"businessId":"biz-1","appType":"THIRD_PARTY","fileId":12,"fileHash":"hash","config":{},"typo":true}`
	err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--data", payload, "--dry-run"})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("err=%v", err)
	}
	if ExitCode(err) != ExitUsage {
		t.Fatalf("exit=%d", ExitCode(err))
	}
}

// TestReviewStartFeishuDryRun 验证字段捷径 V3 会校验并输出签名与额度字段且不调用远端。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestReviewStartFeishuDryRun(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	payload := `{"businessId":"biz-1","appType":"THIRD_PARTY","fileId":12,"fileHash":"hash","config":{},"hasQuota":true,"baseSignature":"payload.signature","packID":"pack-1"}`
	err := Execute(context.Background(), runtime, []string{"review", "task", "start-feishu", "--data", payload, "--dry-run", "--output", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"baseSignature": "payload.signature"`) || !strings.Contains(stdout.String(), `"hasQuota": true`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestChecklistCreateDryRun 验证清单命令按字段级类型校验 JSON 且 dry-run 不依赖 Profile。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestChecklistCreateDryRun(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	payload := `{"name":"采购清单","contractCategory":["PURCHASE"],"reviewStage":[1],"enabled":true,"reviewRuleIds":["rule-1"]}`
	if err := Execute(context.Background(), runtime, []string{"checklist", "create", "--data", payload, "--dry-run", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"name": "采购清单"`) || !strings.Contains(stdout.String(), `"reviewRuleIds"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestRuleBatchUpdateDryRun 验证规则批量更新要求同组参数和每项 ID。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestRuleBatchUpdateDryRun(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	payload := `[{"id":"rule-1","name":"付款期限","riskLevel":2,"content":"不得超过 60 天"}]`
	if err := Execute(context.Background(), runtime, []string{"rule", "batch-update", "--group-id", "group-1", "--data", payload, "--dry-run", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"groupId": "group-1"`) || !strings.Contains(stdout.String(), `"id": "rule-1"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestDestructiveCommandsRequireYes 验证真实删除在装配远端客户端前就要求显式确认。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestDestructiveCommandsRequireYes(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	err := Execute(context.Background(), runtime, []string{"rule", "group", "delete", "--id", "group-1"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err=%v", err)
	}
}

// TestReviewRunDryRunValidatesLocalFile 验证一键审查 dry-run 会检查文件存在性和稳定扩展名。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestReviewRunDryRunValidatesLocalFile(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	filePath := filepath.Join(t.TempDir(), "合同.PDF")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := `{"source":{"type":"file","path":"` + filePath + `","name":"合同.PDF"},"config":{},"wait":true}`
	if err := Execute(context.Background(), runtime, []string{"review", "run", "--data", payload, "--dry-run", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"appType": "THIRD_PARTY"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestExitCodePrefersNetwork 验证 token 刷新网络失败返回 5，而普通凭证缺失返回 3。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestExitCodePrefersNetwork(t *testing.T) {
	networkFailure := fmt.Errorf("%w: %w", auth.ErrAuthentication, &net.DNSError{Err: "timeout", IsTimeout: true})
	if ExitCode(networkFailure) != ExitNetwork {
		t.Fatalf("network exit=%d", ExitCode(networkFailure))
	}
	if ExitCode(auth.ErrCredentialsMissing) != ExitAuth {
		t.Fatalf("auth exit=%d", ExitCode(auth.ErrCredentialsMissing))
	}
}
