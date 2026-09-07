package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/config"
)

const oauthCallbackMessage = "用户授权已完成，可以返回终端。"

// OAuthCallback 接收 OAuth authorization server 回调中的授权码。
type OAuthCallback interface {
	Wait(context.Context, string) (string, error)
	Close()
}

// OAuthLoginOptions 保存 user OAuth 登录的可注入 I/O 和超时依赖。
type OAuthLoginOptions struct {
	HTTPClient    *http.Client
	Now           func() time.Time
	Output        io.Writer
	OpenBrowser   func(string) error
	NoOpenBrowser bool
	StartCallback func(string) (OAuthCallback, error)
}

type OAuthMetadata struct {
	AuthorizationEndpoint       string   `json:"authorization_endpoint"`
	TokenEndpoint               string   `json:"token_endpoint"`
	RegistrationEndpoint        string   `json:"registration_endpoint"`
	DeviceAuthorizationEndpoint string   `json:"device_authorization_endpoint"`
	RevocationEndpoint          string   `json:"revocation_endpoint"`
	CodeChallengeMethods        []string `json:"code_challenge_methods_supported"`
	GrantTypes                  []string `json:"grant_types_supported"`
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// LoginUserOAuth 执行 authorization code + PKCE user 登录并返回未输出的 token。
// 入参：ctx 控制 metadata、回调等待和 token exchange；profile 提供 OAuth 非敏感配置；options 注入 HTTP/browser/callback。
// 返回值：Token 为授权服务返回的 user token；error 为配置、回调、网络或协议错误。
func LoginUserOAuth(ctx context.Context, profile config.Profile, options OAuthLoginOptions) (Token, error) {
	if !profile.HasOAuthConfiguration() {
		return Token{}, fmt.Errorf("OAuth 配置不完整")
	}
	if err := validateOAuthEndpoint("metadata_url", profile.OAuthMetadataURL); err != nil {
		return Token{}, err
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	metadata, err := fetchOAuthMetadata(ctx, client, profile.OAuthMetadataURL)
	if err != nil {
		return Token{}, err
	}
	if err := validateOAuthEndpoint("authorization_endpoint", metadata.AuthorizationEndpoint); err != nil {
		return Token{}, err
	}
	if err := validateOAuthEndpoint("token_endpoint", metadata.TokenEndpoint); err != nil {
		return Token{}, err
	}
	if len(metadata.CodeChallengeMethods) > 0 && !containsFold(metadata.CodeChallengeMethods, "S256") {
		return Token{}, fmt.Errorf("OAuth 服务端不支持 PKCE S256")
	}
	verifier, err := newOAuthRandomString(32)
	if err != nil {
		return Token{}, fmt.Errorf("生成 OAuth code_verifier: %w", err)
	}
	state, err := newOAuthRandomString(32)
	if err != nil {
		return Token{}, fmt.Errorf("生成 OAuth state: %w", err)
	}
	callbackFactory := options.StartCallback
	if callbackFactory == nil {
		callbackFactory = StartOAuthCallbackServer
	}
	callback, err := callbackFactory(profile.OAuthRedirectURL)
	if err != nil {
		return Token{}, err
	}
	defer callback.Close()
	authorizationURL, err := buildOAuthAuthorizationURL(metadata.AuthorizationEndpoint, profile, state, s256Challenge(verifier))
	if err != nil {
		return Token{}, err
	}
	if options.Output != nil {
		_, _ = fmt.Fprintf(options.Output, "请在浏览器中完成用户授权：\n%s\n", authorizationURL)
	}
	if !options.NoOpenBrowser {
		opener := options.OpenBrowser
		if opener == nil {
			opener = OpenBrowser
		}
		if err := opener(authorizationURL); err != nil && options.Output != nil {
			_, _ = fmt.Fprintf(options.Output, "无法自动打开浏览器，请复制上面的链接完成授权：%v\n", err)
		}
	}
	code, err := callback.Wait(ctx, state)
	if err != nil {
		return Token{}, err
	}
	token, err := exchangeOAuthCode(ctx, client, metadata.TokenEndpoint, profile, code, verifier, now)
	if err != nil {
		return Token{}, err
	}
	return token, nil
}

func fetchOAuthMetadata(ctx context.Context, client *http.Client, endpoint string) (OAuthMetadata, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return OAuthMetadata{}, fmt.Errorf("创建 OAuth metadata 请求: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return OAuthMetadata{}, fmt.Errorf("获取 OAuth metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return OAuthMetadata{}, fmt.Errorf("获取 OAuth metadata 失败: http=%d", response.StatusCode)
	}
	var metadata OAuthMetadata
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&metadata); err != nil {
		return OAuthMetadata{}, fmt.Errorf("解析 OAuth metadata: %w", err)
	}
	return metadata, nil
}

// DiscoverOAuthMetadata 获取并解析 Profile 指向的 OAuth authorization server metadata。
// 入参：ctx context.Context 控制请求；client *http.Client 为可注入 HTTP 客户端；endpoint string 为 metadata URL。
// 返回值：OAuthMetadata 为端点和能力声明；error 为 URL、网络、状态码或 JSON 错误。
func DiscoverOAuthMetadata(ctx context.Context, client *http.Client, endpoint string) (OAuthMetadata, error) {
	if err := validateOAuthEndpoint("metadata_url", endpoint); err != nil {
		return OAuthMetadata{}, err
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return fetchOAuthMetadata(ctx, client, endpoint)
}

func validateOAuthEndpoint(name, rawURL string) error {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("OAuth %s 必须是完整的 http/https URL", name)
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("OAuth %s 必须使用 https；仅 localhost 或回环 IP 允许 http", name)
	}
	return nil
}

func buildOAuthAuthorizationURL(endpoint string, profile config.Profile, state, challenge string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("解析 OAuth authorization_endpoint: %w", err)
	}
	query := parsed.Query()
	query.Set("business_type", profile.OAuthBusinessType)
	query.Set("client_id", profile.OAuthClientID)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("redirect_uri", profile.OAuthRedirectURL)
	query.Set("response_type", "code")
	query.Set("scope", strings.Join(profile.OAuthScopes, " "))
	query.Set("state", state)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func exchangeOAuthCode(ctx context.Context, client *http.Client, endpoint string, profile config.Profile, code, verifier string, now func() time.Time) (Token, error) {
	form := url.Values{}
	form.Set("client_id", profile.OAuthClientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", profile.OAuthRedirectURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("创建 OAuth token 请求: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return Token{}, fmt.Errorf("换取 OAuth token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Token{}, fmt.Errorf("换取 OAuth token 失败: http=%d", response.StatusCode)
	}
	var payload oauthTokenResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return Token{}, fmt.Errorf("解析 OAuth token: %w", err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return Token{}, fmt.Errorf("OAuth token 响应缺少 access_token")
	}
	issuedAt := now()
	token := Token{
		AccessToken:   payload.AccessToken,
		TokenType:     payload.TokenType,
		RefreshToken:  payload.RefreshToken,
		OAuthClientID: profile.OAuthClientID,
		Scope:         payload.Scope,
		IssuedAt:      issuedAt,
	}
	if payload.ExpiresIn > 0 {
		token.ExpiresAt = issuedAt.Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	return token, nil
}

func newOAuthRandomString(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func s256Challenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func containsFold(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), expected) {
			return true
		}
	}
	return false
}

// OpenBrowser 使用系统默认浏览器打开授权链接。
func OpenBrowser(rawURL string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{rawURL}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", rawURL}
	default:
		command, args = "xdg-open", []string{rawURL}
	}
	return exec.Command(command, args...).Start()
}

