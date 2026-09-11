package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
			if result["verification_link_text"] != "点击授权" {
				t.Fatalf("豆包首次及复用授权必须返回统一入口文案: %s", stdout.String())
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
	if !strings.Contains(stdout.String(), `"verification_link_text": "点击授权"`) {
		t.Fatalf("Device 授权缺少统一入口文案: %s", stdout.String())
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
	if strings.Contains(stdout.String(), "verification_link_text") {
		t.Fatalf("已完成授权不应继续提示点击授权: %s", stdout.String())
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

/*
TestAuthDeviceInitPreservesConfigurationUntilAuthorization 验证 user 初始化跳过配置重写，app 仅在远端授权事务创建成功后切换身份。
入参：t *testing.T 为测试上下文。
返回值：无；冗余替换配置、提前切换身份或远端失败后旧凭据发生变化时通过测试失败报告。
*/
func TestAuthDeviceInitPreservesConfigurationUntilAuthorization(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		identity      config.IdentityKind
		remoteFailure bool
	}{
		{name: "user preserves config file", identity: config.IdentityUser},
		{name: "app remote failure preserves identity and credentials", identity: config.IdentityApp, remoteFailure: true},
		{name: "app switches identity after remote success", identity: config.IdentityApp},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runtime, _, _ := testRuntime(t)
			// 使用真实临时配置文件，通过文件身份和字节检查是否发生过原子替换。
			configPath := filepath.Join(t.TempDir(), "config.json")
			runtime.Profiles = config.NewFileStore(configPath)
			now := time.Date(2026, 9, 11, 11, 0, 0, 0, time.UTC)
			runtime.Now = func() time.Time { return now }
			var deviceCalls atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/metadata":
					_ = json.NewEncoder(writer).Encode(map[string]string{
						"device_authorization_endpoint": server.URL + "/device", "token_endpoint": server.URL + "/token",
					})
				case "/device":
					deviceCalls.Add(1)
					// 远端尚未返回结果时保持原身份，失败的授权申请不应改变业务默认身份。
					stored, err := runtime.Profiles.Get("test-user")
					if err != nil || stored.DefaultIdentity != scenario.identity {
						t.Errorf("远端返回成功前默认身份发生变化: identity=%s err=%v", stored.DefaultIdentity, err)
					}
					if scenario.remoteFailure {
						http.Error(writer, "device service unavailable", http.StatusServiceUnavailable)
						return
					}
					_, _ = writer.Write([]byte(`{"device_code":"new-device-code","verification_uri_complete":"https://auth.example.com/device?user_code=NEW","expires_in":600}`))
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()
			runtime.HTTP = server.Client()
			profile := config.Profile{
				Name: "test-user", BaseURL: server.URL, TokenURL: server.URL + "/token", AppID: "app-id",
				OAuthMetadataURL: server.URL + "/metadata", OAuthDeviceClientID: "device-client",
				OAuthBusinessType: "contract-review", OAuthScopes: []string{"contract-review:full"},
				DefaultIdentity: scenario.identity, DefaultOutput: "json",
			}
			if err := runtime.Profiles.Add(profile); err != nil {
				t.Fatal(err)
			}
			configBefore, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			fileBefore, err := os.Stat(configPath)
			if err != nil {
				t.Fatal(err)
			}
			// 在执行前序列化旧凭据，避免内存 Store 的共享指针掩盖原地修改。
			previous := auth.DeviceCredential{
				Pending: &auth.DevicePendingTransaction{Status: auth.DevicePending, DeviceCode: "old-device-code", ExpiresAt: now.Add(time.Minute)},
				Token:   &auth.Token{AccessToken: "old-token", ExpiresAt: now.Add(-time.Hour)},
			}
			previousJSON, err := json.Marshal(previous)
			if err != nil {
				t.Fatal(err)
			}
			deviceStore := &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{profile.Name: previous}}
			runtime.DeviceCredentials = deviceStore
			err = Execute(context.Background(), runtime, []string{"auth", "init", "--profile", profile.Name, "--restart"})
			if deviceCalls.Load() != 1 {
				t.Fatalf("新建授权请求次数错误: calls=%d err=%v", deviceCalls.Load(), err)
			}
			configAfter, readErr := os.ReadFile(configPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			fileAfter, statErr := os.Stat(configPath)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if scenario.identity == config.IdentityUser || scenario.remoteFailure {
				if !os.SameFile(fileBefore, fileAfter) || !bytes.Equal(configBefore, configAfter) {
					t.Error("user 初始化或远端授权失败不应替换配置文件或改变内容")
				}
			}
			persisted, getErr := runtime.Profiles.Get(profile.Name)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if scenario.remoteFailure {
				if err == nil || persisted.DefaultIdentity != config.IdentityApp {
					t.Errorf("远端失败后应保留 app 默认身份: identity=%s err=%v", persisted.DefaultIdentity, err)
				}
				stored, loadErr := deviceStore.Load(profile.Name)
				storedJSON, marshalErr := json.Marshal(stored)
				if loadErr != nil || marshalErr != nil || !bytes.Equal(storedJSON, previousJSON) {
					t.Fatalf("失败后旧凭据发生变化: load=%v marshal=%v", loadErr, marshalErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if persisted.DefaultIdentity != config.IdentityUser || persisted.AppID != profile.AppID {
				t.Fatalf("远端成功后应切换 user 身份并保留其他配置: identity=%s appID=%s", persisted.DefaultIdentity, persisted.AppID)
			}
			stored, err := deviceStore.Load(profile.Name)
			if err != nil || stored.Pending == nil || stored.Pending.DeviceCode != "new-device-code" || stored.Profile == nil || stored.Profile.DefaultIdentity != config.IdentityUser || stored.Profile.AppID != "" || stored.Token != nil {
				t.Fatalf("新授权事务或 user 配置快照未保存: %v", err)
			}
		})
	}
}

/*
TestAuthDeviceInitRenewsExpiredUserToken 验证过期凭证会生成一笔新授权，手动完成后恢复登录，有效凭证继续复用。
入参：t *testing.T 为测试上下文。
返回值：无；过期状态误报成功、重复创建事务、提前兑换或未恢复登录时通过测试失败报告。
*/
func TestAuthDeviceInitRenewsExpiredUserToken(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	for _, scenario := range []struct {
		name      string
		expiresAt time.Time
		renew     bool
	}{
		{name: "refresh_failure", expiresAt: now.Add(time.Minute), renew: true},
		{name: "refresh_window", expiresAt: now.Add(time.Minute), renew: true},
		{name: "expired", expiresAt: now.Add(-time.Minute), renew: true},
		{name: "expires_now", expiresAt: now, renew: true},
		{name: "still_valid", expiresAt: now.Add(time.Minute)},
		{name: "unknown_expiry"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var deviceCalls, tokenCalls atomic.Int32
			verificationURL := "https://auth.example.com/device?user_code=NEW-CODE&tenant=test"
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/metadata":
					_, _ = writer.Write([]byte(`{"token_endpoint":"` + server.URL + `/token","device_authorization_endpoint":"` + server.URL + `/device"}`))
				case "/device":
					deviceCalls.Add(1)
					_, _ = writer.Write([]byte(`{"device_code":"private-new-device-code","user_code":"NEW-CODE","verification_uri":"https://auth.example.com/device","verification_uri_complete":"` + verificationURL + `","expires_in":600}`))
				case "/token":
					_ = request.ParseForm()
					if request.Form.Get("grant_type") == "refresh_token" {
						http.Error(writer, "temporary failure", http.StatusServiceUnavailable)
						return
					}
					tokenCalls.Add(1)
					if err := request.ParseForm(); err != nil || request.Form.Get("grant_type") != auth.DeviceGrantType || request.Form.Get("device_code") != "private-new-device-code" {
						http.Error(writer, "unexpected token exchange", http.StatusBadRequest)
						return
					}
					_, _ = writer.Write([]byte(`{"access_token":"private-new-user-token","token_type":"Bearer","expires_in":3600}`))
				default:
					t.Errorf("unexpected request: %s", request.URL.Path)
					http.NotFound(writer, request)
				}
			}))
			t.Cleanup(server.Close)
			runtime, stdout, _ := testRuntime(t)
			runtime.HTTP = server.Client()
			runtime.Now = func() time.Time { return now }
			profile := config.Profile{
				Name: "test-user", BaseURL: "https://test-open.qtech.cn", TokenURL: "https://test-open.qtech.cn/token",
				OAuthMetadataURL: server.URL + "/metadata", OAuthDeviceClientID: "device-client",
				OAuthBusinessType: "contract-review", OAuthScopes: []string{"contract-review:full"},
				DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
			}
			if err := runtime.Profiles.Add(profile); err != nil {
				t.Fatal(err)
			}
			deviceStore := &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{
				profile.Name: {Token: &auth.Token{AccessToken: "private-old-user-token", ExpiresAt: scenario.expiresAt}},
			}}
			if scenario.name == "refresh_failure" {
				credential := deviceStore.credentials[profile.Name]
				credential.Token.RefreshToken = "fixture-refresh"
				deviceStore.credentials[profile.Name] = credential
			}
			runtime.DeviceCredentials = deviceStore
			provider := auth.NewProvider(runtime.Tokens, runtime.HTTP, runtime.Now).WithDeviceCredentials(deviceStore)
			if scenario.name == "refresh_window" || scenario.name == "refresh_failure" {
				_, err := provider.TokenForIdentity(context.Background(), profile, config.IdentityUser)
				if err == nil || !strings.Contains(err.Error(), "auth init --restart") {
					t.Fatalf("缺少可恢复的授权命令: %v", err)
				}
			}
			// 连续 init 模拟 Agent 重试；待用户完成前只允许一笔授权且不请求 token endpoint。
			for attempt := 0; attempt < 2; attempt++ {
				stdout.Reset()
				args := []string{"auth", "init", "--profile", profile.Name, "--as", "user"}
				if (scenario.name == "refresh_window" || scenario.name == "refresh_failure") && attempt == 0 {
					args = append(args, "--restart")
				}
				if err := Execute(context.Background(), runtime, args); err != nil {
					t.Fatal(err)
				}
				var result deviceAuthOutput
				if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if scenario.renew {
					if result.Status != "pending" || result.VerificationURIComplete != verificationURL || result.Reused != (attempt == 1) || deviceCalls.Load() != 1 {
						t.Fatalf("重新授权结果=%#v，授权请求数=%d", result, deviceCalls.Load())
					}
				} else if result.Status != "succeeded" || result.VerificationURIComplete != "" || deviceCalls.Load() != 0 {
					t.Fatalf("有效凭证未复用: result=%#v，授权请求数=%d", result, deviceCalls.Load())
				}
			}
			if tokenCalls.Load() != 0 {
				t.Fatal("用户完成授权前已兑换 token")
			}
			if !scenario.renew {
				return
			}
			stdout.Reset()
			if err := Execute(context.Background(), runtime, []string{"auth", "complete", "--profile", profile.Name, "--as", "user"}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), `"status": "succeeded"`) || strings.Contains(stdout.String(), "private-") || tokenCalls.Load() != 1 {
				t.Fatalf("手动授权完成结果=%s，兑换请求数=%d", stdout.String(), tokenCalls.Load())
			}
			token, err := provider.TokenForIdentity(context.Background(), profile, config.IdentityUser)
			if err != nil || token.AccessToken != "private-new-user-token" {
				t.Fatalf("业务凭证恢复失败: %v", err)
			}
			stdout.Reset()
			if err := Execute(context.Background(), runtime, []string{"auth", "status", "--profile", profile.Name, "--as", "user"}); err != nil || !strings.Contains(stdout.String(), `"authenticated": true`) {
				t.Fatalf("重新授权未恢复登录: stdout=%s err=%v", stdout.String(), err)
			}
		})
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

