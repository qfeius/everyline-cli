package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/build"
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
	runtime.Secrets = auth.NewFileSecretStore(filepath.Join(directory, "secrets.json"))
	return runtime, stdout, stderr
}

// requireFirstInstallAuthorization 为命令测试建立待完成的新安装授权门禁。
// 入参：t *testing.T 为测试上下文；runtime *Runtime 为待修改运行时。
// 返回值：无；保存失败时通过 t.Fatal 报告。
func requireFirstInstallAuthorization(t *testing.T, runtime *Runtime) {
	t.Helper()
	if err := runtime.InstallState.Save(config.InstallState{
		Schema: config.InstallStateSchema, EventID: "first-install-test", InstalledVersion: "0.0.2",
		FirstInstall: true, AuthorizationRequired: true, NextAction: "authorize",
	}); err != nil {
		t.Fatal(err)
	}
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

// TestConfigAddDefaultsToJSONAndPreservesExplicitOutput 验证新 Profile 默认 json，覆盖时保留用户显式 table 配置。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；默认或覆盖行为漂移时通过 t.Fatal 报告。
func TestConfigAddDefaultsToJSONAndPreservesExplicitOutput(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	baseArgs := []string{"config", "add", "dev", "--base-url", "https://api.example.com", "--token-url", "https://api.example.com/token", "--app-id", "cli-dev"}
	if err := Execute(context.Background(), runtime, baseArgs); err != nil {
		t.Fatal(err)
	}
	profile, err := runtime.Profiles.Get("dev")
	if err != nil || profile.DefaultOutput != "json" {
		t.Fatalf("profile=%#v err=%v，省略 default-output 时应为 json", profile, err)
	}

	withTable := append(append([]string{}, baseArgs...), "--default-output", "table", "--output", "json")
	if err := Execute(context.Background(), runtime, withTable); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), runtime, append(append([]string{}, baseArgs...), "--output", "json")); err != nil {
		t.Fatal(err)
	}
	profile, err = runtime.Profiles.Get("dev")
	if err != nil || profile.DefaultOutput != "table" {
		t.Fatalf("profile=%#v err=%v，覆盖 Profile 时应保留显式 table", profile, err)
	}
}

// TestConfigAddEnvironmentPreset 验证 config add 可创建四套预设，Codex 保留动态注册参数，dev/test 同时内置专用 Device client。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestConfigAddEnvironmentPreset(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	for _, test := range []struct {
		name     string
		baseURL  string
		tokenURL string
	}{
		{name: "dev", baseURL: "https://dev-open.qtech.cn", tokenURL: "https://dev-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal"},
		{name: "test", baseURL: "https://test-open.qtech.cn", tokenURL: "https://test-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal"},
		{name: "blue", baseURL: "https://blue-open.qtech.cn", tokenURL: "https://blue-open.qtech.cn/open-apis/auth/v3/tenant_access_token/internal"},
		{name: "prod", baseURL: "https://open.qfei.cn", tokenURL: "https://open.qfei.cn/open-apis/auth/v3/tenant_access_token/internal"},
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
		if test.name != "blue" && !profile.HasOAuthClientRegistrationConfiguration() {
			t.Fatalf("environment=%s 缺少 Codex OAuth 动态注册预设: %#v", test.name, profile)
		}
		if profile.OAuthClientID != "" {
			t.Fatalf("environment=%s 不应内置浏览器 client_id: %#v", test.name, profile)
		}
		expectedDeviceClientID := ""
		if test.name == "dev" || test.name == "test" {
			expectedDeviceClientID = "zscli_c77221e810ce3977"
		}
		if profile.OAuthDeviceClientID != expectedDeviceClientID {
			t.Fatalf("environment=%s deviceClientID=%q", test.name, profile.OAuthDeviceClientID)
		}
		if expectedDeviceClientID != "" && !profile.HasDeviceOAuthConfiguration() {
			t.Fatalf("environment=%s 缺少 Device Grant 预设: %#v", test.name, profile)
		}
	}
}

// TestVersionReportsLatestStateAndUpdateCommand 验证 version 输出最新版本判断和可执行更新命令。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；结构化字段缺失或比较错误时通过 t.Fatal 报告。
func TestVersionReportsLatestStateAndUpdateCommand(t *testing.T) {
	originalVersion := build.Version
	originalManifestURL := build.UpdateManifestURL
	build.Version = "1.0.0"
	build.UpdateManifestURL = ""
	t.Cleanup(func() {
		build.Version = originalVersion
		build.UpdateManifestURL = originalManifestURL
	})

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"version":"1.1.0","platforms":{"test":{"url":"https://updates.example.com/cli","sha256":"unused"}}}`))
	}))
	defer server.Close()
	runtime, stdout, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	if err := Execute(context.Background(), runtime, []string{"version", "--manifest-url", server.URL}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"version": "1.0.0"`, `"latestVersion": "1.1.0"`, `"isLatest": false`, `"updateRequired": true`, `"updateCommand": "everyline-cli update --manifest-url ` + server.URL + `"`} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("stdout=%s，缺少 %s", stdout.String(), expected)
		}
	}
}

