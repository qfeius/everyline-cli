package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
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
	if token.AccessToken != "token-value" || !token.ExpiresAt.Equal(fixedNow.Add(2*time.Hour)) {
		t.Fatalf("token=%#v", token)
	}
	cached, err := store.Load("test")
	if err != nil || cached.AccessToken != "token-value" {
		t.Fatalf("cached=%#v err=%v", cached, err)
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

// TestProviderUserTokenFromEnvironment 验证自有认证页面交接的 user token 可通过环境变量直接使用。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestProviderUserTokenFromEnvironment(t *testing.T) {
	t.Setenv("EVERYLINE_USER_ACCESS_TOKEN", "user-env-token")
	fixedNow := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	provider := NewProvider(NewFileTokenStore(filepath.Join(t.TempDir(), "tokens.json")), http.DefaultClient, func() time.Time { return fixedNow })
	token, err := provider.TokenForIdentity(context.Background(), config.Profile{Name: "dev"}, config.IdentityUser)
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "user-env-token" || !token.ExpiresAt.Equal(fixedNow.Add(24*time.Hour)) {
		t.Fatalf("token=%#v", token)
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