/*
TestAuthDeviceExpiredTransactionCanRestart 验证首次安装的等待、异常和中断事务到期后可显式重启。
入参：t *testing.T 为测试上下文。
返回值：无；到期前重复创建事务、到期后卡住或重复兑换旧 code 时通过 t.Fatal 报告。
*/
func TestAuthDeviceExpiredTransactionCanRestart(t *testing.T) {
	for _, scenario := range []struct {
		status auth.DevicePendingStatus
		check  bool
	}{
		{status: auth.DevicePending, check: true},
		{status: auth.DevicePendingUncertain},
		{status: auth.DevicePendingChecking},
		{status: auth.DevicePendingUncertain, check: true},
		{status: auth.DevicePendingChecking, check: true},
	} {
		name := string(scenario.status) + "/direct_restart"
		if scenario.check {
			name = string(scenario.status) + "/complete_then_restart"
		}
		t.Run(name, func(t *testing.T) {
			status := scenario.status
			now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
			var deviceCalls, tokenCalls atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/metadata":
					_ = json.NewEncoder(writer).Encode(auth.OAuthMetadata{TokenEndpoint: server.URL + "/token", DeviceAuthorizationEndpoint: server.URL + "/device"})
				case "/device":
					deviceCalls.Add(1)
					_ = json.NewEncoder(writer).Encode(auth.DeviceAuthorizationResponse{
						DeviceCode: "fixture-code", VerificationURIComplete: "https://auth.example.com/device?user_code=fixture", ExpiresIn: 600,
					})
				case "/token":
					tokenCalls.Add(1)
					http.Error(writer, "temporary upstream failure", http.StatusServiceUnavailable)
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
				Name: "test-user", BaseURL: server.URL, TokenURL: server.URL + "/token", OAuthMetadataURL: server.URL + "/metadata",
				OAuthDeviceClientID: "device-client", OAuthScopes: []string{"contract-review:full"}, DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
			}
			if err := runtime.Profiles.Add(profile); err != nil {
				t.Fatal(err)
			}
			run := func(action string, restart bool) deviceAuthOutput {
				t.Helper()
				stdout.Reset()
				args := []string{"auth", action, "--profile", profile.Name, "--as", "user"}
				if restart {
					args = append(args, "--restart")
				}
				if err := Execute(context.Background(), runtime, args); err != nil {
					t.Fatal(err)
				}
				var result deviceAuthOutput
				if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			run("init", true)
			if status == auth.DevicePendingUncertain {
				if result := run("complete", false); result.Status != "uncertain" {
					t.Fatalf("503 后状态=%s", result.Status)
				}
			} else if status == auth.DevicePendingChecking {
				// 模拟进程在发出兑换请求前中断，留下已持久化的 checking。
				credential, err := runtime.DeviceCredentials.Load(profile.Name)
				if err != nil {
					t.Fatal(err)
				}
				credential.Pending.Status = status
				if err := runtime.DeviceCredentials.Save(profile.Name, credential); err != nil {
					t.Fatal(err)
				}
			}
			if result := run("init", true); !result.Reused || deviceCalls.Load() != 1 {
				t.Fatalf("到期前应复用首次安装事务: result=%+v deviceCalls=%d", result, deviceCalls.Load())
			}
			// 覆盖恰好到期的边界；到期查询不得再次向 token endpoint 兑换旧 code。
			now = now.Add(10 * time.Minute)
			if result := run("init", false); result.Status != "expired" || !result.Reused || result.VerificationLinkText != "" {
				t.Fatalf("到期后 init 状态=%+v", result)
			}
			if scenario.check {
				if result := run("complete", false); result.Status != "expired" {
					t.Fatalf("到期后 complete 状态=%+v", result)
				}
			}
			if result := run("init", true); result.Status != "pending" || result.Reused || deviceCalls.Load() != 2 {
				t.Fatalf("显式重启未创建新事务: result=%+v deviceCalls=%d", result, deviceCalls.Load())
			}
			wantTokenCalls := int32(0)
			if status == auth.DevicePendingUncertain {
				wantTokenCalls = 1
			}
			if tokenCalls.Load() != wantTokenCalls {
				t.Fatalf("tokenCalls=%d，期望 %d", tokenCalls.Load(), wantTokenCalls)
			}
		})
	}
}