/*
TestFirstInstallStatusRejectsExistingToken 验证首次安装拒绝旧 token，并通过事件提供指定的安装与授权帮助文案。
入参：t *testing.T 为测试上下文。
返回值：无；状态仍信任旧 token 或缺少正确的授权引导时通过 t.Fatal 报告。
*/
func TestFirstInstallStatusRejectsExistingToken(t *testing.T) {
	runtime, stdout, stderr := testRuntime(t)
	profile := config.Profile{Name: "dev", BaseURL: "https://api.example.com", TokenURL: "https://api.example.com/token", AppID: "app", DefaultIdentity: config.IdentityUser, DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	identityStore := runtime.Tokens.(interface {
		SaveForIdentity(string, config.IdentityKind, auth.Token) error
	})
	if err := identityStore.SaveForIdentity(profile.Name, config.IdentityUser, auth.Token{AccessToken: "old-dev-token", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	requireFirstInstallAuthorization(t, runtime)

	if err := Execute(context.Background(), runtime, []string{"auth", "status", "--profile", profile.Name, "--as", "user", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"authenticated": false`, `"source": "first_install"`, `"authorizationRequired": true`, `"nextAction": "everyline-cli auth login --profile dev --as user --output json"`} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("stdout=%s，缺少 %s", stdout.String(), expected)
		}
	}
	if !strings.Contains(stderr.String(), `"event":"first_install"`) || !strings.Contains(stderr.String(), `"eventId":"first-install-test"`) || !strings.Contains(stderr.String(), `"recommendedSkill":"everyline-cli"`) || !strings.Contains(stderr.String(), `"message":"EveryLine CLI 已安装完成。目前支持合同审查，以及审查清单、规则和规则分组配置。使用前需要先完成账号授权，我现在可以为你打开授权页面或生成授权链接。"`) {
		t.Fatalf("stderr=%s，缺少首次安装 NDJSON 事件", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if err := Execute(context.Background(), runtime, []string{"version", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"firstInstall": true`, `"authorizationRequired": true`, `"nextAction": "authorize"`} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("version stdout=%s，缺少 %s", stdout.String(), expected)
		}
	}
}

// TestFirstInstallBlocksBusinessCommands 验证完成新授权前不会调用任何业务 API。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；业务请求穿透门禁或退出类型错误时通过 t.Fatal 报告。
func TestFirstInstallBlocksBusinessCommands(t *testing.T) {
	businessCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		businessCalls++
		_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":[]}`))
	}))
	defer server.Close()
	t.Setenv("EVERYLINE_ACCESS_TOKEN", "old-app-token")
	runtime, stdout, stderr := testRuntime(t)
	runtime.HTTP = server.Client()
	if err := runtime.Profiles.Add(config.Profile{Name: "dev", BaseURL: server.URL, TokenURL: server.URL + "/token", AppID: "app", DefaultOutput: "json"}); err != nil {
		t.Fatal(err)
	}
	requireFirstInstallAuthorization(t, runtime)

	err := Execute(context.Background(), runtime, []string{"checklist", "list", "--profile", "dev", "--as", "app", "--output", "json"})
	if !errors.Is(err, auth.ErrUserAuthentication) || ExitCode(err) != ExitAuth {
		t.Fatalf("err=%v exit=%d，期望首次安装鉴权门禁", err, ExitCode(err))
	}
	if businessCalls != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"authorizationRequired":true`) {
		t.Fatalf("businessCalls=%d stdout=%q stderr=%q", businessCalls, stdout.String(), stderr.String())
	}
	if !strings.Contains(err.Error(), "everyline-cli Skill") {
		t.Fatalf("err=%v，首次安装提示未指向合并后的 Skill", err)
	}
}

// TestVersionExplicitManifestKeepsOverrideInUpdateCommand 验证显式检查源不会被环境默认源替换。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；建议命令没有保留显式 manifest 时通过 t.Fatal 报告。
func TestVersionExplicitManifestKeepsOverrideInUpdateCommand(t *testing.T) {
	originalVersion := build.Version
	build.Version = "1.0.0"
	t.Cleanup(func() { build.Version = originalVersion })
	t.Setenv("EVERYLINE_CLI_UPDATE_MANIFEST_URL", "https://updates-a.example.com/manifest.json")

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"version":"1.1.0","platforms":{"test":{"url":"https://updates.example.com/cli","sha256":"unused"}}}`))
	}))
	defer server.Close()
	runtime, stdout, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	if err := Execute(context.Background(), runtime, []string{"version", "--manifest-url", server.URL}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"updateCommand": "everyline-cli update --manifest-url `+server.URL+`"`) {
		t.Fatalf("stdout=%s，显式 manifest 必须保留在更新命令中", stdout.String())
	}
}

// TestVersionCheckFailureIsNonBlockingAndUnknown 验证检查失败时 version 仍成功，且不会把 isLatest 误报为 true。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；退出或状态语义错误时通过 t.Fatal 报告。
func TestVersionCheckFailureIsNonBlockingAndUnknown(t *testing.T) {
	originalVersion := build.Version
	build.Version = "1.0.0"
	t.Cleanup(func() { build.Version = originalVersion })
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"invalid":true}`))
	}))
	defer server.Close()
	runtime, stdout, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	if err := Execute(context.Background(), runtime, []string{"version", "--manifest-url", server.URL}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"isLatest": null`) || strings.Contains(stdout.String(), `"isLatest": true`) || !strings.Contains(stdout.String(), `"updateRequired": false`) || !strings.Contains(stdout.String(), `"checkError"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestVersionUpdateCommandForNPMIncludesSkillInstaller 验证 npm 更新命令会运行负责同步登记 Skills 的安装脚本。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；更新命令遗漏包级脚本许可时通过 t.Fatal 报告。
func TestVersionUpdateCommandForNPMIncludesSkillInstaller(t *testing.T) {
	t.Setenv("EVERYLINE_CLI_WRAPPER", "1")
	const expected = "npm install -g --allow-scripts=everyline-cli everyline-cli@latest"
	if actual := versionUpdateCommand("https://updates.example.test/manifest.json", false); actual != expected {
		t.Fatalf("updateCommand=%q，期望 %q", actual, expected)
	}
}

// TestBusinessCommandDefersUpdateUntilWorkflowCompletes 验证发现新版时仍完成当前业务 API，并输出机器可读的延迟更新状态。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；业务被阻断、更新状态缺失或业务结果丢失时通过 t.Fatal 报告。
func TestBusinessCommandDefersUpdateUntilWorkflowCompletes(t *testing.T) {
	originalVersion := build.Version
	build.Version = "1.0.0"
	t.Cleanup(func() { build.Version = originalVersion })
	manifestServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"version":"1.1.0","platforms":{"test":{"url":"https://updates.example.com/cli","sha256":"unused"}}}`))
	}))
	defer manifestServer.Close()
	businessCalls := 0
	businessServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		businessCalls++
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":[]}`))
	}))
	defer businessServer.Close()
	t.Setenv("EVERYLINE_CLI_UPDATE_MANIFEST_URL", manifestServer.URL)
	t.Setenv("EVERYLINE_ACCESS_TOKEN", "test-token")
	runtime, stdout, stderr := testRuntime(t)
	runtime.HTTP = manifestServer.Client()
	if err := runtime.Profiles.Add(config.Profile{Name: "local", BaseURL: businessServer.URL, TokenURL: businessServer.URL + "/token", AppID: "app", DefaultOutput: "json"}); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), runtime, []string{"checklist", "list", "--profile", "local", "--as", "app", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"code":"UPDATE_PENDING"`, `"currentVersion":"1.0.0"`, `"latestVersion":"1.1.0"`, `"updateCommand":"everyline-cli update"`, `"updateAfter":"current_business_workflow"`} {
		if !strings.Contains(stderr.String(), expected) {
			t.Fatalf("stderr=%s，缺少 %s", stderr.String(), expected)
		}
	}
	if businessCalls != 1 || !strings.Contains(stdout.String(), `[]`) {
		t.Fatalf("businessCalls=%d stdout=%s，当前业务必须正常完成", businessCalls, stdout.String())
	}
}

// TestBusinessDryRunSkipsVersionCheck 验证 dry-run 严格保持纯本地校验，不访问更新服务。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；发生网络调用或版本提示时通过 t.Fatal 报告。
func TestBusinessDryRunSkipsVersionCheck(t *testing.T) {
	originalVersion := build.Version
	build.Version = "1.0.0"
	t.Cleanup(func() { build.Version = originalVersion })
	manifestCalls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		manifestCalls++
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"version":"1.1.0","platforms":{"test":{"url":"https://updates.example.com/cli","sha256":"unused"}}}`))
	}))
	defer server.Close()
	t.Setenv("EVERYLINE_CLI_UPDATE_MANIFEST_URL", server.URL)
	runtime, _, stderr := testRuntime(t)
	runtime.HTTP = server.Client()
	payload := `{"businessId":"biz-1","fileId":12,"fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":"中立","matchContractTypeRulePackage":true}}`
	if err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--data", payload, "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if manifestCalls != 0 || strings.Contains(stderr.String(), "[version]") {
		t.Fatalf("manifestCalls=%d stderr=%s，dry-run 不应检查更新", manifestCalls, stderr.String())
	}
}

// TestConfigAddStoresOAuthConfiguration 验证自定义 Profile 可保存用户 OAuth 所需的非敏感配置。
func TestConfigAddStoresOAuthConfiguration(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	if err := Execute(context.Background(), runtime, []string{
		"config", "add", "oauth-dev",
		"--base-url", "https://api.example.com",
		"--token-url", "https://api.example.com/token",
		"--app-id", "cli-oauth",
		"--oauth-metadata-url", "https://auth.example.com/.well-known/oauth-authorization-server/contract-review",
		"--oauth-business-type", "contract-review",
		"--oauth-client-id", "oauth-client",
		"--oauth-redirect-url", "http://127.0.0.1:8000/login",
		"--oauth-scope", "contract-review:full",
	}); err != nil {
		t.Fatal(err)
	}
	profile, err := runtime.Profiles.Get("oauth-dev")
	if err != nil {
		t.Fatal(err)
	}
	if profile.OAuthMetadataURL == "" || profile.OAuthBusinessType != "contract-review" || profile.OAuthClientID != "oauth-client" || profile.OAuthRedirectURL != "http://127.0.0.1:8000/login" || strings.Join(profile.OAuthScopes, " ") != "contract-review:full" {
		t.Fatalf("profile=%#v", profile)
	}
}

// TestConfigAddUserProfileDoesNotRequireAppID 验证 user Profile 可以只配置 OAuth 和环境信息。
func TestConfigAddUserProfileDoesNotRequireAppID(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	if err := Execute(context.Background(), runtime, []string{
		"config", "add", "user-dev",
		"--env", "dev",
		"--default-identity", "user",
	}); err != nil {
		t.Fatal(err)
	}
	profile, err := runtime.Profiles.Get("user-dev")
	if err != nil {
		t.Fatal(err)
	}
	if profile.DefaultIdentity != config.IdentityUser || profile.AppID != "" {
		t.Fatalf("profile=%#v", profile)
	}
}

// TestConfigAddAppProfileStillRequiresAppID 验证 app Profile 仍然必须提供 app-id。
func TestConfigAddAppProfileStillRequiresAppID(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	err := Execute(context.Background(), runtime, []string{
		"config", "add", "app-dev",
		"--env", "dev",
		"--default-identity", "app",
	})
	if err == nil || !strings.Contains(err.Error(), "app-id 不能为空") {
		t.Fatalf("err=%v", err)
	}
}

