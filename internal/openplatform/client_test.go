package openplatform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		ContractInput: map[string]any{"taskId": 42},
		Query:         url.Values{"taskId": []string{"42"}},
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

// TestClientRejectsAbsolutePath 验证业务 Client 不会把 token 发送到任意 URL。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestClientRejectsAbsolutePath(t *testing.T) {
	client := NewClient(config.Profile{BaseURL: "https://example.com"}, staticTokenProvider{}, nil)
	_, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "https://evil.example/open-apis/test"})
	if err == nil {
		t.Fatal("期望绝对 URL 被拒绝")
	}
}

// TestClientTimesOutTokenRefresh 验证获取 token 不会绕过业务操作超时。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestClientTimesOutTokenRefresh(t *testing.T) {
	client := NewClient(config.Profile{BaseURL: "https://example.com"}, blockingTokenProvider{}, nil)
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
	client := NewClient(config.Profile{BaseURL: "https://example.com"}, staticTokenProvider{}, nil)
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
	client := NewClient(config.Profile{BaseURL: "https://example.com"}, staticTokenProvider{}, nil)
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