/*
TestAuthDeviceRetriesFirstInstallState 验证 token 保存后安装状态写入失败，可跨运行时恢复，并在 token 过期后重新授权。
入参：t *testing.T 为测试上下文。
返回值：无；重试再次请求授权、兑换 token 或留下首次安装门禁时通过 t.Fatal 报告。
*/
func TestAuthDeviceRetriesFirstInstallState(t *testing.T) {
	for _, scenario := range []struct {
		name, retryAction string
		expired, restart  bool
	}{
		{"complete_valid", "complete", false, false},
		{"init_valid", "init", false, true},
		{"complete_expired", "complete", true, false},
		{"init_expired", "init", true, false},
		{"restart_expired", "init", true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("SKILL_SESSION_WORKSPACE", "")
			t.Setenv("CODEBUDDY_SESSION_ID", "")
			t.Setenv("SESSION_ID", "first-install-recovery-session")
			t.Chdir(t.TempDir())
			configDir := t.TempDir()
			statePath := filepath.Join(configDir, "install-state.json")
			var deviceCalls, tokenCalls atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/metadata":
					_ = json.NewEncoder(writer).Encode(auth.OAuthMetadata{TokenEndpoint: server.URL + "/token", DeviceAuthorizationEndpoint: server.URL + "/device"})
				case "/device":
					deviceCalls.Add(1)
					_ = json.NewEncoder(writer).Encode(auth.DeviceAuthorizationResponse{
						DeviceCode: "fixture-code", VerificationURIComplete: "https://auth.example.com/device?user_code=fixture", ExpiresIn: 600,
					})
				case "/token":
					if tokenCalls.Add(1) == 1 {
						// 在门禁前置检查之后制造真实文件错误，token 仍能保存到独立的加密存储。
						if err := os.Rename(statePath, statePath+".backup"); err != nil {
							t.Error(err)
							http.Error(writer, "fixture error", http.StatusInternalServerError)
							return
						}
						if err := os.Mkdir(statePath, 0o700); err != nil {
							t.Error(err)
							http.Error(writer, "fixture error", http.StatusInternalServerError)
							return
						}
					}
					_, _ = writer.Write([]byte(`{"access_token":"fixture-access","token_type":"Bearer","expires_in":3600}`))
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()
			currentTime := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
			newRuntime := func() (*Runtime, *bytes.Buffer) {
				stdout := &bytes.Buffer{}
				runtime := NewRuntime(configDir, strings.NewReader(""), stdout, &bytes.Buffer{})
				runtime.HTTP = server.Client()
				runtime.Now = func() time.Time { return currentTime }
				return runtime, stdout
			}
			runtime, _ := newRuntime()
			requireFirstInstallAuthorization(t, runtime)
			profile := config.Profile{
				Name: "test-user", BaseURL: server.URL, TokenURL: server.URL + "/token", OAuthMetadataURL: server.URL + "/metadata",
				OAuthDeviceClientID: "device-client", OAuthScopes: []string{"contract-review:full"}, DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
			}
			if err := runtime.Profiles.Add(profile); err != nil {
				t.Fatal(err)
			}
			if err := Execute(context.Background(), runtime, []string{"auth", "init", "--restart", "--profile", profile.Name, "--as", "user"}); err != nil {
				t.Fatal(err)
			}
			if err := Execute(context.Background(), runtime, []string{"auth", "complete", "--profile", profile.Name, "--as", "user"}); err == nil || !strings.Contains(err.Error(), "保存首次安装授权结果") {
				t.Fatalf("预期安装状态提交失败，实际 err=%v", err)
			}
			if err := os.Remove(statePath); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(statePath+".backup", statePath); err != nil {
				t.Fatal(err)
			}
			// 新运行时必须从加密文件恢复完成证明，不依赖前一条命令的内存状态。
			if scenario.expired {
				currentTime = currentTime.Add(2 * time.Hour)
			}
			runtime, stdout := newRuntime()
			args := []string{"auth", scenario.retryAction, "--profile", profile.Name, "--as", "user"}
			if scenario.restart {
				args = append(args, "--restart")
			}
			retryErr := Execute(context.Background(), runtime, args)
			if scenario.expired {
				if scenario.retryAction == "complete" {
					if !errors.Is(retryErr, auth.ErrUserAuthentication) || !strings.Contains(retryErr.Error(), "auth init --restart") {
						t.Fatalf("过期凭证应提示重新授权: output=%s err=%v", stdout.String(), retryErr)
					}
				} else {
					var result deviceAuthOutput
					if err := json.Unmarshal(stdout.Bytes(), &result); retryErr != nil || err != nil || result.Status != "pending" || result.VerificationURIComplete == "" {
						t.Fatalf("过期后应创建新事务: result=%+v err=%v decode=%v", result, retryErr, err)
					}
				}
				state, err := runtime.InstallState.Load()
				if err != nil || !state.AuthorizationRequired || !state.FirstInstall {
					t.Fatalf("过期凭证不应解除门禁: state=%+v err=%v", state, err)
				}
				wantDeviceCalls := int32(1)
				if scenario.retryAction == "init" {
					wantDeviceCalls = 2
				}
				if deviceCalls.Load() != wantDeviceCalls || tokenCalls.Load() != 1 {
					t.Fatalf("过期恢复请求数错误: device=%d token=%d", deviceCalls.Load(), tokenCalls.Load())
				}
				return
			}
			if retryErr != nil {
				t.Fatal(retryErr)
			}
			var result deviceAuthOutput
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Status != "succeeded" {
				t.Fatalf("重试未完成授权: result=%+v err=%v", result, err)
			}
			state, err := runtime.InstallState.Load()
			if err != nil || state.FirstInstall || state.AuthorizationRequired {
				t.Fatalf("重试后门禁未解除: state=%+v err=%v", state, err)
			}
			stdout.Reset()
			if err := Execute(context.Background(), runtime, []string{"auth", "status", "--profile", profile.Name, "--as", "user"}); err != nil || !strings.Contains(stdout.String(), `"authenticated": true`) {
				t.Fatalf("恢复后状态错误: output=%s err=%v", stdout.String(), err)
			}
			if deviceCalls.Load() != 1 || tokenCalls.Load() != 1 {
				t.Fatalf("恢复时不应重新授权: deviceCalls=%d tokenCalls=%d", deviceCalls.Load(), tokenCalls.Load())
			}
		})
	}
}