// TestAuthUserUseAndStatus 验证已缓存的 OAuth user token 可切换为 Profile 默认身份并查询状态。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestAuthUserUseAndStatus(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	profile := config.Profile{Name: "dev", BaseURL: "https://api.example.com", AuthURL: "https://dev-contract-agent.qtech.cn", TokenURL: "https://api.example.com/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	identityStore, ok := runtime.Tokens.(interface {
		SaveForIdentity(string, config.IdentityKind, auth.Token) error
	})
	if !ok {
		t.Fatal("token store 不支持 user 身份")
	}
	if err := identityStore.SaveForIdentity("dev", config.IdentityUser, auth.Token{AccessToken: "oauth-user-token"}); err != nil {
		t.Fatal(err)
	}
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

// TestBusinessCallRevocationUpdatesAuthStatus 验证业务调用收到 110004 后，auth status 立即报告 user 未授权。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；退出码、提示或缓存状态不符合预期时通过 t.Fatal 报告。
func TestBusinessCallRevocationUpdatesAuthStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Request-Id", "req-revoked")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"code":110004,"msg":"token验证失败","data":null}`))
	}))
	defer server.Close()

	runtime, stdout, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	profile := config.Profile{
		Name: "test-user", BaseURL: server.URL, UserBaseURL: server.URL, AuthURL: "https://test-contract-agent.qtech.cn/login",
		TokenURL: server.URL + "/token", DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	identityStore, ok := runtime.Tokens.(interface {
		SaveForIdentity(string, config.IdentityKind, auth.Token) error
	})
	if !ok {
		t.Fatal("token store 不支持 user 身份")
	}
	if err := identityStore.SaveForIdentity(profile.Name, config.IdentityUser, auth.Token{AccessToken: "revoked-token", ExpiresAt: time.Now().Add(10 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}

	err := Execute(context.Background(), runtime, []string{"checklist", "list", "--profile", profile.Name, "--as", "user", "--output", "json"})
	if !errors.Is(err, auth.ErrUserSessionExpired) || ExitCode(err) != ExitAuth {
		t.Fatalf("err=%v exit=%d，期望 user 鉴权失效", err, ExitCode(err))
	}
	if !strings.Contains(err.Error(), "登录已失效；请重新执行 auth login --profile test-user --as user") || !strings.Contains(err.Error(), "req-revoked") {
		t.Fatalf("错误缺少重新授权提示或 request ID: %v", err)
	}

	stdout.Reset()
	if err := Execute(context.Background(), runtime, []string{"auth", "status", "--profile", profile.Name, "--as", "user", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"authenticated": false`) || !strings.Contains(stdout.String(), `"source": "cache"`) {
		t.Fatalf("status output=%s", stdout.String())
	}
}

// TestAuthUserLoginIgnoresLegacyEnvironmentToken 验证 user 登录不再接受原始 token 环境变量。
func TestAuthUserLoginIgnoresLegacyEnvironmentToken(t *testing.T) {
	t.Setenv("EVERYLINE_USER_ACCESS_TOKEN", "legacy-user-token")
	runtime, _, _ := testRuntime(t)
	profile := config.Profile{
		Name:            "dev",
		BaseURL:         "https://api.example.com",
		AuthURL:         "https://dev-contract-agent.qtech.cn",
		TokenURL:        "https://api.example.com/token",
		AppID:           "app",
		DefaultIdentity: config.IdentityUser,
		DefaultOutput:   "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	err := Execute(context.Background(), runtime, []string{"auth", "login", "--as", "user"})
	if err == nil || !strings.Contains(err.Error(), "OAuth") {
		t.Fatalf("err=%v", err)
	}
}

// TestAuthUserRejectsRemovedAccessTokenStdin 验证已删除的 user raw token flag 不再被命令接受。
func TestAuthUserRejectsRemovedAccessTokenStdin(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	profile := config.Profile{Name: "dev", BaseURL: "https://api.example.com", AuthURL: "https://dev-contract-agent.qtech.cn", TokenURL: "https://api.example.com/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	err := Execute(context.Background(), runtime, []string{"auth", "login", "--as", "user", "--access-token-stdin"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("err=%v", err)
	}
}

// TestAuthStatusIgnoresLegacyEnvironmentToken 验证 user 状态不再把原始 token 环境变量报告为已认证。
func TestAuthStatusIgnoresLegacyEnvironmentToken(t *testing.T) {
	t.Setenv("EVERYLINE_USER_ACCESS_TOKEN", "legacy-user-token")
	runtime, stdout, _ := testRuntime(t)
	profile := config.Profile{Name: "dev", BaseURL: "https://api.example.com", AuthURL: "https://dev-contract-agent.qtech.cn", TokenURL: "https://api.example.com/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), runtime, []string{"auth", "status", "--as", "user", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "legacy-user-token") || strings.Contains(stdout.String(), `"authenticated": true`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestAuthAppLoginAcceptsCredentialFlagsAndPersistsAppID 验证 app 登录支持直接传入 app-id/app-secret，且成功后保存非敏感 app-id。
func TestAuthAppLoginAcceptsCredentialFlagsAndPersistsAppID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["appId"] != "flag-app" || body["appSecret"] != "flag-secret" {
			t.Fatalf("token body=%#v", body)
		}
		_, _ = writer.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"flag-token","expire":7200}`))
	}))
	defer server.Close()

	runtime, stdout, stderr := testRuntime(t)
	runtime.HTTP = server.Client()
	profile := config.Profile{Name: "dev", BaseURL: server.URL, TokenURL: server.URL + "/token", AppID: "old-app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	requireFirstInstallAuthorization(t, runtime)

	if err := Execute(context.Background(), runtime, []string{
		"auth", "login", "--as", "app", "--app-id", "flag-app", "--app-secret", "flag-secret", "--output", "json",
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "flag-secret") || strings.Contains(stderr.String(), "flag-secret") {
		t.Fatalf("secret leaked to output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	updated, err := runtime.Profiles.Get("dev")
	if err != nil {
		t.Fatal(err)
	}
	if updated.AppID != "flag-app" {
		t.Fatalf("app id=%q, want flag-app", updated.AppID)
	}
	installState, err := runtime.InstallState.Load()
	if err != nil || installState.FirstInstall || installState.AuthorizationRequired {
		t.Fatalf("installState=%#v err=%v，app 新授权成功后应解除门禁", installState, err)
	}
	token, err := runtime.Tokens.Load("dev")
	if err != nil || token.AccessToken != "flag-token" {
		t.Fatalf("token=%#v err=%v", token, err)
	}
}

// TestAuthAppLoginResolvesProfileSpecificEnvironmentAppID 验证 app-id 支持 Profile 专用环境变量，并优先于 Profile 中的旧值。
func TestAuthAppLoginResolvesProfileSpecificEnvironmentAppID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["appId"] != "env-app" || body["appSecret"] != "env-secret" {
			t.Fatalf("token body=%#v", body)
		}
		_, _ = writer.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"env-token","expire":7200}`))
	}))
	defer server.Close()

	t.Setenv("EVERYLINE_APP_ID_DEV_2DEU", "env-app")
	t.Setenv("EVERYLINE_APP_SECRET_PROD_2DEU", "wrong-secret")
	t.Setenv("EVERYLINE_APP_SECRET_DEV_2DEU", "env-secret")
	runtime, _, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	profile := config.Profile{Name: "dev-eu", BaseURL: server.URL, TokenURL: server.URL + "/token", AppID: "old-app", DefaultIdentity: config.IdentityApp, DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}

	if err := Execute(context.Background(), runtime, []string{"auth", "login", "--as", "app"}); err != nil {
		t.Fatal(err)
	}
}

// TestAuthAppLoginRejectsConflictingSecretInputs 验证 app-secret flag 与 stdin 输入不能同时使用。
func TestAuthAppLoginRejectsConflictingSecretInputs(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	profile := config.Profile{Name: "dev", BaseURL: "https://api.example.com", TokenURL: "https://api.example.com/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	err := Execute(context.Background(), runtime, []string{
		"auth", "login", "--as", "app", "--app-secret", "flag-secret", "--app-secret-stdin",
	})
	if err == nil || !strings.Contains(err.Error(), "只能选择一个") {
		t.Fatalf("err=%v", err)
	}
}

// TestAuthAppLoginReadsSecretFromStdin 验证 WorkBuddy 用户终端命令的 stdin secret 可完成登录且不进入输出。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；secret 未传给认证服务或出现在 stdout/stderr 时通过 t.Fatal 报告。
func TestAuthAppLoginReadsSecretFromStdin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["appId"] != "workbuddy-app" || body["appSecret"] != "stdin-secret" {
			t.Fatalf("token body=%#v", body)
		}
		_, _ = writer.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"app-token","expire":7200}`))
	}))
	defer server.Close()

	runtime, stdout, stderr := testRuntime(t)
	runtime.Input = strings.NewReader("stdin-secret\n")
	runtime.HTTP = server.Client()
	if err := runtime.Profiles.Add(config.Profile{Name: "test", BaseURL: server.URL, TokenURL: server.URL + "/token", AppID: "old-app", DefaultOutput: "json"}); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), runtime, []string{
		"auth", "login", "--profile", "test", "--as", "app", "--app-id", "workbuddy-app", "--app-secret-stdin", "--output", "json",
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "stdin-secret") || strings.Contains(stderr.String(), "stdin-secret") {
		t.Fatalf("secret leaked: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"authenticated": true`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestAuthUserLoginRejectsAppCredentialFlags 验证 user 登录不会静默接受 app 专用参数。
func TestAuthUserLoginRejectsAppCredentialFlags(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	profile := config.Profile{Name: "dev", BaseURL: "https://api.example.com", AuthURL: "https://auth.example.com", TokenURL: "https://api.example.com/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	err := Execute(context.Background(), runtime, []string{"auth", "login", "--as", "user", "--app-id", "app"})
	if err == nil || !strings.Contains(err.Error(), "user 身份不能使用 app 凭证") {
		t.Fatalf("err=%v", err)
	}
}

// TestAuthUserLoginRequiresOAuthConfiguration 验证缺少 OAuth 配置时返回可操作的配置错误。
func TestAuthUserLoginRequiresOAuthConfiguration(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	profile := config.Profile{
		Name:            "dev",
		BaseURL:         "https://api.example.com",
		AuthURL:         "https://auth.example.com",
		TokenURL:        "https://api.example.com/token",
		AppID:           "app",
		DefaultIdentity: config.IdentityUser,
		DefaultOutput:   "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	err := Execute(context.Background(), runtime, []string{"auth", "login", "--as", "user"})
	if err == nil || !strings.Contains(err.Error(), "OAuth") || !strings.Contains(err.Error(), "https://auth.example.com") {
		t.Fatalf("err=%v", err)
	}
}

// TestAuthUserLoginUsesBrowserOAuth 验证 Codex user 登录会先动态注册浏览器 client，再完成 PKCE 授权且不覆盖 Device client。
func TestAuthUserLoginUsesBrowserOAuth(t *testing.T) {
	callbackListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	callbackPort := callbackListener.Addr().(*net.TCPAddr).Port
	_ = callbackListener.Close()

	var tokenRequest url.Values
	var registrationCalls int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/register/contract-review":
			registrationCalls++
			_, _ = writer.Write([]byte(`{"client_id":"dynamic-oauth-client","client_id_issued_at":1788480000,"client_secret_expires_at":0,"token_endpoint_auth_method":"none"}`))
		case "/metadata":
			_, _ = writer.Write([]byte(`{"authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"` + server.URL + `/token","registration_endpoint":"` + server.URL + `/oauth/register/contract-review","code_challenge_methods_supported":["S256"]}`))
		case "/token":
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			tokenRequest = request.Form
			_, _ = writer.Write([]byte(`{"access_token":"oauth-token","token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-token","scope":"contract-review:full"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	runtime, stdout, stderr := testRuntime(t)
	runtime.HTTP = server.Client()
	runtime.OpenBrowser = func(authorizationURL string) error {
		parsed, err := url.Parse(authorizationURL)
		if err != nil {
			return err
		}
		redirectURL, err := url.Parse(parsed.Query().Get("redirect_uri"))
		if err != nil {
			return err
		}
		query := redirectURL.Query()
		query.Set("code", "authorization-code")
		query.Set("state", parsed.Query().Get("state"))
		redirectURL.RawQuery = query.Encode()
		go func() {
			response, requestErr := http.Get(redirectURL.String())
			if requestErr == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}
	profile := config.Profile{
		Name:                "dev",
		BaseURL:             server.URL,
		AuthURL:             "https://auth.example.com",
		TokenURL:            server.URL + "/tenant-token",
		OAuthMetadataURL:    server.URL + "/metadata",
		OAuthBusinessType:   "contract-review",
		OAuthClientID:       "stale-oauth-client",
		OAuthDeviceClientID: "existing-device-client",
		OAuthRedirectURL:    fmt.Sprintf("http://127.0.0.1:%d/login", callbackPort),
		OAuthScopes:         []string{"contract-review:full"},
		DefaultIdentity:     config.IdentityUser,
		DefaultOutput:       "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}

	if err := Execute(context.Background(), runtime, []string{"auth", "login", "--as", "user"}); err != nil {
		t.Fatal(err)
	}
	if registrationCalls != 1 || tokenRequest.Get("code") != "authorization-code" || tokenRequest.Get("client_id") != "dynamic-oauth-client" || tokenRequest.Get("grant_type") != "authorization_code" {
		t.Fatalf("token request=%v", tokenRequest)
	}
	if tokenRequest.Get("code_verifier") == "" || tokenRequest.Get("redirect_uri") != profile.OAuthRedirectURL {
		t.Fatalf("token request=%v", tokenRequest)
	}
	if !strings.Contains(stderr.String(), "请在浏览器中完成用户授权") || !strings.Contains(stderr.String(), "code_challenge") {
		t.Fatalf("stderr=%s", stderr.String())
	}
	if strings.Contains(stdout.String(), "oauth-token") || strings.Contains(stderr.String(), "oauth-token") {
		t.Fatalf("token leaked: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	identityStore := runtime.Tokens.(interface {
		LoadForIdentity(string, config.IdentityKind) (auth.Token, error)
	})
	cached, err := identityStore.LoadForIdentity("dev", config.IdentityUser)
	if err != nil || cached.AccessToken != "oauth-token" || cached.RefreshToken != "refresh-token" || cached.OAuthClientID != "dynamic-oauth-client" {
		t.Fatalf("cached=%#v err=%v", cached, err)
	}
	storedProfile, err := runtime.Profiles.Get(profile.Name)
	if err != nil || storedProfile.OAuthClientID != "dynamic-oauth-client" || storedProfile.OAuthDeviceClientID != "existing-device-client" {
		t.Fatalf("storedProfile=%#v err=%v", storedProfile, err)
	}
}

// TestAuthUserLoginFailurePreservesOAuthClientAndToken 验证动态注册后的授权失败不会让旧 Profile 与旧 token 失配。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败登录覆盖任一旧凭证关联时通过 t.Fatal 报告。
func TestAuthUserLoginFailurePreservesOAuthClientAndToken(t *testing.T) {
	metadataCalls := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/register/contract-review":
			_, _ = writer.Write([]byte(`{"client_id":"new-oauth-client","token_endpoint_auth_method":"none"}`))
		case "/metadata":
			metadataCalls++
			if metadataCalls > 1 {
				http.Error(writer, "authorization unavailable", http.StatusServiceUnavailable)
				return
			}
			_, _ = writer.Write([]byte(`{"authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"` + server.URL + `/token","registration_endpoint":"` + server.URL + `/oauth/register/contract-review","code_challenge_methods_supported":["S256"]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	runtime, _, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	profile := config.Profile{
		Name: "dev", BaseURL: server.URL, TokenURL: server.URL + "/tenant-token",
		OAuthMetadataURL: server.URL + "/metadata", OAuthBusinessType: "contract-review",
		OAuthClientID: "old-oauth-client", OAuthRedirectURL: "http://127.0.0.1:8000/login",
		OAuthScopes: []string{"contract-review:full"}, DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	identityStore := runtime.Tokens.(interface {
		LoadForIdentity(string, config.IdentityKind) (auth.Token, error)
		SaveForIdentity(string, config.IdentityKind, auth.Token) error
	})
	oldToken := auth.Token{
		AccessToken: "old-access-token", RefreshToken: "old-refresh-token", OAuthClientID: profile.OAuthClientID,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := identityStore.SaveForIdentity(profile.Name, config.IdentityUser, oldToken); err != nil {
		t.Fatal(err)
	}

	err := Execute(context.Background(), runtime, []string{"auth", "login", "--profile", profile.Name, "--as", "user"})
	if err == nil || !strings.Contains(err.Error(), "获取 OAuth metadata 失败") {
		t.Fatalf("err=%v", err)
	}
	storedProfile, profileErr := runtime.Profiles.Get(profile.Name)
	if profileErr != nil || storedProfile.OAuthClientID != profile.OAuthClientID {
		t.Fatalf("storedProfile=%#v err=%v", storedProfile, profileErr)
	}
	storedToken, tokenErr := identityStore.LoadForIdentity(profile.Name, config.IdentityUser)
	if tokenErr != nil || storedToken.AccessToken != oldToken.AccessToken || storedToken.OAuthClientID != oldToken.OAuthClientID {
		t.Fatalf("storedToken=%#v err=%v", storedToken, tokenErr)
	}
}

/*
TestAuthUserLoginDeniedPreservesOAuthClientAndToken 验证动态注册后拒绝授权会显示明确结果，并保留既有登录凭证。
入参：t *testing.T 为测试上下文。
返回值：无；回调响应、CLI 错误、token 兑换次数或原有凭证与预期不符时通过测试失败报告。
*/
func TestAuthUserLoginDeniedPreservesOAuthClientAndToken(t *testing.T) {
	callbackListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	callbackPort := callbackListener.Addr().(*net.TCPAddr).Port
	_ = callbackListener.Close()

	// 请求计数由 HTTP handler 写入，使用原子变量保证跨 goroutine 的检查可靠。
	var registrationCalls, tokenCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/register":
			registrationCalls.Add(1)
			_, _ = writer.Write([]byte(`{"client_id":"new-oauth-client","token_endpoint_auth_method":"none"}`))
		case "/metadata":
			_, _ = writer.Write([]byte(`{"authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"` + server.URL + `/token","registration_endpoint":"` + server.URL + `/register","code_challenge_methods_supported":["S256"]}`))
		case "/token":
			tokenCalls.Add(1)
			http.Error(writer, "unexpected token exchange", http.StatusBadRequest)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	runtime, stdout, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	// 浏览器独立读取完整响应，覆盖 CLI 收到拒绝后关闭回调服务的并发路径。
	browserResults := make(chan struct {
		status int
		body   string
		err    error
	}, 1)
	runtime.OpenBrowser = func(authorizationURL string) error {
		parsed, err := url.Parse(authorizationURL)
		if err != nil {
			return err
		}
		redirectURL, err := url.Parse(parsed.Query().Get("redirect_uri"))
		if err != nil {
			return err
		}
		// 复现服务端拒绝分支只携带 error 和 error_description、没有 state 的回调。
		redirectURL.RawQuery = url.Values{
			"error": {"access_denied"}, "error_description": {"用户拒绝授权"},
		}.Encode()
		go func() {
			result := struct {
				status int
				body   string
				err    error
			}{}
			client := &http.Client{Timeout: 2 * time.Second}
			response, err := client.Get(redirectURL.String())
			if err != nil {
				result.err = err
			} else {
				defer response.Body.Close()
				body, readErr := io.ReadAll(response.Body)
				result.status, result.body, result.err = response.StatusCode, string(body), readErr
			}
			browserResults <- result
		}()
		return nil
	}
	profile := config.Profile{
		Name: "dev", BaseURL: server.URL, TokenURL: server.URL + "/tenant-token",
		OAuthMetadataURL: server.URL + "/metadata", OAuthBusinessType: "contract-review",
		OAuthClientID: "old-oauth-client", OAuthDeviceClientID: "existing-device-client",
		OAuthRedirectURL: fmt.Sprintf("http://127.0.0.1:%d/login", callbackPort),
		OAuthScopes:      []string{"contract-review:full"}, DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	identityStore := runtime.Tokens.(interface {
		LoadForIdentity(string, config.IdentityKind) (auth.Token, error)
		SaveForIdentity(string, config.IdentityKind, auth.Token) error
	})
	oldToken := auth.Token{
		AccessToken: "old-access-token", RefreshToken: "old-refresh-token", OAuthClientID: profile.OAuthClientID,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := identityStore.SaveForIdentity(profile.Name, config.IdentityUser, oldToken); err != nil {
		t.Fatal(err)
	}

	// 拒绝必须结束本次登录；上下文仅用于防止错误实现一直等待授权超时。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = Execute(ctx, runtime, []string{"auth", "login", "--profile", profile.Name, "--as", "user"})
	if err == nil || !strings.Contains(err.Error(), "access_denied") || !strings.Contains(err.Error(), "用户拒绝授权") {
		t.Errorf("err=%v", err)
	}
	if ctx.Err() != nil {
		t.Errorf("拒绝授权应立即结束登录: %v", ctx.Err())
	}
	select {
	case result := <-browserResults:
		if result.err != nil || result.status != http.StatusForbidden || !strings.Contains(result.body, "用户已拒绝授权") || strings.Contains(result.body, "授权已完成") {
			t.Errorf("browser status=%d body=%q err=%v", result.status, result.body, result.err)
		}
	case <-ctx.Done():
		t.Fatal("浏览器未收到完整拒绝响应")
	}
	if registrationCalls.Load() != 1 || tokenCalls.Load() != 0 {
		t.Errorf("registration calls=%d token calls=%d", registrationCalls.Load(), tokenCalls.Load())
	}
	if strings.Contains(stdout.String(), `"authenticated": true`) || strings.Contains(stdout.String(), `"authenticated":true`) {
		t.Errorf("拒绝授权后输出了成功结果: %s", stdout.String())
	}
	storedProfile, profileErr := runtime.Profiles.Get(profile.Name)
	if profileErr != nil || storedProfile.OAuthClientID != profile.OAuthClientID || storedProfile.OAuthDeviceClientID != profile.OAuthDeviceClientID {
		t.Errorf("storedProfile=%#v err=%v", storedProfile, profileErr)
	}
	storedToken, tokenErr := identityStore.LoadForIdentity(profile.Name, config.IdentityUser)
	if tokenErr != nil || storedToken.AccessToken != oldToken.AccessToken || storedToken.RefreshToken != oldToken.RefreshToken || storedToken.OAuthClientID != oldToken.OAuthClientID || !storedToken.ExpiresAt.Equal(oldToken.ExpiresAt) {
		t.Errorf("storedToken=%#v err=%v", storedToken, tokenErr)
	}
}

// TestAuthUserLoginRequiresMetadataRegistrationEndpoint 验证 Codex 登录不再猜测开放平台动态注册路径。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；metadata 缺少 registration_endpoint 时未明确停止则通过 t.Fatal 报告。
func TestAuthUserLoginRequiresMetadataRegistrationEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/metadata" {
			_, _ = writer.Write([]byte(`{"authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token","code_challenge_methods_supported":["S256"]}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	runtime, _, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	profile := config.Profile{
		Name: "test-user", BaseURL: server.URL, TokenURL: server.URL + "/tenant-token",
		OAuthMetadataURL: server.URL + "/metadata", OAuthBusinessType: "contract-review",
		OAuthClientID: "stale-client", OAuthRedirectURL: "http://127.0.0.1:8000/login",
		OAuthScopes: []string{"contract-review:full"}, DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}

	err := Execute(context.Background(), runtime, []string{"auth", "login", "--profile", profile.Name, "--as", "user"})
	if err == nil || !strings.Contains(err.Error(), "OAuth metadata 缺少 registration_endpoint") {
		t.Fatalf("err=%v", err)
	}
}

// TestAuthAppLoginFailureDoesNotPersistFlagAppID 验证 token 交换失败时不污染 Profile 中原有 app-id。
func TestAuthAppLoginFailureDoesNotPersistFlagAppID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"code":999,"msg":"invalid app"}`))
	}))
	defer server.Close()

	runtime, _, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	profile := config.Profile{Name: "dev", BaseURL: server.URL, TokenURL: server.URL + "/token", AppID: "old-app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	err := Execute(context.Background(), runtime, []string{
		"auth", "login", "--as", "app", "--app-id", "new-app", "--app-secret", "secret",
	})
	if err == nil || !strings.Contains(err.Error(), "code=999") {
		t.Fatalf("err=%v", err)
	}
	unchanged, err := runtime.Profiles.Get("dev")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.AppID != "old-app" {
		t.Fatalf("app id=%q, want old-app", unchanged.AppID)
	}
}

// TestAuthAppLoginExplicitlySavesSecret 验证只有显式 --save-app-secret 且授权成功后才写入本地 secret 文件。
func TestAuthAppLoginExplicitlySavesSecret(t *testing.T) {
	t.Setenv("EVERYLINE_APP_SECRET", "")
	t.Setenv("EVERYLINE_APP_SECRET_DEV_2DEU", "")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["appId"] != "app" || body["appSecret"] != "saved-secret" {
			t.Fatalf("token body=%#v", body)
		}
		_, _ = writer.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"saved-token","expire":7200}`))
	}))
	defer server.Close()

	directory := t.TempDir()
	runtime := NewRuntime(directory, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	runtime.Secrets = auth.NewFileSecretStore(filepath.Join(directory, "secrets.json"))
	runtime.HTTP = server.Client()
	profile := config.Profile{Name: "dev", BaseURL: server.URL, TokenURL: server.URL + "/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}

	stdout := runtime.Output.(*bytes.Buffer)
	stderr := runtime.Error.(*bytes.Buffer)
	if err := Execute(context.Background(), runtime, []string{
		"auth", "login", "--as", "app", "--app-secret", "saved-secret", "--save-app-secret", "--output", "json",
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "saved-secret") || strings.Contains(stderr.String(), "saved-secret") {
		t.Fatalf("secret leaked to output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	content, err := os.ReadFile(filepath.Join(directory, "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		AppSecrets map[string]string `json:"app_secrets"`
	}
	if err := json.Unmarshal(content, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.AppSecrets["dev"] != "saved-secret" {
		t.Fatalf("stored secrets=%#v", stored.AppSecrets)
	}
	secretInfo, err := os.Stat(filepath.Join(directory, "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if secretInfo.Mode().Perm() != 0o600 {
		t.Fatalf("secret file mode=%o, want 600", secretInfo.Mode().Perm())
	}
	if configInfo, err := os.Stat(directory); err != nil {
		t.Fatal(err)
	} else if configInfo.Mode().Perm() != 0o700 {
		t.Fatalf("config directory mode=%o, want 700", configInfo.Mode().Perm())
	}
	for _, path := range []string{"config.json", "tokens.json"} {
		content, readErr := os.ReadFile(filepath.Join(directory, path))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(content), "saved-secret") {
			t.Fatalf("secret leaked to %s: %s", path, content)
		}
	}
}

// TestAuthAppLoginUsesSavedSecret 验证没有当前进程凭证时，app 登录会复用当前 Profile 的本地 secret。
func TestAuthAppLoginUsesSavedSecret(t *testing.T) {
	t.Setenv("EVERYLINE_APP_SECRET", "")
	t.Setenv("EVERYLINE_APP_SECRET_DEV_2DEU", "")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["appSecret"] != "stored-secret" {
			t.Fatalf("token body=%#v", body)
		}
		_, _ = writer.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"stored-token","expire":7200}`))
	}))
	defer server.Close()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "secrets.json"), []byte(`{"app_secrets":{"dev":"stored-secret"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(directory, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	runtime.Secrets = auth.NewFileSecretStore(filepath.Join(directory, "secrets.json"))
	runtime.HTTP = server.Client()
	profile := config.Profile{Name: "dev", BaseURL: server.URL, TokenURL: server.URL + "/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}

	if err := Execute(context.Background(), runtime, []string{"auth", "login", "--as", "app"}); err != nil {
		t.Fatal(err)
	}
}

// TestAuthAppLoginFailureDoesNotSaveSecret 验证 token endpoint 失败时 --save-app-secret 不创建本地 secret 文件。
func TestAuthAppLoginFailureDoesNotSaveSecret(t *testing.T) {
	t.Setenv("EVERYLINE_APP_SECRET", "")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"code":999,"msg":"invalid app"}`))
	}))
	defer server.Close()

	directory := t.TempDir()
	runtime := NewRuntime(directory, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	runtime.Secrets = auth.NewFileSecretStore(filepath.Join(directory, "secrets.json"))
	runtime.HTTP = server.Client()
	profile := config.Profile{Name: "dev", BaseURL: server.URL, TokenURL: server.URL + "/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}

	err := Execute(context.Background(), runtime, []string{
		"auth", "login", "--as", "app", "--app-secret", "new-secret", "--save-app-secret",
	})
	if err == nil || !strings.Contains(err.Error(), "code=999") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "secrets.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("secret file should not exist, stat err=%v", err)
	}
}

