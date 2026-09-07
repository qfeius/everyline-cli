package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DeviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// DeviceAuthorizationRequest 是 Device Authorization endpoint 的请求参数。
type DeviceAuthorizationRequest struct {
	Endpoint string
	ClientID string
	Scope    string
	Resource string
}

// DeviceAuthorizationResponse 是供用户在宿主浏览器完成授权的机器可读结果。
type DeviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval,omitempty"`
}

// DeviceGrantError 保留 OAuth Device Grant 的标准错误码和描述。
type DeviceGrantError struct {
	Code        string
	Description string
}

// Error 返回不包含 device code 或 token 的 Device Grant 错误文本。
// 入参：无，接收者包含标准错误码和描述。
// 返回值：string，为可诊断错误。
func (grantError *DeviceGrantError) Error() string {
	if grantError.Description == "" {
		return "OAuth Device Grant 失败: " + grantError.Code
	}
	return "OAuth Device Grant 失败: " + grantError.Code + ": " + grantError.Description
}

// IsDeviceGrantError 判断错误是否为指定 OAuth Device Grant 标准错误码。
// 入参：err error 为待判断错误；code string 为 authorization_pending 等错误码。
// 返回值：bool，类型和错误码均匹配时为 true。
func IsDeviceGrantError(err error, code string) bool {
	var grantError *DeviceGrantError
	return errors.As(err, &grantError) && grantError.Code == code
}

// StartDeviceAuthorization 创建一次短期 Device 授权事务。
// 入参：ctx context.Context 控制请求；client *http.Client 为传输层；input DeviceAuthorizationRequest 为端点和授权范围。
// 返回值：DeviceAuthorizationResponse 为完整授权 URL 和过期时间；error 为配置、协议或网络失败。
func StartDeviceAuthorization(ctx context.Context, client *http.Client, input DeviceAuthorizationRequest) (DeviceAuthorizationResponse, error) {
	if err := validateOAuthEndpoint("device_authorization_endpoint", input.Endpoint); err != nil {
		return DeviceAuthorizationResponse{}, err
	}
	if strings.TrimSpace(input.ClientID) == "" || strings.TrimSpace(input.Scope) == "" {
		return DeviceAuthorizationResponse{}, fmt.Errorf("Device Grant 需要 client_id 和 scope")
	}
	form := url.Values{"client_id": {input.ClientID}, "scope": {input.Scope}}
	if strings.TrimSpace(input.Resource) != "" {
		form.Set("resource", input.Resource)
	}
	var response DeviceAuthorizationResponse
	if err := postOAuthForm(ctx, client, input.Endpoint, form, &response); err != nil {
		return DeviceAuthorizationResponse{}, err
	}
	if strings.TrimSpace(response.DeviceCode) == "" || strings.TrimSpace(response.VerificationURIComplete) == "" || response.ExpiresIn <= 0 {
		return DeviceAuthorizationResponse{}, fmt.Errorf("Device Authorization 响应缺少 device_code、verification_uri_complete 或 expires_in")
	}
	return response, nil
}

// CompleteDeviceAuthorization 使用已授权的 device code 换取 user token。
// 入参：ctx context.Context 控制请求；client *http.Client 为传输层；endpoint/clientID/deviceCode string 为已保存事务参数；now func() time.Time 为时钟。
// 返回值：Token 为 user token；error 为 pending、denied、expired、协议或网络失败。
func CompleteDeviceAuthorization(ctx context.Context, client *http.Client, endpoint, clientID, deviceCode string, now func() time.Time) (Token, error) {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(deviceCode) == "" {
		return Token{}, fmt.Errorf("Device token 请求缺少 client_id 或 device_code")
	}
	form := url.Values{
		"grant_type":  {DeviceGrantType},
		"client_id":   {clientID},
		"device_code": {deviceCode},
	}
	return exchangeOAuthToken(ctx, client, endpoint, form, now)
}

