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