// TestAuthUserLoginRejectsSaveAppSecret 验证 user 身份不能使用 app secret 持久化开关，也不会发起网络请求。
func TestAuthUserLoginRejectsSaveAppSecret(t *testing.T) {
	directory := t.TempDir()
	runtime := NewRuntime(directory, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	runtime.Secrets = auth.NewFileSecretStore(filepath.Join(directory, "secrets.json"))
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	runtime.HTTP = server.Client()
	profile := config.Profile{Name: "dev", BaseURL: "https://api.example.com", AuthURL: "https://auth.example.com", TokenURL: "https://api.example.com/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}

	err := Execute(context.Background(), runtime, []string{"auth", "login", "--as", "user", "--save-app-secret"})
	if err == nil || !strings.Contains(err.Error(), "user 身份") {
		t.Fatalf("err=%v", err)
	}
	if requestCount != 0 {
		t.Fatalf("unexpected request count=%d", requestCount)
	}
	if _, err := os.Stat(filepath.Join(directory, "secrets.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("secret file should not exist, stat err=%v", err)
	}
}

// TestAuthStatusReportsSavedSecretWithoutValue 验证 status 只报告本地 secret 是否存在，不泄漏 secret 内容。
func TestAuthStatusReportsSavedSecretWithoutValue(t *testing.T) {
	t.Setenv("EVERYLINE_APP_SECRET", "")
	t.Setenv("EVERYLINE_APP_SECRET_DEV_2DEU", "")
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "secrets.json"), []byte(`{"app_secrets":{"dev":"status-secret"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(directory, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	runtime.Secrets = auth.NewFileSecretStore(filepath.Join(directory, "secrets.json"))
	profile := config.Profile{Name: "dev", BaseURL: "https://api.example.com", TokenURL: "https://api.example.com/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}

	if err := Execute(context.Background(), runtime, []string{"auth", "status", "--as", "app", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	stdout := runtime.Output.(*bytes.Buffer)
	if !strings.Contains(stdout.String(), `"appSecretConfigured": true`) {
		t.Fatalf("status output=%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "status-secret") {
		t.Fatalf("secret leaked to status: %s", stdout.String())
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

// TestAuthStatusUnknownExpiryDoesNotRenderFakeTime 验证未知过期的 token 不输出零时间或虚构剩余时长。
func TestAuthStatusUnknownExpiryDoesNotRenderFakeTime(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	profile := config.Profile{Name: "dev", BaseURL: "https://api.example.com", TokenURL: "https://api.example.com/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	if identityStore, ok := runtime.Tokens.(interface {
		SaveForIdentity(string, config.IdentityKind, auth.Token) error
	}); ok {
		if err := identityStore.SaveForIdentity("dev", config.IdentityUser, auth.Token{AccessToken: "user-token"}); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal("test token store 不支持 user 身份")
	}
	if err := Execute(context.Background(), runtime, []string{"auth", "status", "--as", "user", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"authenticated": true`) || !strings.Contains(stdout.String(), `"expiresKnown": false`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "expiresAt") || strings.Contains(stdout.String(), "expiresInSeconds") || strings.Contains(stdout.String(), "0001-01-01") {
		t.Fatalf("stdout contains fake expiry: %s", stdout.String())
	}
}

// TestAuthStatusKnownExpiryReportsServerTime 验证已知过期 token 的 status 输出服务端生命周期和真实剩余时间。
func TestAuthStatusKnownExpiryReportsServerTime(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	fixedNow := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	runtime.Now = func() time.Time { return fixedNow }
	profile := config.Profile{Name: "dev", BaseURL: "https://api.example.com", TokenURL: "https://api.example.com/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	identityStore, ok := runtime.Tokens.(interface {
		SaveForIdentity(string, config.IdentityKind, auth.Token) error
	})
	if !ok {
		t.Fatal("test token store 不支持 user 身份")
	}
	if err := identityStore.SaveForIdentity("dev", config.IdentityUser, auth.Token{
		AccessToken: "user-token",
		ExpiresAt:   fixedNow.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), runtime, []string{"auth", "status", "--as", "user", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"authenticated": true`, `"expiresKnown": true`, `"expiresInSeconds": 7200`, `"2026-08-20T12:00:00Z"`} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("stdout 缺少 %q: %s", expected, stdout.String())
		}
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

// loginDeadlineTransport 为登录测试检查实际 HTTP 请求所携带的上下文。
type loginDeadlineTransport func(*http.Request) (*http.Response, error)

/*
RoundTrip 将请求交给测试回调，避免为超时预算验证等待真实的三分钟。
入参：request *http.Request 为 CLI 发出的请求。
返回值：*http.Response 为模拟响应；error 为回调返回的请求错误。
*/
func (transport loginDeadlineTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

/*
TestUserLoginWaitingBudget 验证浏览器登录默认有三分钟预算，显式 --timeout 仍生效且不改变动态注册预算。
入参：t *testing.T 为测试上下文。
返回值：无；登录与注册实际请求的 deadline 不符合预期时测试失败。
*/
func TestUserLoginWaitingBudget(t *testing.T) {
	for _, scenario := range []struct {
		name                            string
		flags                           []string
		loginBudget, registrationBudget time.Duration
	}{
		{"default", nil, 3 * time.Minute, 30 * time.Second},
		{"explicit", []string{"--timeout", "2m"}, 2 * time.Minute, 2 * time.Minute},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runtime, _, _ := testRuntime(t)
			stop := errors.New("已检查登录预算")
			metadataCalls := 0
			runtime.HTTP = &http.Client{Transport: loginDeadlineTransport(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/metadata" {
					metadataCalls++
				}
				budget := scenario.registrationBudget
				if metadataCalls == 2 {
					budget = scenario.loginBudget
				}
				deadline, ok := request.Context().Deadline()
				remaining := time.Until(deadline)
				if !ok || remaining > budget || remaining < budget-time.Second {
					t.Errorf("path=%s remaining=%s，期望预算 %s", request.URL.Path, remaining, budget)
				}
				// LoginUserOAuth 的 metadata 与 callback 共用此上下文；检查后立即结束，不占用回调端口。
				if metadataCalls == 2 {
					return nil, stop
				}
				body := `{"client_id":"fixture-client"}`
				if request.URL.Path == "/metadata" {
					body = `{"registration_endpoint":"https://auth.example.com/register","authorization_endpoint":"https://auth.example.com/authorize","token_endpoint":"https://auth.example.com/token"}`
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			profile := config.Profile{
				Name: "test-user", BaseURL: "https://api.example.com", TokenURL: "https://auth.example.com/token",
				OAuthMetadataURL: "https://auth.example.com/metadata", OAuthBusinessType: "contract-review",
				OAuthRedirectURL: "http://127.0.0.1:8000/login", DefaultIdentity: config.IdentityUser,
			}
			if err := runtime.Profiles.Add(profile); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"auth", "login", "--profile", profile.Name, "--as", "user", "--no-open-browser"}, scenario.flags...)
			if err := Execute(context.Background(), runtime, args); !errors.Is(err, stop) || metadataCalls != 2 {
				t.Fatalf("metadataCalls=%d err=%v", metadataCalls, err)
			}
		})
	}
}

// TestReviewStartDryRun 验证严格 JSON 在无 Profile 时也能完成 dry-run，且不会调用远端。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestReviewStartDryRun(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	payload := `{"businessId":"biz-1","fileId":12,"fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":"中立","selectedCheckListIds":["2001001"],"matchContractTypeRulePackage":true}}`
	err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--data", payload, "--dry-run", "--output", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"fileId": "12"`) || !strings.Contains(stdout.String(), `"selectedPosition": "xxx公司"`) || !strings.Contains(stdout.String(), `"reviewStrength": 1`) || !strings.Contains(stdout.String(), `"selectedCheckListIds"`) || !strings.Contains(stdout.String(), `"matchContractTypeRulePackage": true`) || !strings.Contains(stdout.String(), `"reportBusinessCode": "everyLine_100_openApi_cli"`) || strings.Contains(stdout.String(), `"appType"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestReviewStartDryRunAcceptsLegacyNumericStrength 验证旧版数字强度输入仍可通过 CLI 校验并发往相同后端枚举。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；兼容输入被拒绝或输出枚举漂移时通过 t.Fatal 报告。
func TestReviewStartDryRunAcceptsLegacyNumericStrength(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	payload := `{"businessId":"biz-1","fileId":12,"fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":1,"matchContractTypeRulePackage":true}}`
	if err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--data", payload, "--dry-run", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"reviewStrength": 1`) {
		t.Fatalf("stdout=%s，旧数字强度应输出相同后端枚举", stdout.String())
	}
}