/*
TestAuthDeviceCompleteRejectsOldToken 验证无事件证明或属于旧事件的 token 都不解除新安装门禁。
入参：t *testing.T 为测试上下文。
返回值：无；旧凭证绕过新授权或改变首次安装状态时通过 t.Fatal 报告。
*/
func TestAuthDeviceCompleteRejectsOldToken(t *testing.T) {
	for _, eventID := range []string{"", "previous-install-event"} {
		t.Run("token_event="+eventID, func(t *testing.T) {
			runtime, stdout, _ := testRuntime(t)
			requireFirstInstallAuthorization(t, runtime)
			profile := config.Profile{Name: "test-user", BaseURL: "https://api.example.com", TokenURL: "https://api.example.com/token", DefaultIdentity: config.IdentityUser}
			if err := runtime.Profiles.Add(profile); err != nil {
				t.Fatal(err)
			}
			runtime.DeviceCredentials = &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{
				profile.Name: {
					Token: &auth.Token{AccessToken: "old-token", ExpiresAt: time.Now().Add(time.Hour)}, TokenFirstInstallEventID: eventID,
				},
			}}
			if err := Execute(context.Background(), runtime, []string{"auth", "complete", "--profile", profile.Name, "--as", "user"}); !errors.Is(err, auth.ErrUserAuthentication) {
				t.Fatalf("旧 token 不应报告首次安装成功: output=%s err=%v", stdout.String(), err)
			}
			state, err := runtime.InstallState.Load()
			if err != nil || !state.AuthorizationRequired || state.EventID != "first-install-test" {
				t.Fatalf("旧 token 改变了门禁: state=%+v err=%v", state, err)
			}
		})
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
