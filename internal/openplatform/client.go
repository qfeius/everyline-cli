package openplatform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/contracts"
)

const maxResponseBytes = 32 << 20

// Request 描述 HTTP Adapter 内部的一个远端操作。
type Request struct {
	OperationID   string
	Method        string
	Path          string
	ContractInput any
	Query         url.Values
	Header        http.Header
	Body          []byte
	Timeout       time.Duration
	SuccessCode   int
}

// Response 是已验证成功码后的业务响应，不向 CLI handler 暴露远端 envelope。
type Response struct {
	Data      json.RawMessage
	RequestID string
}

// TokenProvider 是 OpenPlatformClient 获取 Bearer token 的依赖边界。
type TokenProvider interface {
	Token(context.Context, config.Profile) (auth.Token, error)
}

// Client 隐藏基础地址、鉴权、错误信封、超时和安全重试。
type Client struct {
	profile    config.Profile
	tokens     TokenProvider
	httpClient *http.Client
	sleep      func(context.Context, time.Duration) error
}

// NewClient 创建生产 HTTP Adapter。
// 入参：profile config.Profile 为环境配置；tokens TokenProvider 为鉴权来源；httpClient *http.Client 为可注入传输层。
// 返回值：*Client，可执行受控的 /open-apis/ 请求。
func NewClient(profile config.Profile, tokens TokenProvider, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{profile: profile, tokens: tokens, httpClient: httpClient, sleep: sleepContext}
}

// Do 执行远端请求，先校验路由与 Schema，再验证 HTTP 状态和接口专属业务成功码。
// 入参：ctx context.Context 控制取消；operation Request 描述方法、路径、逻辑契约输入、body 和成功码。
// 返回值：Response 为成功业务数据；error 为鉴权、网络或 API 错误。
func (client *Client) Do(ctx context.Context, operation Request) (Response, error) {
	if operation.SuccessCode == 0 {
		operation.SuccessCode = 200
	}
	if operation.Timeout <= 0 {
		operation.Timeout = 30 * time.Second
	}
	if !strings.HasPrefix(operation.Path, "/open-apis/") || strings.Contains(operation.Path, "://") {
		return Response{}, fmt.Errorf("拒绝非 /open-apis/ 相对路径: %s", operation.Path)
	}
	contractInput, err := requestContractInput(operation)
	if err != nil {
		return Response{}, err
	}
	if err := contracts.ValidateRequest(operation.OperationID, operation.Method, operation.Path, contractInput); err != nil {
		return Response{}, err
	}
	// token 获取、所有请求尝试与退避共享一个截止时间，--timeout 表示整次操作预算。
	operationContext, cancelOperation := context.WithTimeout(ctx, operation.Timeout)
	defer cancelOperation()
	token, err := client.tokens.Token(operationContext, client.profile)
	if err != nil {
		return Response{}, err
	}

	maxAttempts := 1
	if operation.Method == http.MethodGet {
		maxAttempts = 3
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		response, retry, err := client.doOnce(operationContext, operation, token.AccessToken)
		if err == nil || !retry || attempt == maxAttempts {
			return response, err
		}
		delay := retryDelay(err, attempt)
		if err := client.sleep(operationContext, delay); err != nil {
			return Response{}, err
		}
	}
	return Response{}, fmt.Errorf("请求未执行")
}

// requestContractInput 对 JSON 操作解析最终 HTTP body，其他操作使用显式的 query/path/multipart 逻辑输入。
// 入参：operation Request 为待发送请求。
// 返回值：any 为 Schema 校验输入；error 在 JSON body 非法或包含多值时包装 ErrContractMismatch。
func requestContractInput(operation Request) (any, error) {
	contentType := strings.ToLower(operation.Header.Get("Content-Type"))
	if !strings.HasPrefix(contentType, "application/json") {
		return operation.ContractInput, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(operation.Body))
	decoder.UseNumber()
	var input any
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("%w: %s JSON body 无效: %v", contracts.ErrContractMismatch, operation.OperationID, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%w: %s JSON body 只能包含一个值", contracts.ErrContractMismatch, operation.OperationID)
	}
	return input, nil
}

