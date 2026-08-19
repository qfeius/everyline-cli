package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	"git.qtech.cn/ai/everyline-cli/internal/contracts"
	"git.qtech.cn/ai/everyline-cli/internal/review"
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

// TestConfigAddEnvironmentPreset 验证 config add 可用预设环境创建 test 和 blue Profile。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestConfigAddEnvironmentPreset(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	for _, test := range []struct {
		name     string
		baseURL  string
		tokenURL string
	}{
		{name: "test", baseURL: "https://test-open.qtech.cn", tokenURL: "https://test-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal"},
		{name: "blue", baseURL: "https://blue-open.qtech.cn", tokenURL: "https://blue-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal"},
	} {
		if err := Execute(context.Background(), runtime, []string{
			"config", "add", test.name,
			"--env", test.name,
			"--app-id", "cli-" + test.name,
		}); err != nil {
			t.Fatalf("environment=%s err=%v", test.name, err)
		}
		profile, err := runtime.Profiles.Get(test.name)
		if err != nil {
			t.Fatalf("environment=%s profile err=%v", test.name, err)
		}
		if profile.BaseURL != test.baseURL || profile.TokenURL != test.tokenURL {
			t.Fatalf("environment=%s profile=%#v", test.name, profile)
		}
		if profile.AuthURL == "" {
			t.Fatalf("environment=%s 缺少自有认证页面: %#v", test.name, profile)
		}
	}
}

