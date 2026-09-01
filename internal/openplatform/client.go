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

const invalidUserSessionCode = "110004"

// Request 描述 HTTP Adapter 内部的一个远端操作。
type Request struct {
	OperationID string
	Method      string
	Path        string
	// UserPath 为 user 身份的专用路由；为空时复用 Path，便于逐步迁移已有接口。
	UserPath      string
	Identity      config.IdentityKind
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
	identity   config.IdentityKind
}

// NewClient 创建生产 HTTP Adapter。
// 入参：profile config.Profile 为环境配置；tokens TokenProvider 为鉴权来源；httpClient *http.Client 为可注入传输层。
// 返回值：*Client，可执行受控的 /open-apis/ 请求。
func NewClient(profile config.Profile, tokens TokenProvider, httpClient *http.Client) *Client {
	return NewClientForIdentity(profile, tokens, httpClient, config.IdentityApp)
}

// NewClientForIdentity 创建绑定业务身份的 HTTP Adapter。
// 入参：profile config.Profile 为环境配置；tokens TokenProvider 为鉴权来源；httpClient *http.Client 为传输层；identity config.IdentityKind 为请求身份。
// 返回值：*Client，可执行受控的 /open-apis/ 请求。
func NewClientForIdentity(profile config.Profile, tokens TokenProvider, httpClient *http.Client, identity config.IdentityKind) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	if identity == "" {
		identity = config.IdentityApp
	}
	return &Client{profile: profile, tokens: tokens, httpClient: httpClient, sleep: sleepContext, identity: identity}
}

// Do 执行远端请求，先校验路由与 Schema，再验证 HTTP 状态和接口专属业务成功码。
// 入参：ctx context.Context 控制取消；operation Request 描述方法、路径、逻辑契约输入、body 和成功码。
// 返回值：Response 为成功业务数据；error 为鉴权、网络或 API 错误。
func (client *Client) Do(ctx context.Context, operation Request) (Response, error) {
	identity := operation.Identity
	if identity == "" {
		identity = client.identity
	}
	if identity == "" {
		identity = config.IdentityApp
	}
	if operation.SuccessCode == 0 {
		operation.SuccessCode = 200
	}
	if operation.Timeout <= 0 {
		operation.Timeout = 30 * time.Second
	}
	if !strings.HasPrefix(operation.Path, "/open-apis/") || strings.Contains(operation.Path, "://") {
		return Response{}, fmt.Errorf("拒绝非 /open-apis/ 相对路径: %s", operation.Path)
	}
	contractPath := operation.Path
	contractInput, err := requestContractInput(operation)
	if err != nil {
		return Response{}, err
	}
	if err := contracts.ValidateRequest(operation.OperationID, operation.Method, contractPath, contractInput); err != nil {
		return Response{}, err
	}
	operation.Path = routePath(operation, identity)
	operation.Identity = identity
	if !strings.HasPrefix(operation.Path, "/open-apis/") || strings.Contains(operation.Path, "://") {
		return Response{}, fmt.Errorf("拒绝非 /open-apis/ 相对路径: %s", operation.Path)
	}
	// token 获取、所有请求尝试与退避共享一个截止时间，--timeout 表示整次操作预算。
	operationContext, cancelOperation := context.WithTimeout(ctx, operation.Timeout)
	defer cancelOperation()
	token, err := client.tokenForIdentity(operationContext, identity)
	if err != nil {
		return Response{}, err
	}

	maxAttempts := 1
	if operation.Method == http.MethodGet {
		maxAttempts = 3
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		response, retry, err := client.doOnce(operationContext, operation, token.AccessToken)
		if sessionError := client.invalidateExpiredUserSession(identity, err); sessionError != nil {
			return Response{}, sessionError
		}
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

// invalidateExpiredUserSession 在服务端返回 110004 时删除当前 Profile 的 user token，并生成明确的重新授权错误。
// 入参：identity config.IdentityKind 为本次请求身份；requestErr error 为远端请求结果。
// 返回值：error，非 user 会话失效时为 nil；命中时包含重新授权提示及原始 API 定位信息。
func (client *Client) invalidateExpiredUserSession(identity config.IdentityKind, requestErr error) error {
	if identity != config.IdentityUser || requestErr == nil {
		return nil
	}
	var apiError *APIError
	if !errors.As(requestErr, &apiError) || apiError.Code != invalidUserSessionCode {
		return nil
	}
	invalidator, ok := client.tokens.(interface {
		InvalidateForIdentity(string, config.IdentityKind) error
	})
	if !ok {
		return fmt.Errorf("%w；清理本地 token 缓存失败: token provider 不支持身份失效；%v", auth.ErrUserSessionExpired, apiError)
	}
	if err := invalidator.InvalidateForIdentity(client.profile.Name, identity); err != nil {
		return fmt.Errorf("%w；清理本地 token 缓存失败: %v；%v", auth.ErrUserSessionExpired, err, apiError)
	}
	return fmt.Errorf("%w；%v", auth.ErrUserSessionExpired, apiError)
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
	requestURL := strings.TrimRight(client.profile.BaseURLFor(operation.Identity), "/") + operation.Path
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

// tokenForIdentity 兼容旧 TokenProvider，同时优先调用支持 user/app 分流的新接口。
// 入参：ctx context.Context 控制 token 请求；identity config.IdentityKind 为业务身份。
// 返回值：auth.Token 为 Bearer 凭证；error 为鉴权失败。
func (client *Client) tokenForIdentity(ctx context.Context, identity config.IdentityKind) (auth.Token, error) {
	if provider, ok := client.tokens.(interface {
		TokenForIdentity(context.Context, config.Profile, config.IdentityKind) (auth.Token, error)
	}); ok {
		return provider.TokenForIdentity(ctx, client.profile, identity)
	}
	if identity == config.IdentityUser {
		// 旧版 TokenProvider 只有 app 接口，user 请求不能静默降级为 tenant token。
		return auth.Token{}, auth.ErrUserAuthentication
	}
	return client.tokens.Token(ctx, client.profile)
}

// routePath 根据身份选择 Service 提供的 user 路由；当前未声明专用路由时复用已验证的 app 路径。
// 入参：operation Request 为请求定义；identity config.IdentityKind 为业务身份。
// 返回值：string，为最终发送的相对路径。
func routePath(operation Request, identity config.IdentityKind) string {
	if identity == config.IdentityUser && strings.TrimSpace(operation.UserPath) != "" {
		return operation.UserPath
	}
	return operation.Path
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