type loopbackOAuthCallback struct {
	server   *http.Server
	listener net.Listener
	results  chan oauthCallbackResult
	once     sync.Once
}

type oauthCallbackResult struct {
	code  string
	state string
	err   error
}

// StartOAuthCallbackServer 在 loopback 地址启动一次性 OAuth callback server。
func StartOAuthCallbackServer(rawURL string) (OAuthCallback, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "http" || !isLoopbackHost(parsed.Hostname()) || parsed.Port() == "" || parsed.Path == "" {
		return nil, fmt.Errorf("OAuth redirect URL 必须是带端口和路径的 loopback http URL")
	}
	listener, err := net.Listen("tcp", parsed.Host)
	if err != nil {
		return nil, fmt.Errorf("启动 OAuth callback server: %w", err)
	}
	callback := &loopbackOAuthCallback{
		listener: listener,
		results:  make(chan oauthCallbackResult, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(parsed.Path, callback.handle)
	callback.server = &http.Server{Handler: mux}
	go func() {
		if serveErr := callback.server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			callback.send(oauthCallbackResult{err: serveErr})
		}
	}()
	return callback, nil
}

// handle 接收 OAuth 回调，并向浏览器和等待登录的终端返回一致的授权结果。
// 入参：writer http.ResponseWriter 为浏览器响应；request *http.Request 携带授权码或错误参数。
// 返回值：无；通过 HTTP 响应展示结果，通过 results 通知登录流程。
func (callback *loopbackOAuthCallback) handle(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	// 拒绝回调可能没有 state；沿用错误直接结束登录的语义，成功授权仍由 Wait 校验 state。
	result := oauthCallbackResult{state: query.Get("state")}
	message, status := oauthCallbackMessage, http.StatusOK
	if oauthError := strings.TrimSpace(query.Get("error")); oauthError != "" {
		result.err = fmt.Errorf("OAuth 授权失败: %s", oauthError)
		if description := strings.TrimSpace(query.Get("error_description")); description != "" {
			result.err = fmt.Errorf("%w（%s）", result.err, description)
		}
		message, status = result.err.Error()+"，请返回终端重试。", http.StatusBadRequest
		if oauthError == "access_denied" {
			message, status = "用户已拒绝授权，可以关闭此页面并返回终端。", http.StatusForbidden
		}
	} else if strings.TrimSpace(query.Get("code")) == "" || strings.TrimSpace(query.Get("state")) == "" {
		result.err = fmt.Errorf("OAuth callback 缺少 code 或 state")
		message, status = result.err.Error()+"，请返回终端重试。", http.StatusBadRequest
	} else {
		result.code = query.Get("code")
	}
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(status)
	_, _ = io.WriteString(writer, message)
	callback.send(result)
}

func (callback *loopbackOAuthCallback) send(result oauthCallbackResult) {
	callback.once.Do(func() { callback.results <- result })
}

func (callback *loopbackOAuthCallback) Wait(ctx context.Context, expectedState string) (string, error) {
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("等待 OAuth callback: %w", ctx.Err())
	case result := <-callback.results:
		if result.err != nil {
			return "", result.err
		}
		if result.state != expectedState {
			return "", fmt.Errorf("OAuth callback state 不匹配")
		}
		return result.code, nil
	}
}

// Close 关闭一次性回调服务，给已接收回调的浏览器留出完成响应的时间。
// 入参：无；使用 callback 保存的 *http.Server。
// 返回值：无；优雅关闭超过一秒时强制释放连接。
func (callback *loopbackOAuthCallback) Close() {
	if callback.server != nil {
		// Wait 返回会触发登录流程关闭服务，此时 handler 的响应可能仍在 HTTP 缓冲区。
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := callback.server.Shutdown(ctx); err != nil {
			_ = callback.server.Close()
		}
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