// TestReviewStartDryRunRejectsMissingRuleSource 验证规则来源全部缺失时 dry-run 在本地以用法错误阻断。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；未阻断、错误不明确或退出码不是 2 时通过 t.Fatal 报告。
func TestReviewStartDryRunRejectsMissingRuleSource(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	payload := `{"businessId":"biz-1","fileId":12,"fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":"中立","matchContractTypeRulePackage":false}}`
	err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--data", payload, "--dry-run"})
	if !errors.Is(err, review.ErrReviewRuleSourceRequired) || ExitCode(err) != ExitUsage {
		t.Fatalf("err=%v exit=%d", err, ExitCode(err))
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%s，失败校验不应输出请求", stdout.String())
	}
}

// TestReviewStartRejectsRemovedOptionalField 验证 start CLI 契约不接受未纳入 CLI 的可选字段。
func TestReviewStartRejectsRemovedOptionalField(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	payload := `{"businessId":"biz-1","fileId":12,"fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":1},"appType":"THIRD_PARTY"}`
	err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--data", payload, "--dry-run", "--output", "json"})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("err=%v stdout=%s", err, stdout.String())
	}
	if ExitCode(err) != ExitUsage {
		t.Fatalf("exit=%d err=%v", ExitCode(err), err)
	}
}

