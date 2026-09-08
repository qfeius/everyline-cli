package auth

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

// TestOAuthCallbackResponse 验证浏览器回调页区分授权成功、用户拒绝和协议错误。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；页面状态、提示或终端结果不符合回调内容时报告失败。
func TestOAuthCallbackResponse(t *testing.T) {
	tests := []struct {
		name       string
		query      url.Values
		wantStatus int
		wantBody   string
		wantError  string
	}{
		{
			name:       "denied without state",
			query:      url.Values{"error": {"access_denied"}, "error_description": {"用户拒绝授权"}},
			wantStatus: http.StatusForbidden, wantBody: "用户已拒绝授权", wantError: "access_denied（用户拒绝授权）",
		},
		{
			name:       "denied with state",
			query:      url.Values{"error": {"access_denied"}, "state": {"expected-state"}},
			wantStatus: http.StatusForbidden, wantBody: "用户已拒绝授权", wantError: "access_denied",
		},
		{
			name:       "provider error",
			query:      url.Values{"error": {"server_error"}, "error_description": {"服务暂时不可用"}},
			wantStatus: http.StatusBadRequest, wantBody: "OAuth 授权失败", wantError: "server_error（服务暂时不可用）",
		},
		{
			name: "missing code", query: url.Values{"state": {"expected-state"}},
			wantStatus: http.StatusBadRequest, wantBody: "缺少 code 或 state", wantError: "缺少 code 或 state",
		},
		{
			name: "missing state", query: url.Values{"code": {"authorization-code"}},
			wantStatus: http.StatusBadRequest, wantBody: "缺少 code 或 state", wantError: "缺少 code 或 state",
		},
		{
			name: "success", query: url.Values{"code": {"authorization-code"}, "state": {"expected-state"}},
			wantStatus: http.StatusOK, wantBody: oauthCallbackMessage,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			callback, callbackURL := newTestOAuthCallback(t)
			defer callback.Close()
			// 使用真实 loopback 请求覆盖截图中的 /login 路径及缺少 state 的拒绝回调。
			response, err := (&http.Client{Timeout: time.Second}).Get(callbackURL + "?" + test.query.Encode())
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != test.wantStatus || !strings.Contains(string(body), test.wantBody) {
				t.Fatalf("status=%d body=%s", response.StatusCode, body)
			}
			if response.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
				t.Fatalf("content type=%s", response.Header.Get("Content-Type"))
			}
			if test.wantError != "" && strings.Contains(string(body), oauthCallbackMessage) {
				t.Fatalf("失败回调显示了授权成功提示: %s", body)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			code, err := callback.Wait(ctx, "expected-state")
			if test.wantError == "" {
				if err != nil || code != "authorization-code" {
					t.Fatalf("code=%q err=%v", code, err)
				}
			} else if code != "" || err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("code=%q err=%v", code, err)
			}
		})
	}
}

// TestOAuthCallbackCloseWaitsForResponse 验证登录结束关闭服务时仍完整发送浏览器响应。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；关闭操作提前中断 HTTP 响应时报告失败。
func TestOAuthCallbackCloseWaitsForResponse(t *testing.T) {
	callback := &loopbackOAuthCallback{results: make(chan oauthCallbackResult, 1)}
	// 暂停 handler 的返回以确定性复现：CLI 已收到拒绝结果，HTTP 响应尚未发送完毕。
	finishHandler := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		callback.handle(writer, request)
		<-finishHandler
	}))
	defer server.Close()
	callback.server = server.Config
	requestErrors := make(chan error, 1)
	go func() {
		response, err := (&http.Client{Timeout: 2 * time.Second}).Get(server.URL + "?error=access_denied")
		if err == nil {
			defer response.Body.Close()
			var body []byte
			body, err = io.ReadAll(response.Body)
			if err == nil && !strings.Contains(string(body), "用户已拒绝授权") {
				err = fmt.Errorf("unexpected callback body: %s", body)
			}
		}
		requestErrors <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, waitErr := callback.Wait(ctx, "expected-state")
	closed := make(chan struct{})
	go func() {
		callback.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Error("callback server closed before its response handler finished")
	case <-time.After(50 * time.Millisecond):
	}
	close(finishHandler)
	<-closed
	if waitErr == nil || !strings.Contains(waitErr.Error(), "access_denied") {
		t.Fatalf("err=%v", waitErr)
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

/*
TestOAuthCallbackTimeoutPage 验证登录期限后回调展示准确文案且不返回授权码。
入参：t *testing.T 为测试上下文。
返回值：无；迟到授权被接受、响应错误或服务未释放时报错。
*/
func TestOAuthCallbackTimeoutPage(t *testing.T) {
	server, callbackURL := newTestOAuthCallback(t)
	callback := server.(*loopbackOAuthCallback)
	defer callback.Close()
	callback.timeoutPageTTL = time.Second
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		code, err := callback.Wait(ctx, "expected-state")
		if code != "" {
			finished <- fmt.Errorf("late code accepted: %s", code)
			return
		}
		callback.Close()
		finished <- err
	}()
	// 等待登录线程登记期限，再模拟浏览器迟到的真实 HTTP 回调。
	readyDeadline := time.Now().Add(time.Second)
	for {
		callback.mu.RLock()
		ready := callback.waitContext != nil
		callback.mu.RUnlock()
		if ready {
			break
		}
		if time.Now().After(readyDeadline) {
			t.Fatal("callback wait did not start")
		}
		time.Sleep(time.Millisecond)
	}
	response, err := (&http.Client{Timeout: time.Second}).Get(callbackURL + "?code=late-code&state=expected-state")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusGone || string(body) != "授权链接已超时，请重新生成授权链接" {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if response.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatal("incorrect content type")
	}
	select {
	case err := <-finished:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("late callback did not finish login")
	}
}

/*
TestOAuthCallbackTimeoutPageRetention 验证没有浏览器回调时页面保留有上限，取消不进入保留期。
入参：t *testing.T 为测试上下文。
返回值：无；等待未结束或返回错误类型不正确时报错。
*/
func TestOAuthCallbackTimeoutPageRetention(t *testing.T) {
	for _, expired := range []bool{true, false} {
		callback := &loopbackOAuthCallback{results: make(chan oauthCallbackResult, 1), timeoutPageTTL: 20 * time.Millisecond}
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
		wantError := context.Canceled
		if expired {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			wantError = context.DeadlineExceeded
		} else {
			cancel()
		}
		started := time.Now()
		_, err := callback.Wait(ctx, "expected-state")
		cancel()
		if !errors.Is(err, wantError) {
			t.Fatalf("err=%v", err)
		}
		if expired && time.Since(started) < callback.timeoutPageTTL {
			t.Fatal("timeout page closed too early")
		}
	}
}
