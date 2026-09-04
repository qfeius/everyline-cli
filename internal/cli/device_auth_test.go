package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/config"
)

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

// TestAuthDeviceInitRegistersOAuthClient 验证未配置 client_id 时，CLI 先通过开放平台动态注册并持久化返回值。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；请求协议、Device client 传递或持久化不正确时通过 t.Fatal 报告。
func TestAuthDeviceInitRegistersOAuthClient(t *testing.T) {
	var registrationRequest struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		Scope                   string   `json:"scope"`
	}
	var deviceClientID string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/open-api/v3/oauth/register/contract-review":
			if request.Method != http.MethodPost {
				t.Fatalf("registration method=%s", request.Method)
			}
			if request.Header.Get("Authorization") != "" {
				t.Fatalf("动态注册不应携带 Authorization: %q", request.Header.Get("Authorization"))
			}
			if err := json.NewDecoder(request.Body).Decode(&registrationRequest); err != nil {
				t.Fatal(err)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"client_id":"dynamic-client","client_id_issued_at":1788480000,"client_secret_expires_at":0,"token_endpoint_auth_method":"none"}`))
		case "/metadata":
			_, _ = writer.Write([]byte(`{"token_endpoint":"` + server.URL + `/token","device_authorization_endpoint":"` + server.URL + `/device"}`))
		case "/device":
			_ = request.ParseForm()
			deviceClientID = request.Form.Get("client_id")
			_, _ = writer.Write([]byte(`{"device_code":"private-device-code","user_code":"ABCD","verification_uri":"https://auth.example.com/device","verification_uri_complete":"https://auth.example.com/device?user_code=ABCD","expires_in":600}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	runtime, _, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	runtime.DeviceCredentials = &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{}}
	profile := config.Profile{
		Name: "test-user", BaseURL: server.URL, UserBaseURL: server.URL, TokenURL: server.URL + "/token",
		OAuthMetadataURL: server.URL + "/metadata", OAuthBusinessType: "contract-review",
		OAuthRedirectURL: "http://127.0.0.1:8000/login", OAuthScopes: []string{"contract-review:full"},
		DefaultIdentity: config.IdentityUser, DefaultOutput: "json",
	}
	if err := runtime.Profiles.Add(profile); err != nil {
		t.Fatal(err)
	}

	if err := Execute(context.Background(), runtime, []string{"auth", "init", "--profile", profile.Name, "--as", "user"}); err != nil {
		t.Fatal(err)
	}
	if registrationRequest.ClientName != "EveryLine CLI" ||
		strings.Join(registrationRequest.RedirectURIs, " ") != profile.OAuthRedirectURL ||
		strings.Join(registrationRequest.GrantTypes, " ") != "authorization_code" ||
		strings.Join(registrationRequest.ResponseTypes, " ") != "code" ||
		registrationRequest.TokenEndpointAuthMethod != "none" || registrationRequest.Scope != "contract-review:full" {
		t.Fatalf("registration request=%#v", registrationRequest)
	}
	if deviceClientID != "dynamic-client" {
		t.Fatalf("device client_id=%q", deviceClientID)
	}
	storedProfile, err := runtime.Profiles.Get(profile.Name)
	if err != nil || storedProfile.OAuthClientID != "dynamic-client" || storedProfile.OAuthDeviceClientID != "dynamic-client" {
		t.Fatalf("storedProfile=%#v err=%v", storedProfile, err)
	}
}

// TestAuthDeviceInitStopsWhenOAuthClientRegistrationFails 验证动态注册失败时不会使用内置 client_id 继续授权。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；注册失败被吞掉或 Device endpoint 被调用时通过 t.Fatal 报告。
func TestAuthDeviceInitStopsWhenOAuthClientRegistrationFails(t *testing.T) {
	deviceCalled := false
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/open-api/v3/oauth/register/contract-review":
			writer.WriteHeader(http.StatusServiceUnavailable)
		case "/metadata":
			_, _ = writer.Write([]byte(`{"token_endpoint":"` + server.URL + `/token","device_authorization_endpoint":"` + server.URL + `/device"}`))
		case "/device":
			deviceCalled = true
			writer.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(writer, request)
		}
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
	if err == nil || !strings.Contains(err.Error(), "注册 OAuth client 失败: http=503") {
		t.Fatalf("err=%v", err)
	}
	if deviceCalled {
		t.Fatal("动态注册失败后不应调用 Device Authorization endpoint")
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