// TestAuthUserLoginAndUse 验证 user token 从 stdin 缓存后可切换为 Profile 默认身份。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestAuthUserLoginAndUse(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	runtime.Input = strings.NewReader("user-token\n")
	profile := config.Profile{Name: "dev", BaseURL: "https://api.example.com", AuthURL: "https://dev-contract-agent.qtech.cn", TokenURL: "https://api.example.com/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), runtime, []string{"auth", "login", "--as", "user", "--access-token-stdin", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"identity": "user"`) {
		t.Fatalf("login output=%s", stdout.String())
	}
	stdout.Reset()
	if err := Execute(context.Background(), runtime, []string{"auth", "use", "--as", "user", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	current, err := runtime.Profiles.Current()
	if err != nil || current.DefaultIdentity != config.IdentityUser {
		t.Fatalf("current=%#v err=%v", current, err)
	}
	stdout.Reset()
	if err := Execute(context.Background(), runtime, []string{"auth", "status", "--as", "user", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"authenticated": true`) {
		t.Fatalf("status output=%s", stdout.String())
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
	payload := `{"businessId":"biz-1","appType":"THIRD_PARTY","fileId":12,"fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--data", payload, "--dry-run", "--output", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"fileId": 12`) || !strings.Contains(stdout.String(), `"config": {}`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestReviewSubjectExtractDryRun 验证主体提取命令的 dry-run 输出与真实请求保持字符串 fileId 类型一致。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；输出类型漂移时通过 t.Fatal 报告。
func TestReviewSubjectExtractDryRun(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	err := Execute(context.Background(), runtime, []string{
		"review", "subject", "extract",
		"--business-id", "biz-1",
		"--file-id", "12",
		"--dry-run", "--output", "json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"fileId": "12"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestReviewStartRejectsUnknownField 验证命令处理器不会猜测未声明字段。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestReviewStartRejectsUnknownField(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	payload := `{"businessId":"biz-1","appType":"THIRD_PARTY","fileId":12,"fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","config":{},"typo":true}`
	err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--data", payload, "--dry-run"})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("err=%v", err)
	}
	if ExitCode(err) != ExitUsage {
		t.Fatalf("exit=%d", ExitCode(err))
	}
}

// TestReviewStartRejectsUsageReportContext 验证 startReview 不接受仅供服务端内部自动触发链路使用的字段。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；字段出现在 CLI 请求时通过未知字段错误阻断。
func TestReviewStartRejectsUsageReportContext(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	payload := `{"businessId":"biz-1","appType":"THIRD_PARTY","fileId":12,"fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","usageReportContext":{}}`
	err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--data", payload, "--dry-run"})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("err=%v", err)
	}
}

// TestReviewStartFeishuDryRun 验证字段捷径入口校验并输出后端当前字段，且不调用远端。
func TestReviewStartFeishuDryRun(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	payload := `{"fileId":12,"reviewStrength":0,"selectedPosition":"legal","hasQuota":true,"baseSignature":"payload.signature","packID":"pack-1"}`
	err := Execute(context.Background(), runtime, []string{"review", "task", "start-feishu", "--data", payload, "--dry-run", "--output", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"reviewStrength": 0`) || !strings.Contains(stdout.String(), `"baseSignature": "payload.signature"`) || !strings.Contains(stdout.String(), `"hasQuota": true`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestReviewRunURLDryRunDefaultsConfig 验证 URL 工作流的身份字段和默认配置在 CLI 层可见。
func TestReviewRunURLDryRunDefaultsConfig(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	payload := `{"source":{"type":"url","fileUrl":"https://files.example.com/contract.pdf","name":"合同.pdf"},"businessId":"biz-url","fileHash":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`
	if err := Execute(context.Background(), runtime, []string{"review", "run", "--data", payload, "--dry-run", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"businessId": "biz-url"`) || !strings.Contains(stdout.String(), `"fileHash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`) || !strings.Contains(stdout.String(), `"config": {}`) {
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

// TestDryRunRejectsBlankPathIDs 验证 dry-run 不会让只含空白的必填路径 ID 绕过校验。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestDryRunRejectsBlankPathIDs(t *testing.T) {
	tests := [][]string{
		{"checklist", "update", "--id", "  ", "--data", `{"name":"清单","reviewRuleIds":["rule-1"]}`, "--dry-run"},
		{"rule", "create", "--group-id", "\t", "--data", `{"name":"规则","riskLevel":1,"content":"内容"}`, "--dry-run"},
	}
	for _, args := range tests {
		runtime, _, _ := testRuntime(t)
		if err := Execute(context.Background(), runtime, args); err == nil || !strings.Contains(err.Error(), "不能为空") {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
}

// TestRuleDryRunRequiresRiskLevel 验证缺失 riskLevel 的规则请求不会被零值掩盖。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestRuleDryRunRequiresRiskLevel(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	err := Execute(context.Background(), runtime, []string{
		"rule", "create", "--group-id", "group-1", "--data", `{"name":"规则","content":"内容"}`, "--dry-run",
	})
	if err == nil || !strings.Contains(err.Error(), "riskLevel 不能为空") {
		t.Fatalf("err=%v", err)
	}
}

// TestSingleUpdateDryRunEnforcesOneIDSource 验证三个单项更新命令拒绝冲突 ID，且一致 ID 不进入 body。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestSingleUpdateDryRunEnforcesOneIDSource(t *testing.T) {
	tests := [][]string{
		{"checklist", "update", "--id", "check-1", "--data", `{"id":"other","name":"清单","reviewRuleIds":["rule-1"]}`, "--dry-run"},
		{"rule", "group", "update", "--id", "group-1", "--data", `{"id":"other","name":"分组"}`, "--dry-run"},
		{"rule", "update", "--group-id", "group-1", "--rule-id", "rule-1", "--data", `{"id":"other","name":"规则","riskLevel":1,"content":"内容"}`, "--dry-run"},
	}
	for _, args := range tests {
		runtime, _, _ := testRuntime(t)
		if err := Execute(context.Background(), runtime, args); err == nil || !strings.Contains(err.Error(), "不一致") {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}

	runtime, stdout, _ := testRuntime(t)
	err := Execute(context.Background(), runtime, []string{"checklist", "update", "--id", "check-1", "--data", `{"id":"check-1","name":"清单","reviewRuleIds":["rule-1"]}`, "--dry-run", "--output", "json"})
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		ID   string         `json:"id"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.ID != "check-1" {
		t.Fatalf("path id=%q", output.ID)
	}
	if _, exists := output.Data["id"]; exists {
		t.Fatalf("dry-run body 不应包含 id: %#v", output.Data)
	}
}

// TestRuleGroupDeleteDryRunOmitsUndefinedCascadeField 验证分组删除预览只包含接口路径 ID。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestRuleGroupDeleteDryRunOmitsUndefinedCascadeField(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	if err := Execute(context.Background(), runtime, []string{"rule", "group", "delete", "--id", "group-1", "--dry-run", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if _, exists := output["cascade"]; exists {
		t.Fatalf("dry-run 不应输出未定义的 cascade 字段: %#v", output)
	}
	if output["id"] != "group-1" {
		t.Fatalf("output=%#v", output)
	}
}

// TestBatchDeleteDryRunRejectsDuplicateIDs 验证 flag 和 JSON 两种批量删除输入都拒绝规范化后的重复项。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestBatchDeleteDryRunRejectsDuplicateIDs(t *testing.T) {
	tests := [][]string{
		{"checklist", "batch-delete", "--id", "same", "--id", " same ", "--dry-run"},
		{"rule", "batch-delete", "--group-id", "group-1", "--data", `["same","same"]`, "--dry-run"},
	}
	for _, args := range tests {
		runtime, _, _ := testRuntime(t)
		if err := Execute(context.Background(), runtime, args); err == nil || !strings.Contains(err.Error(), "重复") {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
}

// TestChecklistBatchDeleteDryRunMatchesRequestBody 验证 dry-run 输出与真实批量删除 JSON body 保持一致。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestChecklistBatchDeleteDryRunMatchesRequestBody(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	err := Execute(context.Background(), runtime, []string{"checklist", "batch-delete", "--id", "check-1", "--id", "check-2", "--dry-run", "--output", "json"})
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.IDs) != 2 || output.IDs[0] != "check-1" || output.IDs[1] != "check-2" {
		t.Fatalf("output=%s，期望对象形状的 ids", stdout.String())
	}
}

// TestExitCodeMapsTaskFailureToAPI 验证远端审查任务失败不会被误判为参数错误或成功。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestExitCodeMapsTaskFailureToAPI(t *testing.T) {
	if code := ExitCode(review.ErrTaskFailed); code != ExitAPI {
		t.Fatalf("code=%d，期望 %d", code, ExitAPI)
	}
}

// TestUnverifiedBatchWriteFailsBeforeProfile 验证真实批量写在装配鉴权前就返回可识别的契约错误。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestUnverifiedBatchWriteFailsBeforeProfile(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	err := Execute(context.Background(), runtime, []string{
		"checklist", "batch-create", "--data", `[{"name":"清单","reviewRuleIds":["rule-1"]}]`,
	})
	if !errors.Is(err, contracts.ErrContractUnverified) {
		t.Fatalf("err=%v，期望 ErrContractUnverified", err)
	}
}

// TestCompletionCommandAvailable 验证 Cobra 标准 shell completion 命令未被隐藏。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestCompletionCommandAvailable(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	root := NewRootCommand(runtime)
	if _, _, err := root.Find([]string{"completion", "zsh"}); err != nil {
		t.Fatalf("completion 命令不可用: %v", err)
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

// TestReviewRunDryRunAllowsExtensionlessLocalPath 验证 source.name 承担文件格式校验，临时路径可不带扩展名。
func TestReviewRunDryRunAllowsExtensionlessLocalPath(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	filePath := filepath.Join(t.TempDir(), "upload")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := `{"source":{"type":"file","path":"` + filePath + `","name":"合同.pdf"},"config":{}}`
	if err := Execute(context.Background(), runtime, []string{"review", "run", "--data", payload, "--dry-run", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"path": "`+filePath+`"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestReviewRunRendersPartialResultOnFailure 验证远端失败时命令先输出 upload/start/final 快照，再返回任务错误。
func TestReviewRunRendersPartialResultOnFailure(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/open-apis/contract-review/v3/file/contract/upload":
			_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":{"fileId":11,"businessId":"biz-1","fileHash":"` + hash + `"}}`))
		case "/open-apis/contract-review/v3/smartAudit/task/startReview":
			_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":{"taskId":88,"status":"running"}}`))
		case "/open-apis/contract-review/v3/smartAudit/task/status":
			_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":{"taskId":88,"status":"fail","message":"规则失败"}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	t.Setenv("EVERYLINE_ACCESS_TOKEN", "test-token")
	runtime, stdout, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	profile := config.Profile{Name: "local", BaseURL: server.URL, TokenURL: server.URL + "/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(t.TempDir(), "upload")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := `{"source":{"type":"file","path":"` + filePath + `","name":"合同.pdf"},"businessId":"biz-1","appType":"THIRD_PARTY","wait":true}`
	err := Execute(context.Background(), runtime, []string{"review", "run", "--data", payload, "--interval", "1ms", "--deadline", "1s", "--output", "json"})
	if !errors.Is(err, review.ErrTaskFailed) {
		t.Fatalf("err=%v", err)
	}
	for _, field := range []string{`"upload"`, `"start"`, `"final"`, `"status": "fail"`} {
		if !strings.Contains(stdout.String(), field) {
			t.Fatalf("stdout=%s，缺少 %s", stdout.String(), field)
		}
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
	if ExitCode(auth.ErrUserAuthentication) != ExitAuth {
		t.Fatalf("user auth exit=%d", ExitCode(auth.ErrUserAuthentication))
	}
}

// TestExitCodeMapsContractFailuresToAPI 验证未核验和运行时契约漂移不会被误报为命令用法错误。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestExitCodeMapsContractFailuresToAPI(t *testing.T) {
	for _, err := range []error{contracts.ErrContractUnverified, contracts.ErrContractMismatch} {
		if ExitCode(err) != ExitAPI {
			t.Fatalf("err=%v exit=%d", err, ExitCode(err))
		}
	}
}
