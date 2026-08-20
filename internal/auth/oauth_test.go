package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/config"
)

type fakeOAuthCallback struct{}

func (fakeOAuthCallback) Wait(context.Context, string) (string, error) {
	return "authorization-code", nil
}
func (fakeOAuthCallback) Close() {}

// TestLoginUserOAuthNoOpenBrowserPrintsLink 验证手动复制链接模式仍输出 PKCE 授权链接且不启动浏览器。
func TestLoginUserOAuthNoOpenBrowserPrintsLink(t *testing.T) {
	server := newOAuthTestServer(t)
	defer server.Close()
	profile := config.Profile{
		Name:              "dev",
		OAuthMetadataURL:  server.URL + "/metadata",
		OAuthBusinessType: "contract-review",
		OAuthClientID:     "oauth-client",
		OAuthRedirectURL:  "http://127.0.0.1:8000/login",
		OAuthScopes:       []string{"contract-review:full"},
	}
	var output bytes.Buffer
	opened := false
	fixedNow := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	token, err := LoginUserOAuth(context.Background(), profile, OAuthLoginOptions{
		HTTPClient: server.Client(),
		Now:        func() time.Time { return fixedNow },
		Output:     &output,
		OpenBrowser: func(string) error {
			opened = true
			return nil
		},
		NoOpenBrowser: true,
		StartCallback: func(string) (OAuthCallback, error) { return fakeOAuthCallback{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened || !strings.Contains(output.String(), "请在浏览器中完成用户授权") || !strings.Contains(output.String(), "code_challenge") {
		t.Fatalf("opened=%t output=%s", opened, output.String())
	}
	if token.AccessToken != "oauth-token" || !token.ExpiresAt.Equal(fixedNow.Add(time.Hour)) {
		t.Fatalf("token=%#v", token)
	}
	serialized, err := json.Marshal(token)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(serialized), `"issued_at"`) {
		t.Fatalf("token serialization missing issued_at: %s", serialized)
	}
}

func newOAuthTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/metadata":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"authorization_endpoint":           "https://auth.example.com/authorize",
				"token_endpoint":                   server.URL + "/token",
				"code_challenge_methods_supported": []string{"S256"},
			})
		case "/token":
			_ = request.ParseForm()
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"access_token":  "oauth-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": "refresh-token",
				"scope":         request.Form.Get("scope"),
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	return server
}

// TestStartOAuthCallbackServerRejectsStateMismatch 验证 loopback callback 不接受其它 OAuth 会话的 state。
func TestStartOAuthCallbackServerRejectsStateMismatch(t *testing.T) {
	callback, callbackURL := newTestOAuthCallback(t)
	defer callback.Close()
	requestErrors := make(chan error, 1)
	go func() {
		requestErrors <- requestOAuthCallback(callbackURL, url.Values{"code": {"code"}, "state": {"wrong-state"}})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := callback.Wait(ctx, "expected-state"); err == nil || !strings.Contains(err.Error(), "state 不匹配") {
		t.Fatalf("err=%v", err)
	}
	if err := <-requestErrors; err != nil {
		t.Fatal(err)
	}
}

// TestStartOAuthCallbackServerPropagatesOAuthError 验证授权服务返回 error 时不进入 token exchange。
func TestStartOAuthCallbackServerPropagatesOAuthError(t *testing.T) {
	callback, callbackURL := newTestOAuthCallback(t)
	defer callback.Close()
	requestErrors := make(chan error, 1)
	go func() {
		requestErrors <- requestOAuthCallback(callbackURL, url.Values{"error": {"access_denied"}, "state": {"expected-state"}})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := callback.Wait(ctx, "expected-state"); err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err=%v", err)
	}
	if err := <-requestErrors; err != nil {
		t.Fatal(err)
	}
}

func newTestOAuthCallback(t *testing.T) (OAuthCallback, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/login", port)
	callback, err := StartOAuthCallbackServer(callbackURL)
	if err != nil {
		t.Fatal(err)
	}
	return callback, callbackURL
}

func requestOAuthCallback(callbackURL string, query url.Values) error {
	parsed, err := url.Parse(callbackURL)
	if err != nil {
		return err
	}
	parsed.RawQuery = query.Encode()
	for attempt := 0; attempt < 20; attempt++ {
		response, requestErr := http.Get(parsed.String())
		if requestErr == nil {
			_ = response.Body.Close()
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("callback request failed: %s", parsed.String())
}
