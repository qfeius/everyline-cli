package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/config"
)

// memoryDeviceCredentialStore 为 CLI Device 授权测试提供线程安全内存存储。
type memoryDeviceCredentialStore struct {
	mu          sync.Mutex
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
	deviceStore := &memoryDeviceCredentialStore{credentials: map[string]auth.DeviceCredential{}}
	runtime.DeviceCredentials = deviceStore
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

var _ auth.DeviceCredentialStore = (*memoryDeviceCredentialStore)(nil)
