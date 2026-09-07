package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/config"
)

// TestProviderLoginContract 验证 token 请求使用 appId/appSecret、code=0，并缓存绝对过期时间。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestProviderLoginContract(t *testing.T) {
	fixedNow := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/token" {
			t.Errorf("method/path=%s %s", request.Method, request.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["appId"] != "cli-test" || body["appSecret"] != "secret-value" || len(body) != 2 {
			t.Errorf("token body=%#v", body)
		}
		_, _ = writer.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"token-value","expire":7200}`))
	}))
	defer server.Close()
	store := NewFileTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	provider := NewProvider(store, server.Client(), func() time.Time { return fixedNow })
	profile := config.Profile{Name: "test", TokenURL: server.URL + "/token", AppID: "cli-test"}
	token, err := provider.Login(context.Background(), profile, "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "token-value" || token.TokenType != "Bearer" || !token.IssuedAt.Equal(fixedNow) || !token.ExpiresAt.Equal(fixedNow.Add(2*time.Hour)) {
		t.Fatalf("token=%#v", token)
	}
	cached, err := store.Load("test")
	if err != nil || cached.AccessToken != "token-value" {
		t.Fatalf("cached=%#v err=%v", cached, err)
	}
}

// TestProviderLoginUsesEnvironmentAppID 验证 app token 刷新链路也遵守环境变量优先于 Profile 的 app-id 规则。
func TestProviderLoginUsesEnvironmentAppID(t *testing.T) {
	t.Setenv("EVERYLINE_APP_ID", "env-app")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["appId"] != "env-app" || body["appSecret"] != "secret-value" {
			t.Fatalf("token body=%#v", body)
		}
		_, _ = writer.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"token-value","expire":7200}`))
	}))
	defer server.Close()

	provider := NewProvider(NewFileTokenStore(filepath.Join(t.TempDir(), "tokens.json")), server.Client(), time.Now)
	profile := config.Profile{Name: "test", TokenURL: server.URL + "/token", AppID: "profile-app"}
	if _, err := provider.Login(context.Background(), profile, "secret-value"); err != nil {
		t.Fatal(err)
	}
}

// TestFileTokenStoreConcurrentInstances 验证多个独立 token 仓库实例并发保存时不会互相覆盖。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestFileTokenStoreConcurrentInstances(t *testing.T) {
	const tokenCount = 40
	path := filepath.Join(t.TempDir(), "tokens.json")
	start := make(chan struct{})
	errors := make(chan error, tokenCount)
	var waitGroup sync.WaitGroup
	for index := 0; index < tokenCount; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			<-start
			profileName := fmt.Sprintf("profile-%02d", index)
			errors <- NewFileTokenStore(path).Save(profileName, Token{AccessToken: profileName})
		}(index)
	}
	close(start)
	waitGroup.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < tokenCount; index++ {
		profileName := fmt.Sprintf("profile-%02d", index)
		token, err := NewFileTokenStore(path).Load(profileName)
		if err != nil || token.AccessToken != profileName {
			t.Fatalf("profile=%s token=%#v err=%v", profileName, token, err)
		}
	}
}

// TestIdentityTokenCacheIsolated 验证 app/user token 使用不同缓存 key，互相注销不会覆盖。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestIdentityTokenCacheIsolated(t *testing.T) {
	store := NewFileTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	appToken := Token{AccessToken: "app-token", ExpiresAt: time.Now().Add(time.Hour)}
	userToken := Token{AccessToken: "user-token", ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.SaveForIdentity("dev", config.IdentityApp, appToken); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveForIdentity("dev", config.IdentityUser, userToken); err != nil {
		t.Fatal(err)
	}
	if got, err := store.LoadForIdentity("dev", config.IdentityApp); err != nil || got.AccessToken != appToken.AccessToken {
		t.Fatalf("app token=%#v err=%v", got, err)
	}
	if got, err := store.LoadForIdentity("dev", config.IdentityUser); err != nil || got.AccessToken != userToken.AccessToken {
		t.Fatalf("user token=%#v err=%v", got, err)
	}
	if err := store.DeleteForIdentity("dev", config.IdentityUser); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadForIdentity("dev", config.IdentityApp); err != nil {
		t.Fatalf("user logout 影响 app token: %v", err)
	}
}