// doOnce 执行单次请求并判断是否允许 GET 安全重试。
// 入参：ctx context.Context 控制取消；operation Request 为远端操作；accessToken string 为 Bearer token。
// 返回值：Response 为业务数据；bool 表示错误是否可重试；error 为本次失败原因。
func (client *Client) doOnce(ctx context.Context, operation Request, accessToken string) (Response, bool, error) {
	requestURL := strings.TrimRight(client.profile.BaseURL, "/") + operation.Path
	if len(operation.Query) > 0 {
		requestURL += "?" + operation.Query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, operation.Method, requestURL, bytes.NewReader(operation.Body))
	if err != nil {
		return Response{}, false, fmt.Errorf("创建请求 %s: %w", operation.OperationID, err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("User-Agent", "everyline-cli")
	for name, values := range operation.Header {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}

	httpResponse, err := client.httpClient.Do(request)
	if err != nil {
		var networkError net.Error
		return Response{}, errors.As(err, &networkError), fmt.Errorf("请求 %s: %w", operation.OperationID, err)
	}
	defer httpResponse.Body.Close()
	content, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxResponseBytes))
	if err != nil {
		return Response{}, false, fmt.Errorf("读取 %s 响应: %w", operation.OperationID, err)
	}
	requestID := firstNonEmpty(httpResponse.Header.Get("X-Request-Id"), httpResponse.Header.Get("X-Tt-Logid"), httpResponse.Header.Get("Trace-Id"))
	var envelope struct {
		Code json.RawMessage `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil {
		// 网关可能在限流或故障时返回空 body/HTML；重试决策必须独立于业务 envelope 是否可解析。
		retryable := httpResponse.StatusCode == http.StatusTooManyRequests || httpResponse.StatusCode >= 500
		apiError := &APIError{
			HTTPStatus: httpResponse.StatusCode,
			Code:       "INVALID_RESPONSE",
			Message:    "远端响应不是合法 JSON",
			RequestID:  requestID,
			RetryAfter: parseRetryAfter(httpResponse.Header.Get("Retry-After")),
			Retryable:  retryable,
		}
		return Response{}, apiError.Retryable, apiError
	}
	code := rawCode(envelope.Code)
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 || code != strconv.Itoa(operation.SuccessCode) {
		apiError := &APIError{
			HTTPStatus: httpResponse.StatusCode,
			Code:       code,
			Message:    envelope.Msg,
			RequestID:  requestID,
			RetryAfter: parseRetryAfter(httpResponse.Header.Get("Retry-After")),
			Retryable:  httpResponse.StatusCode == http.StatusTooManyRequests || httpResponse.StatusCode >= 500,
		}
		if apiError.Message == "" {
			apiError.Message = http.StatusText(httpResponse.StatusCode)
		}
		return Response{}, apiError.Retryable, apiError
	}
	if len(envelope.Data) == 0 {
		envelope.Data = json.RawMessage("null")
	}
	return Response{Data: envelope.Data, RequestID: requestID}, false, nil
}

// rawCode 将数字或字符串业务码统一成字符串，避免错误模型丢失平台原值。
// 入参：value json.RawMessage 为 envelope.code。
// 返回值：string，去除 JSON 引号后的业务码。
func rawCode(value json.RawMessage) string {
	if len(value) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	return strings.TrimSpace(string(value))
}

// parseRetryAfter 解析 Retry-After 的秒数或标准 HTTP-date 形式。
// 入参：value string 为响应头值。
// 返回值：time.Duration，无法解析时为 0。
func parseRetryAfter(value string) time.Duration {
	normalized := strings.TrimSpace(value)
	seconds, err := strconv.Atoi(normalized)
	if err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	retryAt, err := http.ParseTime(normalized)
	if err != nil {
		return 0
	}
	delay := time.Until(retryAt)
	if delay <= 0 {
		return 0
	}
	return delay
}

// retryDelay 计算带轻微抖动的指数退避，并优先尊重 Retry-After。
// 入参：err error 为上次错误；attempt int 为从 1 开始的尝试序号。
// 返回值：time.Duration，下一次重试前的等待时间。
func retryDelay(err error, attempt int) time.Duration {
	var apiError *APIError
	if errors.As(err, &apiError) && apiError.RetryAfter > 0 {
		return apiError.RetryAfter
	}
	base := time.Duration(1<<(attempt-1)) * 200 * time.Millisecond
	return base + time.Duration(rand.Intn(100))*time.Millisecond
}

// sleepContext 等待指定时间，并响应上层取消。
// 入参：ctx context.Context 控制取消；delay time.Duration 为等待时长。
// 返回值：error，取消时返回 ctx.Err()，正常等待为 nil。
func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// firstNonEmpty 返回第一个非空字符串，用于兼容不同网关的 request ID 头。
// 入参：values ...string 为候选值。
// 返回值：string，第一个非空值；全部为空时返回空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
