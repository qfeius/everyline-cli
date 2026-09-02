package openplatform

import (
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
	"git.qtech.cn/ai/everyline-cli/internal/contracts"
)

// staticTokenProvider 为 HTTP 契约测试提供固定 token。
type staticTokenProvider struct{}

// Token 返回不参与刷新逻辑的固定凭证。
// 入参：context.Context 和 config.Profile 仅满足接口。
// 返回值：auth.Token 为测试凭证；error 始终为 nil。
func (staticTokenProvider) Token(context.Context, config.Profile) (auth.Token, error) {
	return auth.Token{AccessToken: "token-test", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// identityTokenProvider 为 user/app 分流测试提供身份可见的固定 token。
type identityTokenProvider struct{}

// Token 返回兼容旧接口的 app 测试凭证。
// 入参：context.Context 和 config.Profile 仅满足旧 TokenProvider 接口。
// 返回值：auth.Token 为 app 测试凭证；error 始终为 nil。
func (identityTokenProvider) Token(context.Context, config.Profile) (auth.Token, error) {
	return auth.Token{AccessToken: "app-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// TokenForIdentity 返回带身份区分的测试凭证，验证 Client 不会把 user 请求降级到 app token。
// 入参：context.Context 为请求上下文；config.Profile 为当前环境；identity 为业务身份。
// 返回值：auth.Token 为对应身份凭证；error 始终为 nil。
func (identityTokenProvider) TokenForIdentity(context.Context, config.Profile, config.IdentityKind) (auth.Token, error) {
	return auth.Token{AccessToken: "user-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// blockingTokenProvider 模拟等待取消的远端 token 刷新。
type blockingTokenProvider struct{}

// Token 等待调用上下文结束，用于验证 token 刷新也受 operation 超时约束。
// 入参：ctx context.Context 为超时上下文；config.Profile 仅满足接口。
// 返回值：auth.Token 为空；error 为 ctx.Err()。
func (blockingTokenProvider) Token(ctx context.Context, _ config.Profile) (auth.Token, error) {
	<-ctx.Done()
	return auth.Token{}, ctx.Err()
}

// TestClientSuccessContract 验证相对路径、query、Bearer header 和 code=200 解封装。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestClientSuccessContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/open-apis/contract-review/v3/smartAudit/task/status" {
			t.Errorf("method/path=%s %s", request.Method, request.URL.Path)
		}
		if request.URL.Query().Get("taskId") != "42" {
			t.Errorf("query=%s", request.URL.RawQuery)
		}
		if request.URL.Query().Get("businessId") != "biz-42" {
			t.Errorf("query=%s", request.URL.RawQuery)
		}
		if request.Header.Get("Authorization") != "Bearer token-test" {
			t.Errorf("authorization=%q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("X-Request-Id", "req-1")
		_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":{"taskId":42,"status":"running"}}`))
	}))
	defer server.Close()
	client := NewClient(config.Profile{BaseURL: server.URL}, staticTokenProvider{}, server.Client())
	response, err := client.Do(context.Background(), Request{
		OperationID:   "smartAuditTaskStatus",
		Method:        http.MethodGet,
		Path:          "/open-apis/contract-review/v3/smartAudit/task/status",
		ContractInput: map[string]any{"taskId": 42, "businessId": "biz-42"},
		Query:         url.Values{"taskId": []string{"42"}, "businessId": []string{"biz-42"}},
		SuccessCode:   200,
	})
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(response.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data["status"] != "running" || response.RequestID != "req-1" {
		t.Fatalf("response=%#v requestID=%s", data, response.RequestID)
	}
}

// TestClientUserIdentityRoute 验证 user 身份选择 UserBaseURL、UserPath 和 user token。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestClientUserIdentityRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/open-apis/user/review-checklists" {
			t.Errorf("user path=%s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer user-token" {
			t.Errorf("user authorization=%q", request.Header.Get("Authorization"))
		}
		_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":[]}`))
	}))
	defer server.Close()
	client := NewClientForIdentity(config.Profile{BaseURL: "https://app.example.com", UserBaseURL: server.URL}, identityTokenProvider{}, server.Client(), config.IdentityUser)
	_, err := client.Do(context.Background(), Request{
		OperationID:   "listReviewChecklists",
		Method:        http.MethodGet,
		Path:          "/open-apis/review-rules/review-checklists",
		UserPath:      "/open-apis/user/review-checklists",
		ContractInput: map[string]any{},
		SuccessCode:   200,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestClientAPIError 验证 429 保留业务码、request ID、Retry-After 和可重试属性。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestClientAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Request-Id", "req-rate")
		writer.Header().Set("Retry-After", "3")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"code":429001,"msg":"rate limited","data":null}`))
	}))
	defer server.Close()
	client := NewClient(config.Profile{BaseURL: server.URL}, staticTokenProvider{}, server.Client())
	_, err := client.Do(context.Background(), Request{
		OperationID:   "createReviewChecklist",
		Method:        http.MethodPost,
		Path:          "/open-apis/review-rules/review-checklists",
		ContractInput: map[string]any{"name": "清单", "reviewRuleIds": []string{"rule-1"}},
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          []byte(`{"name":"清单","reviewRuleIds":["rule-1"]}`),
		SuccessCode:   200,
	})
	var apiError *APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("err=%T %v", err, err)
	}
	if apiError.Code != "429001" || apiError.RequestID != "req-rate" || apiError.RetryAfter != 3*time.Second || !apiError.Retryable {
		t.Fatalf("apiError=%#v", apiError)
	}
}

// TestClientInvalidatesRevokedUserSession 验证 110004 会清理当前 Profile 的 user token，同时保留 app token。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失效范围、错误提示或 API 定位信息不正确时通过 t.Fatal 报告。
func TestClientInvalidatesRevokedUserSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Request-Id", "req-expired")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"code":110004,"msg":"token验证失败","data":null}`))
	}))
	defer server.Close()

	store := auth.NewFileTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err := store.SaveForIdentity("test-user", config.IdentityApp, auth.Token{AccessToken: "app-token"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveForIdentity("test-user", config.IdentityUser, auth.Token{AccessToken: "user-token"}); err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{Name: "test-user", BaseURL: server.URL, UserBaseURL: server.URL}
	provider := auth.NewProvider(store, server.Client(), time.Now)
	client := NewClientForIdentity(profile, provider, server.Client(), config.IdentityUser)
	_, err := client.Do(context.Background(), Request{
		OperationID: "listReviewChecklists", Method: http.MethodGet, Path: "/open-apis/review-rules/review-checklists", ContractInput: map[string]any{},
	})
	if !errors.Is(err, auth.ErrUserSessionExpired) || !strings.Contains(err.Error(), "req-expired") {
		t.Fatalf("err=%v，期望重新授权提示和 request ID", err)
	}
	if _, loadErr := store.LoadForIdentity("test-user", config.IdentityUser); !errors.Is(loadErr, os.ErrNotExist) {
		t.Fatalf("user token 未失效: %v", loadErr)
	}
	if appToken, loadErr := store.LoadForIdentity("test-user", config.IdentityApp); loadErr != nil || appToken.AccessToken != "app-token" {
		t.Fatalf("app token 被误删: token=%#v err=%v", appToken, loadErr)
	}
}

// TestClientPreservesReauthenticatedUserSession 验证旧请求返回 110004 时不会删除请求期间重新登录写入的新 user token。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；请求时序、错误类型或新 token 保留状态不符合预期时通过 t.Fatal 报告。
func TestClientPreservesReauthenticatedUserSession(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseResponse := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseResponse) })
	}
	defer release()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-releaseResponse
		writer.Header().Set("X-Request-Id", "req-stale-session")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"code":110004,"msg":"token验证失败","data":null}`))
	}))
	defer server.Close()

	tokenPath := filepath.Join(t.TempDir(), "tokens.json")
	requestStore := auth.NewFileTokenStore(tokenPath)
	loginStore := auth.NewFileTokenStore(tokenPath)
	if err := requestStore.SaveForIdentity("test-user", config.IdentityUser, auth.Token{AccessToken: "old-token"}); err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{Name: "test-user", BaseURL: server.URL, UserBaseURL: server.URL}
	provider := auth.NewProvider(requestStore, server.Client(), time.Now)
	client := NewClientForIdentity(profile, provider, server.Client(), config.IdentityUser)
	requestResult := make(chan error, 1)
	go func() {
		_, err := client.Do(context.Background(), Request{
			OperationID: "listReviewChecklists", Method: http.MethodGet, Path: "/open-apis/review-rules/review-checklists", ContractInput: map[string]any{},
		})
		requestResult <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("旧 token 请求未按时发出")
	}
	// 模拟另一个 CLI 进程在旧请求返回前完成重新授权并写入新 token。
	if err := loginStore.SaveForIdentity("test-user", config.IdentityUser, auth.Token{AccessToken: "new-token"}); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-requestResult:
		if !errors.Is(err, auth.ErrUserSessionExpired) {
			t.Fatalf("err=%v，期望旧请求报告登录失效", err)
		}
	case <-time.After(time.Second):
		t.Fatal("旧请求未按时返回")
	}
	if token, err := loginStore.LoadForIdentity("test-user", config.IdentityUser); err != nil || token.AccessToken != "new-token" {
		t.Fatalf("新 user token 被旧请求误删: token=%#v err=%v", token, err)
	}
}

// TestClientRefreshesAndReplaysTrustedExpiredUserSession 验证 110004 只触发一次 refresh_token grant 并原样重放业务请求。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；刷新次数、Bearer 更新或缓存写入不正确时通过 t.Fatal 报告。
func TestClientRefreshesAndReplaysTrustedExpiredUserSession(t *testing.T) {
	var businessAttempts atomic.Int32
	var refreshAttempts atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/metadata":
			_, _ = writer.Write([]byte(`{"token_endpoint":"` + server.URL + `/token"}`))
		case "/token":
			refreshAttempts.Add(1)
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("grant_type") != "refresh_token" || request.Form.Get("refresh_token") != "old-refresh" || request.Form.Get("client_id") != "oauth-client" {
				t.Errorf("refresh form=%v", request.Form)
			}
			_, _ = writer.Write([]byte(`{"access_token":"new-user-token","token_type":"Bearer","refresh_token":"new-refresh","expires_in":3600}`))
		case "/open-apis/review-rules/review-checklists":
			businessAttempts.Add(1)
			if request.Header.Get("Authorization") == "Bearer old-user-token" {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte(`{"code":110004,"msg":"token验证失败","data":null}`))
				return
			}
			if request.Header.Get("Authorization") != "Bearer new-user-token" {
				t.Errorf("authorization=%q", request.Header.Get("Authorization"))
			}
			_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":[]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	store := auth.NewFileTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err := store.SaveForIdentity("test-user", config.IdentityUser, auth.Token{
		AccessToken: "old-user-token", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{
		Name: "test-user", BaseURL: server.URL, UserBaseURL: server.URL,
		OAuthMetadataURL: server.URL + "/metadata", OAuthClientID: "oauth-client",
	}
	provider := auth.NewProvider(store, server.Client(), time.Now)
	client := NewClientForIdentity(profile, provider, server.Client(), config.IdentityUser)
	if _, err := client.Do(context.Background(), Request{
		OperationID: "listReviewChecklists", Method: http.MethodGet,
		Path: "/open-apis/review-rules/review-checklists", ContractInput: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	if businessAttempts.Load() != 2 || refreshAttempts.Load() != 1 {
		t.Fatalf("businessAttempts=%d refreshAttempts=%d", businessAttempts.Load(), refreshAttempts.Load())
	}
	stored, err := store.LoadForIdentity("test-user", config.IdentityUser)
	if err != nil || stored.AccessToken != "new-user-token" || stored.RefreshToken != "new-refresh" {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
}

// TestClientRetriesNonJSONRateLimit 验证 GET 在网关返回非 JSON 429 时仍按安全重试策略继续执行。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestClientRetriesNonJSONRateLimit(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if attempts.Add(1) == 1 {
			writer.Header().Set("Retry-After", "1")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte("rate limited"))
			return
		}
		_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":[]}`))
	}))
	defer server.Close()
	client := NewClient(config.Profile{BaseURL: server.URL}, staticTokenProvider{}, server.Client())
	// 测试只验证重试决策，不等待真实 Retry-After 时长。
	client.sleep = func(context.Context, time.Duration) error { return nil }
	_, err := client.Do(context.Background(), Request{
		OperationID: "listReviewChecklists", Method: http.MethodGet, Path: "/open-apis/review-rules/review-checklists", ContractInput: map[string]any{}, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts=%d，期望非 JSON 429 后重试一次", attempts.Load())
	}
}