// TestReviewStartRequiresConfigFields 验证 start CLI 要求 API 文档定义的 config 必填字段。
func TestReviewStartRequiresConfigFields(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	payload := `{"businessId":"biz-1","fileId":12,"fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","config":{}}`
	err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--data", payload, "--dry-run"})
	if err == nil || !strings.Contains(err.Error(), "selectedPosition") {
		t.Fatalf("err=%v", err)
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
	if !strings.Contains(stdout.String(), `"fileId": "12"`) || strings.Contains(stdout.String(), `"appType"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestReviewFileUploadDryRunOmitsOptionalAppType 验证本地上传不再强制输出 appType。
func TestReviewFileUploadDryRunOmitsOptionalAppType(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	filePath := filepath.Join(t.TempDir(), "contract.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), runtime, []string{
		"review", "file", "upload", "--file", filePath, "--name", "合同.pdf", "--dry-run", "--output", "json",
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), `"appType"`) || strings.Contains(stdout.String(), `"businessId"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestReviewFileUploadDryRunAcceptsStdinSource 验证沙箱可选择 stdin 且不再被 --file 必填规则阻断。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；参数互斥或规范化输出错误时通过 t.Fatal 报告。
func TestReviewFileUploadDryRunAcceptsStdinSource(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	runtime.Input = strings.NewReader("sandbox document")
	if err := Execute(context.Background(), runtime, []string{
		"review", "file", "upload", "--stdin", "--name", "合同.docx", "--dry-run",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"file": "stdin"`) || !strings.Contains(stdout.String(), `"name": "合同.docx"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
	stdout.Reset()
	err := Execute(context.Background(), runtime, []string{
		"review", "file", "upload", "--stdin", "--file", "contract.docx", "--name", "合同.docx", "--dry-run",
	})
	if err == nil || !strings.Contains(err.Error(), "只能选择一个") {
		t.Fatalf("err=%v", err)
	}
}

// TestReviewFileUploadRejectsRemovedAppTypeFlag 验证旧 app-type flag 在参数解析阶段即被拒绝。
func TestReviewFileUploadRejectsRemovedAppTypeFlag(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	err := Execute(context.Background(), runtime, []string{
		"review", "file", "upload", "--file", "missing.pdf", "--name", "合同.pdf", "--app-type", "CLM",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("err=%v", err)
	}
}

// TestReviewFileUploadRejectsRemovedBusinessIDFlag 验证上传命令已删除的 business-id flag 在参数解析阶段被拒绝。
func TestReviewFileUploadRejectsRemovedBusinessIDFlag(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	err := Execute(context.Background(), runtime, []string{
		"review", "file", "upload", "--file", "missing.pdf", "--name", "合同.pdf", "--business-id", "biz-1",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("err=%v", err)
	}
}

// TestReviewStartRejectsUnknownField 验证命令处理器不会猜测未声明字段。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestReviewStartRejectsUnknownField(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	payload := `{"businessId":"biz-1","fileId":12,"fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":1},"typo":true}`
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
	payload := `{"businessId":"biz-1","fileId":12,"fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":1},"usageReportContext":{}}`
	err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--data", payload, "--dry-run"})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("err=%v", err)
	}
}

// TestReviewRunURLDryRunUsesUploadIdentity 验证 URL 工作流无需调用方预填上传响应身份。
func TestReviewRunURLDryRunUsesUploadIdentity(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	payload := `{"source":{"type":"url","fileUrl":"https://files.example.com/contract.pdf","name":"合同.pdf"},"config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":1,"selectedCheckListIds":["2001001"],"matchContractTypeRulePackage":true}}`
	if err := Execute(context.Background(), runtime, []string{"review", "run", "--data", payload, "--dry-run", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), `"businessId"`) || strings.Contains(stdout.String(), `"fileHash"`) || !strings.Contains(stdout.String(), `"selectedAuditRole": "甲方"`) || !strings.Contains(stdout.String(), `"reviewStrength": 1`) || !strings.Contains(stdout.String(), `"selectedCheckListIds"`) || !strings.Contains(stdout.String(), `"matchContractTypeRulePackage": true`) || strings.Contains(stdout.String(), `"appType"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

// TestReviewRunDryRunRejectsMissingRuleSource 验证一键工作流和 task start 使用同一规则来源约束。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；缺失规则来源未在 dry-run 阶段阻断时通过 t.Fatal 报告。
func TestReviewRunDryRunRejectsMissingRuleSource(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	filePath := filepath.Join(t.TempDir(), "contract.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := `{"source":{"type":"file","path":"` + filePath + `","name":"合同.pdf"},"config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":"中立"}}`
	err := Execute(context.Background(), runtime, []string{"review", "run", "--data", payload, "--dry-run"})
	if !errors.Is(err, review.ErrReviewRuleSourceRequired) || ExitCode(err) != ExitUsage {
		t.Fatalf("err=%v exit=%d", err, ExitCode(err))
	}
}

// TestReviewRunRejectsRemovedOptionalField 验证 review run 不接受已排除的发起审查可选字段。
func TestReviewRunRejectsRemovedOptionalField(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	payload := `{"source":{"type":"url","fileUrl":"https://files.example.com/contract.pdf","name":"合同.pdf"},"businessId":"biz-url","fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":1},"appType":"THIRD_PARTY"}`
	err := Execute(context.Background(), runtime, []string{"review", "run", "--data", payload, "--dry-run"})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("err=%v", err)
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

// TestRemainingBatchWritesCallRemote 验证清单批量创建、清单批量更新和规则批量更新均发送一次真实数组请求。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；任一命令未请求预期 method/path/body 或未透传响应 data 时通过 t.Fatal 报告。
func TestRemainingBatchWritesCallRemote(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		args   []string
	}{
		{
			name:   "checklist-batch-create",
			method: http.MethodPost,
			path:   "/open-apis/review-rules/review-checklists/batch",
			args:   []string{"checklist", "batch-create", "--data", `[{"name":"清单","reviewRuleIds":["rule-1"]}]`, "--output", "json"},
		},
		{
			name:   "checklist-batch-update",
			method: http.MethodPut,
			path:   "/open-apis/review-rules/review-checklists/batch",
			args:   []string{"checklist", "batch-update", "--data", `[{"id":"check-1","name":"清单","reviewRuleIds":["rule-1"]}]`, "--output", "json"},
		},
		{
			name:   "rule-batch-update",
			method: http.MethodPut,
			path:   "/open-apis/review-rules/review-rule-groups/group-1/rules/batch",
			args:   []string{"rule", "batch-update", "--group-id", "group-1", "--data", `[{"id":"rule-1","name":"规则","riskLevel":1,"content":"内容"}]`, "--output", "json"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var received []map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != test.method || request.URL.Path != test.path {
					t.Errorf("request=%s %s", request.Method, request.URL.Path)
				}
				if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
					t.Errorf("decode body: %v", err)
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":[{"id":"result-1"}]}`))
			}))
			defer server.Close()

			t.Setenv("EVERYLINE_ACCESS_TOKEN", "test-token")
			runtime, stdout, _ := testRuntime(t)
			runtime.HTTP = server.Client()
			if err := runtime.Profiles.Add(config.Profile{
				Name:            "local",
				BaseURL:         server.URL,
				TokenURL:        server.URL + "/token",
				AppID:           "app",
				DefaultIdentity: config.IdentityApp,
				DefaultOutput:   "json",
			}); err != nil {
				t.Fatal(err)
			}
			if err := Execute(context.Background(), runtime, test.args); err != nil {
				t.Fatal(err)
			}
			if len(received) != 1 || !strings.Contains(stdout.String(), `"id": "result-1"`) {
				t.Fatalf("received=%#v stdout=%s", received, stdout.String())
			}
		})
	}
}

