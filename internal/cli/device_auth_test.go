package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/config"
)

// TestDoubaoLocalDeviceAuthorizationUsesFixedSession 验证豆包本地电脑补齐固定会话后全程使用 Device Grant。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；本地 OAuth 缓存被复用、发生浏览器登录或跨命令丢失 Device 事务时通过测试失败报告。
func TestDoubaoLocalDeviceAuthorizationUsesFixedSession(t *testing.T) {
	t.Setenv("SKILL_SESSION_WORKSPACE", "")
	t.Setenv("CODEBUDDY_SESSION_ID", "")
	t.Setenv("SESSION_ID", "")
	t.Chdir(t.TempDir())
	configDir := t.TempDir()
	if runtime := NewRuntime(configDir, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); runtime.DeviceCredentials != nil {
		t.Fatal("未提供宿主变量时应保持普通本地运行时")
	}
	// Skill 在豆包本地模式只生成一次标识，并在后续每个独立 CLI 进程中显式注入。
	t.Setenv("SESSION_ID", "fixed-doubao-local-session")
	verificationURL := "https://test-myaccount.qtech.cn/device?user_code=fixture-code&tenant=test"
	var deviceCalls, tokenCalls, unexpectedCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/metadata":
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"device_authorization_endpoint": server.URL + "/device", "token_endpoint": server.URL + "/token",
				"registration_endpoint": server.URL + "/register", "authorization_endpoint": server.URL + "/authorize",
			})
		case "/device":
			deviceCalls.Add(1)
			if err := request.ParseForm(); err != nil || request.PostForm.Get("client_id") != "device-client" || request.PostForm.Has("redirect_uri") {
				t.Errorf("Device 初始化参数错误: %v", request.PostForm)
			}
			_ = json.NewEncoder(writer).Encode(auth.DeviceAuthorizationResponse{
				DeviceCode: "fixture-device-code", VerificationURIComplete: verificationURL, ExpiresIn: 600,
			})
		case "/token":
			tokenCalls.Add(1)
			if err := request.ParseForm(); err != nil || request.PostForm.Get("grant_type") != auth.DeviceGrantType || request.PostForm.Get("device_code") != "fixture-device-code" || request.PostForm.Get("client_id") != "device-client" {
				t.Errorf("Device 兑换参数错误: %v", request.PostForm)
			}
			_, _ = writer.Write([]byte(`{"access_token":"fixture-device-access","token_type":"Bearer","refresh_token":"fixture-device-refresh","expires_in":3600}`))
		default:
			unexpectedCalls.Add(1)
			http.Error(writer, "unexpected browser OAuth request", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	newRuntime := func() (*Runtime, *bytes.Buffer) {
		stdout := &bytes.Buffer{}
		runtime := NewRuntime(configDir, strings.NewReader(""), stdout, &bytes.Buffer{})
		runtime.HTTP = server.Client()
		runtime.OpenBrowser = func(string) error {
			t.Error("豆包本地模式不应打开 OAuth 浏览器")
			return nil
		}
		if runtime.DeviceCredentials == nil || runtime.DeviceCredentialError != nil {
			t.Fatalf("固定 SESSION_ID 后未识别 Device 运行时: %v", runtime.DeviceCredentialError)
		}
		return runtime, stdout
	}
	profile := config.Profile{
		Name: "test-user", BaseURL: server.URL, TokenURL: server.URL + "/token", OAuthMetadataURL: server.URL + "/metadata",
		OAuthBusinessType: "contract-review", OAuthClientID: "browser-client", OAuthDeviceClientID: "device-client",
		OAuthRedirectURL: "http://127.0.0.1:8000/login", OAuthScopes: []string{"contract-review:full"},
		DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	runtime, stdout := newRuntime()
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	legacyStore := auth.NewFileTokenStore(filepath.Join(configDir, "tokens.json"))
	if err := legacyStore.SaveForIdentity(profile.Name, config.IdentityUser, auth.Token{AccessToken: "legacy-browser-token", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), runtime, []string{"auth", "status", "--profile", profile.Name, "--as", "user"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"source": "device"`) || !strings.Contains(stdout.String(), `"authenticated": false`) {
		t.Fatalf("Device 会话不应继承本地 OAuth 授权: %s", stdout.String())
	}
	if err := Execute(context.Background(), runtime, []string{"auth", "login", "--profile", profile.Name, "--as", "user"}); !errors.Is(err, auth.ErrUserAuthentication) || !strings.Contains(err.Error(), "auth init") {
		t.Fatalf("豆包本地登录未指向 Device Grant: %v", err)
	}
	// 每个命令重新创建真实运行时，验证事务经加密文件跨进程恢复，不依赖内存 store。
	for index, command := range []string{"init", "init", "complete", "status"} {
		runtime, stdout = newRuntime()
		if err := Execute(context.Background(), runtime, []string{"auth", command, "--profile", profile.Name, "--as", "user"}); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		var result map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		switch command {
		case "init":
			if result["verification_uri_complete"] != verificationURL || (index == 1 && result["reused"] != true) {
				t.Fatalf("Device 链接或事务未保留: %s", stdout.String())
			}
		case "complete":
			if result["status"] != "succeeded" {
				t.Fatalf("Device 授权未完成: %s", stdout.String())
			}
		case "status":
			if result["authenticated"] != true || result["source"] != "device" {
				t.Fatalf("未读取 Device 授权结果: %s", stdout.String())
			}
		}
	}
	if deviceCalls.Load() != 1 || tokenCalls.Load() != 1 || unexpectedCalls.Load() != 0 {
		t.Fatalf("device=%d token=%d unexpected=%d", deviceCalls.Load(), tokenCalls.Load(), unexpectedCalls.Load())
	}
}

// memoryDeviceCredentialStore 为 CLI Device 授权测试提供线程安全内存存储。
type memoryDeviceCredentialStore struct {
	mu          sync.Mutex
	refreshMu   sync.Mutex
	credentials map[string]auth.DeviceCredential
}

// Load 返回指定 Profile 的内存 Device 凭证。
// 入参：profileName string 为 Profile 名称。
// 返回值：auth.DeviceCredential 为凭证；error 为不存在。
func (store *memoryDeviceCredentialStore) Load(profileName string) (auth.DeviceCredential, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	credential, ok := store.credentials[profileName]
	if !ok {
		return auth.DeviceCredential{}, auth.ErrDeviceCredentialNotFound
	}
	return credential, nil
}

// Save 覆盖保存指定 Profile 的内存 Device 凭证。
// 入参：profileName string 为 Profile 名称；credential auth.DeviceCredential 为凭证。
// 返回值：error，始终为 nil。
func (store *memoryDeviceCredentialStore) Save(profileName string, credential auth.DeviceCredential) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.credentials == nil {
		store.credentials = map[string]auth.DeviceCredential{}
	}
	store.credentials[profileName] = credential
	return nil
}

// Delete 删除指定 Profile 的内存 Device 凭证。
// 入参：profileName string 为 Profile 名称。
// 返回值：error，始终为 nil。
func (store *memoryDeviceCredentialStore) Delete(profileName string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.credentials, profileName)
	return nil
}

// WithRefreshLock 串行执行内存测试中的刷新操作。
// 入参：profileName string 仅满足接口；action func() error 为刷新操作。
// 返回值：error，为 action 的结果。
func (store *memoryDeviceCredentialStore) WithRefreshLock(_ string, action func() error) error {
	store.refreshMu.Lock()
	defer store.refreshMu.Unlock()
	return action()
}

// TestAuthDeviceInitAndComplete 验证沙箱授权输出完整 URL、保存事务并在完成后隐藏 token。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；协议或输出泄露时通过 t.Fatal 报告。
func TestAuthDeviceInitAndComplete(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	var deviceForm url.Values
	var tokenForm url.Values
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/metadata":
			_, _ = writer.Write([]byte(`{"token_endpoint":"` + server.URL + `/token","device_authorization_endpoint":"` + server.URL + `/device","revocation_endpoint":"` + server.URL + `/revoke","grant_types_supported":["urn:ietf:params:oauth:grant-type:device_code","refresh_token"]}`))
		case "/device":
			_ = request.ParseForm()
			deviceForm = request.Form
			_, _ = writer.Write([]byte(`{"device_code":"private-device-code","user_code":"ABCD","verification_uri":"https://auth.example.com/device","verification_uri_complete":"https://auth.example.com/device?user_code=ABCD&tenant=test","expires_in":600}`))
		case "/token":
			_ = request.ParseForm()
			tokenForm = request.Form
			_, _ = writer.Write([]byte(`{"access_token":"private-access-token","token_type":"Bearer","refresh_token":"private-refresh-token","scope":"contract-review:full","expires_in":3600}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	runtime, stdout, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	runtime.Now = func() time.Time { return now }
	deviceStore := &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{
		"test-user": {Token: &auth.Token{AccessToken: "old-dev-token", ExpiresAt: now.Add(time.Hour)}},
	}}
	runtime.DeviceCredentials = deviceStore
	requireFirstInstallAuthorization(t, runtime)
	profile := config.Profile{
		Name: "test-user", BaseURL: "https://test-open.qtech.cn", UserBaseURL: "https://test-open.qtech.cn",
		TokenURL: "https://test-open.qtech.cn/token", OAuthMetadataURL: server.URL + "/metadata",
		OAuthBusinessType: "contract-review", OAuthClientID: "browser-client", OAuthDeviceClientID: "device-client",
		OAuthRedirectURL: "http://127.0.0.1:8000/login", OAuthScopes: []string{"contract-review:full"},
		DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), runtime, []string{"auth", "init", "--profile", "test-user", "--as", "user"}); err != nil {
		t.Fatal(err)
	}
	if deviceForm.Get("client_id") != "device-client" || deviceForm.Get("scope") != "contract-review:full" || deviceForm.Get("resource") != "https://test-open.qtech.cn" {
		t.Fatalf("device form=%v", deviceForm)
	}
	if !strings.Contains(stdout.String(), `"verification_uri_complete": "https://auth.example.com/device?user_code=ABCD&tenant=test"`) {
		t.Fatalf("stdout=%s，完整授权 URL 未原样输出", stdout.String())
	}
	if strings.Contains(stdout.String(), "private-device-code") {
		t.Fatalf("stdout 泄露 device code: %s", stdout.String())
	}
	pending, err := deviceStore.Load("test-user")
	if err != nil || pending.Pending == nil || pending.Token == nil || pending.Token.AccessToken != "old-dev-token" {
		t.Fatalf("首次安装应启动新事务并暂存旧 token: pending=%#v err=%v", pending, err)
	}

	stdout.Reset()
	if err := Execute(context.Background(), runtime, []string{"auth", "complete", "--profile", "test-user", "--as", "user"}); err != nil {
		t.Fatal(err)
	}
	if tokenForm.Get("grant_type") != auth.DeviceGrantType || tokenForm.Get("device_code") != "private-device-code" {
		t.Fatalf("token form=%v", tokenForm)
	}
	if strings.Contains(stdout.String(), "private-access-token") || strings.Contains(stdout.String(), "private-refresh-token") {
		t.Fatalf("auth complete 泄露 token: %s", stdout.String())
	}
	var output deviceAuthOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil || output.Status != "succeeded" {
		t.Fatalf("output=%#v err=%v", output, err)
	}
	stored, err := deviceStore.Load("test-user")
	if err != nil || stored.Pending != nil || stored.Token == nil || stored.Token.RefreshToken != "private-refresh-token" {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	installState, err := runtime.InstallState.Load()
	if err != nil || installState.FirstInstall || installState.AuthorizationRequired {
		t.Fatalf("installState=%#v err=%v，成功授权后应解除门禁", installState, err)
	}
}

// TestAuthDeviceInitReusesFirstInstallTransaction 验证同一首次安装事件重放 --restart 时只创建一笔 Device 授权。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；重复请求授权端点、链接变化或事件未绑定时通过 t.Fatal 报告。
func TestAuthDeviceInitReusesFirstInstallTransaction(t *testing.T) {
	now := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	var deviceCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/metadata":
			_, _ = writer.Write([]byte(`{"token_endpoint":"` + server.URL + `/token","device_authorization_endpoint":"` + server.URL + `/device"}`))
		case "/device":
			deviceCalls.Add(1)
			_, _ = writer.Write([]byte(`{"device_code":"one-device-code","user_code":"ABCD","verification_uri":"https://auth.example.com/device","verification_uri_complete":"https://auth.example.com/device?user_code=ABCD","expires_in":600}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	runtime, stdout, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	runtime.Now = func() time.Time { return now }
	runtime.DeviceCredentials = &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{}}
	requireFirstInstallAuthorization(t, runtime)
	profile := config.Profile{
		Name: "test-user", BaseURL: "https://test-open.qtech.cn", TokenURL: "https://test-open.qtech.cn/token",
		OAuthMetadataURL: server.URL + "/metadata", OAuthBusinessType: "contract-review", OAuthDeviceClientID: "device-client",
		OAuthScopes: []string{"contract-review:full"}, DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	arguments := []string{"auth", "init", "--restart", "--profile", profile.Name, "--as", "user"}
	if err := Execute(context.Background(), runtime, arguments); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := Execute(context.Background(), runtime, arguments); err != nil {
		t.Fatal(err)
	}
	if deviceCalls.Load() != 1 {
		t.Fatalf("deviceCalls=%d，同一首次安装事件只应创建一笔授权", deviceCalls.Load())
	}
	if !strings.Contains(stdout.String(), `"reused": true`) || !strings.Contains(stdout.String(), `"verification_uri_complete": "https://auth.example.com/device?user_code=ABCD"`) {
		t.Fatalf("stdout=%s，重放时应返回同一授权入口并标记 reused", stdout.String())
	}
	credential, err := runtime.DeviceCredentials.Load(profile.Name)
	if err != nil || credential.Pending == nil || credential.Pending.FirstInstallEventID != "first-install-test" {
		t.Fatalf("credential=%#v err=%v，Device 事务未绑定首次安装事件", credential, err)
	}
}

// TestAuthDeviceInitSerializesConcurrentCalls 验证同一 Profile 的并发初始化共享一笔 Device 授权。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；并发请求绕过锁、创建多笔事务或未返回复用标记时通过 t.Fatal 报告。
func TestAuthDeviceInitSerializesConcurrentCalls(t *testing.T) {
	now := time.Date(2026, 9, 7, 11, 30, 0, 0, time.UTC)
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var startedOnce sync.Once
	var deviceCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/metadata":
			_, _ = writer.Write([]byte(`{"token_endpoint":"` + server.URL + `/token","device_authorization_endpoint":"` + server.URL + `/device"}`))
		case "/device":
			deviceCalls.Add(1)
			startedOnce.Do(func() { close(requestStarted) })
			<-releaseRequest
			_, _ = writer.Write([]byte(`{"device_code":"shared-device-code","user_code":"EFGH","verification_uri":"https://auth.example.com/device","verification_uri_complete":"https://auth.example.com/device?user_code=EFGH","expires_in":600}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	profile := config.Profile{
		Name: "test-user", BaseURL: "https://test-open.qtech.cn", TokenURL: "https://test-open.qtech.cn/token",
		OAuthMetadataURL: server.URL + "/metadata", OAuthBusinessType: "contract-review", OAuthDeviceClientID: "device-client",
		OAuthScopes: []string{"contract-review:full"}, DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	deviceStore := &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{}}
	type initResult struct {
		output string
		err    error
	}
	runInit := func() <-chan initResult {
		result := make(chan initResult, 1)
		runtime, stdout, _ := testRuntime(t)
		runtime.HTTP = server.Client()
		runtime.Now = func() time.Time { return now }
		runtime.DeviceCredentials = deviceStore
		if err := runtime.Profiles.Add(profile); err != nil {
			t.Fatal(err)
		}
		go func() {
			err := Execute(context.Background(), runtime, []string{"auth", "init", "--profile", profile.Name, "--as", "user"})
			result <- initResult{output: stdout.String(), err: err}
		}()
		return result
	}

	firstResult := runInit()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("首个 Device 初始化请求未按时发出")
	}
	secondResult := runInit()
	select {
	case result := <-secondResult:
		t.Fatalf("第二个 init 在首个事务保存前返回: output=%s err=%v", result.output, result.err)
	case <-time.After(50 * time.Millisecond):
	}
	if deviceCalls.Load() != 1 {
		t.Fatalf("deviceCalls=%d，并发初始化不应创建第二笔授权", deviceCalls.Load())
	}
	close(releaseRequest)
	first := <-firstResult
	second := <-secondResult
	if first.err != nil || second.err != nil {
		t.Fatalf("firstErr=%v secondErr=%v", first.err, second.err)
	}
	if deviceCalls.Load() != 1 || !strings.Contains(second.output, `"reused": true`) {
		t.Fatalf("deviceCalls=%d secondOutput=%s，并发调用应复用首笔授权", deviceCalls.Load(), second.output)
	}
}

// TestAuthDeviceInitRequiresDedicatedClient 验证 Device Grant 缺少专用 client 时在任何网络请求前停止。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；Device 流程尝试动态注册、复用浏览器 client 或访问授权端点时通过 t.Fatal 报告。
func TestAuthDeviceInitRequiresDedicatedClient(t *testing.T) {
	requestCalled := false
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCalled = true
		http.Error(writer, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()

	runtime, _, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	runtime.DeviceCredentials = &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{}}
	profile := config.Profile{
		Name: "test-user", BaseURL: server.URL, TokenURL: server.URL + "/token",
		OAuthMetadataURL: server.URL + "/metadata", OAuthBusinessType: "contract-review",
		OAuthRedirectURL: "http://127.0.0.1:8000/login", OAuthScopes: []string{"contract-review:full"},
		DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}

	err := Execute(context.Background(), runtime, []string{"auth", "init", "--profile", profile.Name, "--as", "user"})
	if err == nil || !strings.Contains(err.Error(), "Device OAuth 配置不完整") {
		t.Fatalf("err=%v", err)
	}
	if requestCalled {
		t.Fatal("缺少专用 Device client 时不应发起任何网络请求")
	}
}

// TestSelectedProfileRestoresEncryptedDeviceSnapshot 验证沙箱 HOME 重建后可用显式 Profile 名恢复配置。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；恢复或状态识别失败时通过 t.Fatal 报告。
func TestSelectedProfileRestoresEncryptedDeviceSnapshot(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	profile := config.Profile{
		Name: "test-user", BaseURL: "https://test-open.qtech.cn", TokenURL: "https://test-open.qtech.cn/token",
		OAuthMetadataURL:  "https://test-myaccount.qtech.cn/.well-known/oauth-authorization-server/contract-review",
		OAuthBusinessType: "contract-review", OAuthClientID: "client", OAuthRedirectURL: "http://127.0.0.1:8000/login",
		OAuthScopes: []string{"contract-review:full"}, DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	runtime.Profiles = config.NewFileStore(t.TempDir() + "/empty-config.json")
	runtime.DeviceCredentials = &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{
		"test-user": {Profile: &profile, Token: &auth.Token{AccessToken: "secret", ExpiresAt: time.Now().Add(time.Hour)}},
	}}
	if err := Execute(context.Background(), runtime, []string{"auth", "status", "--profile", "test-user", "--as", "user"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"source": "device"`) || !strings.Contains(stdout.String(), `"authenticated": true`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
	restored, err := runtime.Profiles.Get("test-user")
	if err != nil || restored.DefaultIdentity != config.IdentityUser {
		t.Fatalf("restored=%#v err=%v", restored, err)
	}
}

// TestAuthUserLoginRedirectsSandboxToDeviceGrant 验证豆包或 WorkBuddy 沙箱不会启动 loopback OAuth 登录。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；命令未返回鉴权错误或下一步未指向 auth init 时通过 t.Fatal 报告。
func TestAuthUserLoginRedirectsSandboxToDeviceGrant(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	profile := config.Profile{
		Name: "test-user", BaseURL: "https://test-open.qtech.cn", UserBaseURL: "https://test-open.qtech.cn",
		TokenURL: "https://test-open.qtech.cn/token", OAuthMetadataURL: "https://account.example.com/.well-known/oauth-authorization-server",
		OAuthBusinessType: "contract-review", OAuthClientID: "browser-client", OAuthDeviceClientID: "device-client",
		OAuthRedirectURL: "http://127.0.0.1:8000/login", OAuthScopes: []string{"contract-review:full"},
		DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	runtime.DeviceCredentials = &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{}}

	err := Execute(context.Background(), runtime, []string{"auth", "login", "--profile", profile.Name, "--as", "user"})
	if !errors.Is(err, auth.ErrUserAuthentication) {
		t.Fatalf("err=%v，期望 user 鉴权错误", err)
	}
	if !strings.Contains(err.Error(), "auth init --profile test-user --as user --output json") || strings.Contains(err.Error(), "auth login") {
		t.Fatalf("沙箱登录未引导到 Device Grant: %v", err)
	}
}

// TestAuthDeviceCompleteExplainsSessionRecovery 验证待完成事务不可见时优先恢复原会话标识，而不是误导用户重新授权。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；错误提示缺少 WorkBuddy 会话绑定和无重复授权恢复步骤时通过 t.Fatal 报告。
func TestAuthDeviceCompleteExplainsSessionRecovery(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	profile := config.Profile{
		Name: "test-user", BaseURL: "https://test-open.qtech.cn", TokenURL: "https://test-open.qtech.cn/token",
		DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	runtime.DeviceCredentials = &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{}}

	err := Execute(context.Background(), runtime, []string{"auth", "complete", "--profile", profile.Name, "--as", "user"})
	if err == nil {
		t.Fatal("auth complete 缺少待完成事务时应返回错误")
	}
	for _, expected := range []string{
		"auth init 与 auth complete 必须复用同一 Device 会话标识",
		"WorkBuddy CODEBUDDY_SESSION_ID",
		"恢复 auth init 使用的原标识后重新执行 auth complete",
		"仅在 CLI 明确返回 denied、expired 或 invalid_grant 后开始新事务",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("auth complete 恢复提示缺少 %q: %v", expected, err)
		}
	}
}

// TestAuthDeviceCompleteSerializesConcurrentChecks 验证两个 complete 命令串行兑换同一 Device code，并共同返回成功状态。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；第二个命令提前返回 uncertain、重复兑换或凭证写回错误时通过 t.Fatal 报告。
func TestAuthDeviceCompleteSerializesConcurrentChecks(t *testing.T) {
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/token" {
			http.NotFound(writer, request)
			return
		}
		if tokenCalls.Add(1) == 1 {
			close(requestStarted)
			<-releaseRequest
		}
		_, _ = writer.Write([]byte(`{"access_token":"device-access","token_type":"Bearer","refresh_token":"device-refresh","expires_in":3600}`))
	}))
	defer server.Close()

	profile := config.Profile{
		Name: "test-user", BaseURL: "https://test-open.qtech.cn", TokenURL: "https://test-open.qtech.cn/token",
		OAuthMetadataURL: server.URL + "/metadata", OAuthBusinessType: "contract-review", OAuthClientID: "browser-client",
		OAuthDeviceClientID: "device-client", OAuthRedirectURL: "http://127.0.0.1:8000/login",
		OAuthScopes: []string{"contract-review:full"}, DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	deviceStore := &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{
		profile.Name: {
			Profile: &profile,
			Pending: &auth.DevicePendingTransaction{
				Status: auth.DevicePending, DeviceCode: "one-time-device-code", TokenEndpoint: server.URL + "/token",
				ClientID: "device-client", ExpiresAt: now.Add(10 * time.Minute),
			},
		},
	}}
	type commandResult struct {
		output string
		err    error
	}
	runComplete := func() <-chan commandResult {
		result := make(chan commandResult, 1)
		runtime, stdout, _ := testRuntime(t)
		runtime.HTTP = server.Client()
		runtime.Now = func() time.Time { return now }
		runtime.DeviceCredentials = deviceStore
		if err := runtime.Profiles.Add(profile); err != nil {
			t.Fatal(err)
		}
		go func() {
			err := Execute(context.Background(), runtime, []string{"auth", "complete", "--profile", profile.Name, "--as", "user"})
			result <- commandResult{output: stdout.String(), err: err}
		}()
		return result
	}

	firstResult := runComplete()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("首个 complete 请求未按时发出")
	}
	secondResult := runComplete()
	var earlySecond *commandResult
	select {
	case result := <-secondResult:
		earlySecond = &result
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseRequest)
	first := <-firstResult
	second := commandResult{}
	if earlySecond != nil {
		second = *earlySecond
	} else {
		second = <-secondResult
	}
	if earlySecond != nil {
		t.Fatalf("第二个 complete 在首个兑换完成前返回: output=%s err=%v", second.output, second.err)
	}
	for _, result := range []commandResult{first, second} {
		var output deviceAuthOutput
		if result.err != nil || json.Unmarshal([]byte(result.output), &output) != nil || output.Status != "succeeded" {
			t.Fatalf("complete output=%s err=%v", result.output, result.err)
		}
	}
	if tokenCalls.Load() != 1 {
		t.Fatalf("tokenCalls=%d，期望 Device code 只兑换一次", tokenCalls.Load())
	}
}

// TestAuthLogoutClearsLocalCredentialsWhenRevocationFails 验证远端撤销失败时仍清理 Device 与兼容文件缓存。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；任一本地凭证残留或错误未说明本地状态时通过 t.Fatal 报告。
func TestAuthLogoutClearsLocalCredentialsWhenRevocationFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	runtime, _, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	profile := config.Profile{
		Name: "test-user", BaseURL: "https://test-open.qtech.cn", TokenURL: "https://test-open.qtech.cn/token",
		OAuthMetadataURL: server.URL + "/metadata", OAuthBusinessType: "contract-review", OAuthClientID: "browser-client",
		OAuthDeviceClientID: "device-client", OAuthRedirectURL: "http://127.0.0.1:8000/login",
		OAuthRevocationURL: server.URL + "/revoke", OAuthScopes: []string{"contract-review:full"},
		DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	deviceStore := &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{
		profile.Name: {Token: &auth.Token{AccessToken: "device-access", RefreshToken: "device-refresh"}},
	}}
	runtime.DeviceCredentials = deviceStore
	identityStore := runtime.Tokens.(interface {
		SaveForIdentity(string, config.IdentityKind, auth.Token) error
		LoadForIdentity(string, config.IdentityKind) (auth.Token, error)
	})
	if err := identityStore.SaveForIdentity(profile.Name, config.IdentityUser, auth.Token{AccessToken: "legacy-user-token"}); err != nil {
		t.Fatal(err)
	}

	err := Execute(context.Background(), runtime, []string{"auth", "logout", "--profile", profile.Name, "--as", "user"})
	if err == nil || !strings.Contains(err.Error(), "本地凭证已清理") {
		t.Fatalf("err=%v，期望报告远端撤销失败但本地已清理", err)
	}
	if _, loadErr := deviceStore.Load(profile.Name); !errors.Is(loadErr, auth.ErrDeviceCredentialNotFound) {
		t.Fatalf("Device 凭证仍存在: %v", loadErr)
	}
	if _, loadErr := identityStore.LoadForIdentity(profile.Name, config.IdentityUser); loadErr == nil {
		t.Fatal("兼容 user token 仍存在")
	}
}

// TestAuthLogoutWaitsForRefreshBeforeDeleting 验证 logout 与 refresh 使用同一把锁，并在刷新完成后删除最新凭证。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；logout 提前成功或刷新后凭证复活时通过 t.Fatal 报告。
func TestAuthLogoutWaitsForRefreshBeforeDeleting(t *testing.T) {
	runtime, _, _ := testRuntime(t)
	profile := config.Profile{
		Name: "test-user", BaseURL: "https://test-open.qtech.cn", TokenURL: "https://test-open.qtech.cn/token",
		DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	deviceStore := &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{
		profile.Name: {Token: &auth.Token{AccessToken: "old-access"}},
	}}
	runtime.DeviceCredentials = deviceStore
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	refreshDone := make(chan error, 1)
	go func() {
		refreshDone <- deviceStore.WithRefreshLock(profile.Name, func() error {
			close(refreshStarted)
			<-releaseRefresh
			return deviceStore.Save(profile.Name, auth.DeviceCredential{Token: &auth.Token{AccessToken: "new-access"}})
		})
	}()
	<-refreshStarted
	logoutResult := make(chan error, 1)
	go func() {
		logoutResult <- Execute(context.Background(), runtime, []string{"auth", "logout", "--profile", profile.Name, "--as", "user"})
	}()
	var earlyLogout error
	logoutReturnedEarly := false
	select {
	case earlyLogout = <-logoutResult:
		logoutReturnedEarly = true
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseRefresh)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	if logoutReturnedEarly {
		t.Fatalf("logout 在 refresh 完成前返回: %v", earlyLogout)
	}
	if err := <-logoutResult; err != nil {
		t.Fatal(err)
	}
	if _, err := deviceStore.Load(profile.Name); !errors.Is(err, auth.ErrDeviceCredentialNotFound) {
		t.Fatalf("logout 后 Device 凭证重新出现: %v", err)
	}
}

var _ auth.DeviceCredentialStore = (*memoryDeviceCredentialStore)(nil)