// TestParseRetryAfterHTTPDate 验证标准 HTTP-date 形式也会转换为正的退避时间。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestParseRetryAfterHTTPDate(t *testing.T) {
	value := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
	delay := parseRetryAfter(value)
	if delay < 3*time.Second || delay > 5*time.Second {
		t.Fatalf("delay=%s，期望解析 HTTP-date Retry-After", delay)
	}
}

// TestClientRejectsAbsolutePath 验证业务 Client 不会把 token 发送到任意 URL。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestClientRejectsAbsolutePath(t *testing.T) {
	client := NewClient(config.Profile{BaseURL: "https://open.qfei.cn"}, staticTokenProvider{}, nil)
	_, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "https://evil.example/open-apis/test"})
	if err == nil {
		t.Fatal("期望绝对 URL 被拒绝")
	}
}

// TestClientTimesOutTokenRefresh 验证获取 token 不会绕过业务操作超时。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestClientTimesOutTokenRefresh(t *testing.T) {
	client := NewClient(config.Profile{BaseURL: "https://open.qfei.cn"}, blockingTokenProvider{}, nil)
	started := time.Now()
	_, err := client.Do(context.Background(), Request{OperationID: "listReviewChecklists", Method: http.MethodGet, Path: "/open-apis/review-rules/review-checklists", ContractInput: map[string]any{}, Timeout: 20 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v，期望 context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("token 超时未及时生效: %s", elapsed)
	}
}