// TestRuleBatchCreateCallsRemote 验证规则批量创建通过一次 POST 请求发送规则数组，并输出服务端 data。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestRuleBatchCreateCallsRemote(t *testing.T) {
	var received []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/open-apis/review-rules/review-rule-groups/group-1/rules/batch" {
			t.Errorf("request=%s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("authorization=%q", request.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode body: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":[{"id":"rule-1","name":"付款期限"}]}`))
	}))
	defer server.Close()

	t.Setenv("EVERYLINE_ACCESS_TOKEN", "test-token")
	runtime, stdout, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	profile := config.Profile{
		Name:            "local",
		BaseURL:         server.URL,
		TokenURL:        server.URL + "/token",
		AppID:           "app",
		DefaultIdentity: config.IdentityApp,
		DefaultOutput:   "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}

	payload := `[{"name":"付款期限","riskLevel":2,"content":"付款期限不得超过 60 天"},{"name":"违约责任","riskLevel":1,"content":"检查违约责任是否对等"}]`
	if err := Execute(context.Background(), runtime, []string{
		"rule", "batch-create", "--group-id", "group-1", "--data", payload, "--output", "json",
	}); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 || received[0]["name"] != "付款期限" || received[1]["name"] != "违约责任" {
		t.Fatalf("received=%#v", received)
	}
	if !strings.Contains(stdout.String(), `"id": "rule-1"`) {
		t.Fatalf("stdout=%s", stdout.String())
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
	payload := `{"source":{"type":"file","path":"` + filePath + `","name":"合同.PDF"},"config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":"中立","matchContractTypeRulePackage":true},"wait":true}`
	if err := Execute(context.Background(), runtime, []string{"review", "run", "--data", payload, "--dry-run", "--output", "json"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), `"appType"`) {
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
	payload := `{"source":{"type":"file","path":"` + filePath + `","name":"合同.pdf"},"config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":"中立","matchContractTypeRulePackage":true}}`
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
	payload := `{"source":{"type":"file","path":"` + filePath + `","name":"合同.pdf"},"businessId":"biz-1","config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":"中立","matchContractTypeRulePackage":true},"wait":true}`
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

// TestReviewTaskResultPollsAndRendersInfo 验证 result 命令隐藏轮询过程，并输出成功后的最终详情。
// 该测试属于命令层回归验证，确认 CLI 入口正确复用领域结果编排。
func TestReviewTaskResultPollsAndRendersInfo(t *testing.T) {
	statusCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/open-apis/contract-review/v3/smartAudit/task/status":
			if request.URL.Query().Get("appType") != "" || request.URL.Query().Get("visibilityScope") != review.VisibilityScopeContractResult {
				t.Errorf("status query=%s", request.URL.RawQuery)
			}
			statusCalls++
			if statusCalls == 1 {
				_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":{"taskId":88,"status":"running"}}`))
				return
			}
			_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":{"taskId":88,"status":"success"}}`))
		case "/open-apis/contract-review/v3/smartAudit/task/info":
			if request.URL.Query().Get("appType") != "" || request.URL.Query().Get("visibilityScope") != review.VisibilityScopeContractResult {
				t.Errorf("info query=%s", request.URL.RawQuery)
			}
			_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":{"taskId":88,"status":"success","url":"https://review.example.com/tasks/88","result":{"riskCount":0}}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	t.Setenv("EVERYLINE_ACCESS_TOKEN", "test-token")
	runtime, stdout, stderr := testRuntime(t)
	runtime.HTTP = server.Client()
	profile := config.Profile{Name: "local", BaseURL: server.URL, TokenURL: server.URL + "/token", AppID: "app", DefaultOutput: "json"}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}

	err := Execute(context.Background(), runtime, []string{
		"review", "task", "result", "--task-id", "88", "--business-id", "biz-1",
		"--visibility-scope", "contractResult",
		"--interval", "1ms", "--deadline", "1s", "--output", "json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if statusCalls != 2 {
		t.Fatalf("statusCalls=%d，期望 running 后继续轮询一次", statusCalls)
	}
	for _, expected := range []string{`"status": "success"`, `"riskCount": 0`} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("stdout=%s，缺少 %s", stdout.String(), expected)
		}
	}
	for _, expected := range []string{"status=running", "status=success"} {
		if !strings.Contains(stderr.String(), expected) {
			t.Fatalf("stderr=%s，缺少轮询状态 %s", stderr.String(), expected)
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