// TestProviderUserTokenFromEnvironment 验证 user Provider 不再接受原始 token 环境变量。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestProviderUserTokenFromEnvironment(t *testing.T) {
	t.Setenv("EVERYLINE_USER_ACCESS_TOKEN", "user-env-token")
	provider := NewProvider(NewFileTokenStore(filepath.Join(t.TempDir(), "tokens.json")), http.DefaultClient, time.Now)
	if _, err := provider.TokenForIdentity(context.Background(), config.Profile{Name: "dev"}, config.IdentityUser); !errors.Is(err, ErrUserAuthentication) {
		t.Fatalf("err=%v", err)
	}
}

// TestFileTokenStoreLoadsLegacyTokenWithoutLifecycleFields 验证旧版 tokens.json 缺少生命周期字段时仍可读取。
func TestFileTokenStoreLoadsLegacyTokenWithoutLifecycleFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	if err := os.WriteFile(path, []byte(`{"dev":{"access_token":"legacy-token"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := NewFileTokenStore(path).Load("dev")
	if err != nil || token.AccessToken != "legacy-token" || !token.ValidAt(time.Now(), 0) {
		t.Fatalf("token=%#v err=%v", token, err)
	}
}

// TestAppIDFromEnvironmentFallback 验证没有 Profile 专用变量时使用通用 app-id 环境变量。
func TestAppIDFromEnvironmentFallback(t *testing.T) {
	t.Setenv("EVERYLINE_APP_ID", "generic-app")
	if got := AppIDFromEnvironment("dev"); got != "generic-app" {
		t.Fatalf("app id=%q, want generic-app", got)
	}
}

// TestProfileSecretEnvironmentKeysDoNotCollide 验证点、横线和下划线 Profile 使用不同的专用密钥变量。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestProfileSecretEnvironmentKeysDoNotCollide(t *testing.T) {
	t.Setenv("EVERYLINE_APP_SECRET_PROD_2DEU", "hyphen-secret")
	t.Setenv("EVERYLINE_APP_SECRET_PROD_2EEU", "dot-secret")
	t.Setenv("EVERYLINE_APP_SECRET_PROD_5FEU", "underscore-secret")
	t.Setenv("EVERYLINE_APP_SECRET__50_52_4F_44_2D_45_55", "uppercase-secret")
	tests := map[string]string{
		"prod-eu": "hyphen-secret",
		"prod.eu": "dot-secret",
		"prod_eu": "underscore-secret",
		"PROD-EU": "uppercase-secret",
	}
	for profileName, expected := range tests {
		if actual := SecretFromEnvironment(profileName); actual != expected {
			t.Errorf("profile=%s secret=%q，期望 %q", profileName, actual, expected)
		}
	}
}

// TestProviderConcurrentFileRefreshReusesNewToken 验证后进入刷新锁的请求复用并发刷新结果，而不是把已更新 token 当作错误。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；重复刷新、并发请求失败或缓存未更新时通过 t.Fatal 报告。
func TestProviderConcurrentFileRefreshReusesNewToken(t *testing.T) {
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRefresh) }) }
	t.Cleanup(release)
	var refreshCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/metadata":
			_, _ = writer.Write([]byte(`{"token_endpoint":"` + server.URL + `/token","grant_types_supported":["refresh_token"]}`))
		case "/token":
			if err := request.ParseForm(); err != nil || request.Form.Get("client_id") != "issuing-client" {
				http.Error(writer, "wrong client", http.StatusBadRequest)
				return
			}
			if refreshCalls.Add(1) == 1 {
				close(refreshStarted)
				<-releaseRefresh
			}
			_, _ = writer.Write([]byte(`{"access_token":"new-user-token","token_type":"Bearer","refresh_token":"new-refresh","expires_in":3600}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	tokenPath := filepath.Join(t.TempDir(), "tokens.json")
	firstStore := NewFileTokenStore(tokenPath)
	secondStore := NewFileTokenStore(tokenPath)
	oldToken := Token{
		AccessToken: "old-user-token", RefreshToken: "old-refresh", OAuthClientID: "issuing-client",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := firstStore.SaveForIdentity("test-user", config.IdentityUser, oldToken); err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{Name: "test-user", OAuthMetadataURL: server.URL + "/metadata", OAuthClientID: "newer-profile-client"}
	providers := []*Provider{
		NewProvider(firstStore, server.Client(), time.Now),
		NewProvider(secondStore, server.Client(), time.Now),
	}
	type refreshResult struct {
		token Token
		err   error
	}
	results := make(chan refreshResult, len(providers))
	go func() {
		token, err := providers[0].RefreshForIdentity(context.Background(), profile, config.IdentityUser, oldToken.AccessToken)
		results <- refreshResult{token: token, err: err}
	}()
	select {
	case <-refreshStarted:
	case <-time.After(time.Second):
		t.Fatal("首个 refresh 请求未按时发出")
	}
	go func() {
		token, err := providers[1].RefreshForIdentity(context.Background(), profile, config.IdentityUser, oldToken.AccessToken)
		results <- refreshResult{token: token, err: err}
	}()
	release()

	for range providers {
		select {
		case result := <-results:
			if result.err != nil || result.token.AccessToken != "new-user-token" || result.token.OAuthClientID != "issuing-client" {
				t.Fatalf("并发 refresh 结果=%#v err=%v", result.token, result.err)
			}
		case <-time.After(time.Second):
			t.Fatal("并发 refresh 未按时完成")
		}
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("refreshCalls=%d，期望只刷新一次", refreshCalls.Load())
	}
}

// TestProviderDeviceRefreshReusesConcurrentResult 验证进入 Device 刷新锁后发现 token 已更新时直接复用新凭证。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；重新发起网络刷新或未返回新 token 时通过 t.Fatal 报告。
func TestProviderDeviceRefreshReusesConcurrentResult(t *testing.T) {
	store := &encryptedDeviceStore{dir: t.TempDir(), key: make([]byte, 32)}
	if err := store.Save("test-user", DeviceCredential{Token: &Token{AccessToken: "new-device-token"}}); err != nil {
		t.Fatal(err)
	}
	provider := NewProvider(NewFileTokenStore(filepath.Join(t.TempDir(), "tokens.json")), http.DefaultClient, time.Now).
		WithDeviceCredentials(store)

	got, err := provider.refreshDeviceUserToken(context.Background(), config.Profile{Name: "test-user"}, "old-device-token")
	if err != nil || got.AccessToken != "new-device-token" {
		t.Fatalf("token=%#v err=%v", got, err)
	}
}

// TestProviderDeviceMissingCredentialsUsesDeviceGrantHint 验证沙箱缺少 user 凭证时只提示 Device Grant 入口。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；错误类型或下一步命令仍指向 loopback 登录时通过 t.Fatal 报告。
func TestProviderDeviceMissingCredentialsUsesDeviceGrantHint(t *testing.T) {
	store := &encryptedDeviceStore{dir: t.TempDir(), key: make([]byte, 32)}
	provider := NewProvider(NewFileTokenStore(filepath.Join(t.TempDir(), "tokens.json")), http.DefaultClient, time.Now).
		WithDeviceCredentials(store)

	_, err := provider.TokenForIdentity(context.Background(), config.Profile{Name: "test-user"}, config.IdentityUser)
	if !errors.Is(err, ErrUserAuthentication) {
		t.Fatalf("err=%v，期望 user 鉴权错误", err)
	}
	if !strings.Contains(err.Error(), "auth init --profile test-user --as user --output json") || strings.Contains(err.Error(), "auth login") {
		t.Fatalf("沙箱鉴权提示未固定使用 Device Grant: %v", err)
	}
}