// TestClientTimeoutCoversRetries 验证 Retry-After 和多次 GET 重试不能突破整次操作超时。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestClientTimeoutCoversRetries(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts.Add(1)
		writer.Header().Set("Retry-After", "5")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte(`{"code":503001,"msg":"busy","data":null}`))
	}))
	defer server.Close()
	client := NewClient(config.Profile{BaseURL: server.URL}, staticTokenProvider{}, server.Client())
	started := time.Now()
	_, err := client.Do(context.Background(), Request{
		OperationID: "listReviewChecklists", Method: http.MethodGet, Path: "/open-apis/review-rules/review-checklists", ContractInput: map[string]any{}, Timeout: 40 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v，期望 context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("重试突破全局超时: %s", elapsed)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts=%d，Retry-After 等待期间不应发起第二次请求", attempts.Load())
	}
}

// TestClientRejectsCatalogDrift 验证 HTTP Adapter 在发送 token 前拒绝 method/path 与契约目录不一致的请求。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestClientRejectsCatalogDrift(t *testing.T) {
	client := NewClient(config.Profile{BaseURL: "https://open.qfei.cn"}, staticTokenProvider{}, nil)
	_, err := client.Do(context.Background(), Request{
		OperationID: "listReviewChecklists", Method: http.MethodPost, Path: "/open-apis/review-rules/review-checklists",
	})
	if !errors.Is(err, contracts.ErrContractMismatch) {
		t.Fatalf("err=%v，期望 ErrContractMismatch", err)
	}
}

// TestClientValidatesFinalJSONBody 验证最终 body 漂移时不会被旁路 ContractInput 掩盖。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestClientValidatesFinalJSONBody(t *testing.T) {
	client := NewClient(config.Profile{BaseURL: "https://open.qfei.cn"}, staticTokenProvider{}, nil)
	_, err := client.Do(context.Background(), Request{
		OperationID:   "createReviewChecklist",
		Method:        http.MethodPost,
		Path:          "/open-apis/review-rules/review-checklists",
		ContractInput: map[string]any{"name": "清单", "reviewRuleIds": []string{"rule-1"}},
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          []byte(`{"name":"清单"}`),
	})
	if !errors.Is(err, contracts.ErrContractMismatch) {
		t.Fatalf("err=%v，期望 ErrContractMismatch", err)
	}
}
