package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OAuthClientRegistrationRequest 保存开放平台动态注册 public client 所需的非敏感参数。
type OAuthClientRegistrationRequest struct {
	Endpoint    string
	ClientName  string
	RedirectURI string
	Scope       string
}

type oauthClientRegistrationPayload struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope,omitempty"`
}

type oauthClientRegistrationResponse struct {
	ClientID string `json:"client_id"`
}

// RegisterOAuthClient 通过开放平台匿名接口动态注册 OAuth public client，并仅返回后续授权所需的 client_id。
// 入参：ctx 控制请求；client 为可注入 HTTP 客户端；input 提供注册端点、客户端名称、回调地址和 scope。
// 返回值：string 为服务端生成的 client_id；error 为配置、网络、HTTP 或响应协议错误。
func RegisterOAuthClient(ctx context.Context, client *http.Client, input OAuthClientRegistrationRequest) (string, error) {
	if err := validateOAuthEndpoint("client_registration_endpoint", input.Endpoint); err != nil {
		return "", err
	}
	if strings.TrimSpace(input.ClientName) == "" || strings.TrimSpace(input.RedirectURI) == "" {
		return "", fmt.Errorf("注册 OAuth client 需要 client_name 和 redirect_uri")
	}
	if err := validateOAuthEndpoint("redirect_uri", input.RedirectURI); err != nil {
		return "", err
	}
	payload := oauthClientRegistrationPayload{
		ClientName:              input.ClientName,
		RedirectURIs:            []string{input.RedirectURI},
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
		Scope:                   strings.TrimSpace(input.Scope),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("编码 OAuth client 注册请求: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, input.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("创建 OAuth client 注册请求: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("注册 OAuth client: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("注册 OAuth client 失败: http=%d", response.StatusCode)
	}
	var result oauthClientRegistrationResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return "", fmt.Errorf("解析 OAuth client 注册响应: %w", err)
	}
	if strings.TrimSpace(result.ClientID) == "" {
		return "", fmt.Errorf("OAuth client 注册响应缺少 client_id")
	}
	return strings.TrimSpace(result.ClientID), nil
}