// RefreshOAuthToken 使用 refresh token 换取新 access token，并保留未轮换的 refresh token。
// 入参：ctx context.Context 控制请求；client *http.Client 为传输层；endpoint/clientID/refreshToken string 为刷新参数；now func() time.Time 为时钟。
// 返回值：Token 为新凭证；error 为 invalid_grant、协议或网络失败。
func RefreshOAuthToken(ctx context.Context, client *http.Client, endpoint, clientID, refreshToken string, now func() time.Time) (Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	}
	token, err := exchangeOAuthToken(ctx, client, endpoint, form, now)
	if err != nil {
		return Token{}, err
	}
	if token.RefreshToken == "" {
		token.RefreshToken = refreshToken
	}
	return token, nil
}

// RevokeOAuthRefreshToken 在远端吊销 refresh token。
// 入参：ctx context.Context 控制请求；client *http.Client 为传输层；endpoint/clientID/refreshToken string 为撤销参数。
// 返回值：error，配置、网络或非 2xx 响应时非 nil。
func RevokeOAuthRefreshToken(ctx context.Context, client *http.Client, endpoint, clientID, refreshToken string) error {
	if err := validateOAuthEndpoint("revocation_endpoint", endpoint); err != nil {
		return err
	}
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(refreshToken) == "" {
		return fmt.Errorf("OAuth token 撤销需要 client_id 和 refresh_token")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	form := url.Values{"client_id": {clientID}, "token": {refreshToken}, "token_type_hint": {"refresh_token"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("创建 OAuth token 撤销请求: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("撤销 OAuth token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("撤销 OAuth token 失败: http=%d", response.StatusCode)
	}
	return nil
}

// exchangeOAuthToken 执行 Device complete 或 refresh 的 token endpoint 请求。
// 入参：ctx context.Context 控制请求；client *http.Client 为传输层；endpoint string 为 token endpoint；form url.Values 为表单；now func() time.Time 为时钟。
// 返回值：Token 为解析后的凭证；error 为 OAuth 标准错误、网络或响应校验失败。
func exchangeOAuthToken(ctx context.Context, client *http.Client, endpoint string, form url.Values, now func() time.Time) (Token, error) {
	if err := validateOAuthEndpoint("token_endpoint", endpoint); err != nil {
		return Token{}, err
	}
	if strings.TrimSpace(form.Get("client_id")) == "" {
		return Token{}, fmt.Errorf("OAuth token 请求缺少 client_id")
	}
	var payload oauthTokenResponse
	if err := postOAuthForm(ctx, client, endpoint, form, &payload); err != nil {
		return Token{}, err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return Token{}, fmt.Errorf("OAuth token 响应缺少 access_token")
	}
	if now == nil {
		now = time.Now
	}
	issuedAt := now()
	token := Token{
		AccessToken: payload.AccessToken, TokenType: payload.TokenType, RefreshToken: payload.RefreshToken,
		OAuthClientID: form.Get("client_id"), Scope: payload.Scope, IssuedAt: issuedAt,
	}
	if payload.ExpiresIn > 0 {
		token.ExpiresAt = issuedAt.Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	return token, nil
}

// postOAuthForm 发送表单并统一解析 OAuth 成功或标准错误响应。
// 入参：ctx context.Context 控制请求；client *http.Client 为传输层；endpoint string 为 HTTPS 地址；form url.Values 为表单；result any 为 JSON 目标。
// 返回值：error，为网络、HTTP、OAuth 或 JSON 解析失败。
func postOAuthForm(ctx context.Context, client *http.Client, endpoint string, form url.Values, result any) error {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("创建 OAuth 请求: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("执行 OAuth 请求: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var payload struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); decodeErr == nil && payload.Error != "" {
			return &DeviceGrantError{Code: payload.Error, Description: payload.ErrorDescription}
		}
		return fmt.Errorf("OAuth 请求失败: http=%d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(result); err != nil {
		return fmt.Errorf("解析 OAuth 响应: %w", err)
	}
	return nil
}
