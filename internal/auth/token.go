package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/contracts"
)

var (
	ErrCredentialsMissing = errors.New("缺少应用密钥或有效 token")
	ErrAuthentication     = errors.New("鉴权失败")
)

const OperationTenantAccessTokenInternal = "tenantAccessTokenInternal"

// Token 是缓存中的访问凭证及其绝对过期时间。
type Token struct {
	AccessToken string    `json:"access_token" yaml:"-"`
	ExpiresAt   time.Time `json:"expires_at" yaml:"expires_at"`
}

// ValidAt 判断 token 在预留刷新窗口后是否仍有效。
// 入参：now time.Time 为当前时间；refreshBefore time.Duration 为提前刷新窗口。
// 返回值：bool，有非空 token 且未进入刷新窗口时为 true。
func (token Token) ValidAt(now time.Time, refreshBefore time.Duration) bool {
	return token.AccessToken != "" && now.Add(refreshBefore).Before(token.ExpiresAt)
}

// TokenStore 定义访问 token 缓存的最小读写能力。
type TokenStore interface {
	Load(string) (Token, error)
	Save(string, Token) error
	Delete(string) error
}

// Provider 统一处理环境变量覆盖、本地缓存和远端刷新。
type Provider struct {
	store      TokenStore
	httpClient *http.Client
	now        func() time.Time
}

// NewProvider 创建 token 提供器。
// 入参：store TokenStore 为安全缓存；httpClient *http.Client 为 token 专用客户端；now func() time.Time 为时钟。
// 返回值：*Provider，可用于登录、读取和刷新 token。
func NewProvider(store TokenStore, httpClient *http.Client, now func() time.Time) *Provider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	return &Provider{store: store, httpClient: httpClient, now: now}
}

// Token 优先读取显式环境变量，其次读取有效缓存，最后用环境变量中的 app secret 刷新。
// 入参：ctx context.Context 控制请求取消；profile config.Profile 指定凭证和 token 地址。
// 返回值：Token 为可用凭证；error 在缺少凭证或远端失败时非 nil。
func (provider *Provider) Token(ctx context.Context, profile config.Profile) (Token, error) {
	if accessToken := strings.TrimSpace(os.Getenv("EVERYLINE_ACCESS_TOKEN")); accessToken != "" {
		return Token{AccessToken: accessToken, ExpiresAt: provider.now().Add(24 * time.Hour)}, nil
	}
	if cached, err := provider.store.Load(profile.Name); err == nil && cached.ValidAt(provider.now(), time.Minute) {
		return cached, nil
	}
	secret := SecretFromEnvironment(profile.Name)
	if secret == "" {
		return Token{}, ErrCredentialsMissing
	}
	return provider.Login(ctx, profile, secret)
}

// Login 使用 appId/appSecret 获取 tenant token，并仅缓存 token 而不保存 app secret。
// 入参：ctx context.Context 控制请求取消；profile config.Profile 提供 token URL 和 app ID；appSecret string 为仅驻留内存的密钥。
// 返回值：Token 为新凭证；error 在协议或网络失败时非 nil。
func (provider *Provider) Login(ctx context.Context, profile config.Profile, appSecret string) (Token, error) {
	if strings.TrimSpace(appSecret) == "" {
		return Token{}, ErrCredentialsMissing
	}
	contractInput := map[string]string{"appId": profile.AppID, "appSecret": appSecret}
	if err := contracts.ValidateRequest(OperationTenantAccessTokenInternal, http.MethodPost, "profile.token_url", contractInput); err != nil {
		return Token{}, err
	}
	payload, err := json.Marshal(contractInput)
	if err != nil {
		return Token{}, fmt.Errorf("编码 token 请求: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, profile.TokenURL, bytes.NewReader(payload))
	if err != nil {
		return Token{}, fmt.Errorf("创建 token 请求: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := provider.httpClient.Do(request)
	if err != nil {
		return Token{}, fmt.Errorf("%w: 请求 token: %w", ErrAuthentication, err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return Token{}, fmt.Errorf("%w: 读取 token 响应: %v", ErrAuthentication, err)
	}
	var envelope struct {
		Code              int    `json:"code"`
		Message           string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		TenantTokenCompat string `json:"tenantAccessToken"`
		Expire            int64  `json:"expire"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil {
		return Token{}, fmt.Errorf("%w: 解析 token 响应: %v", ErrAuthentication, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Code != 0 {
		return Token{}, fmt.Errorf("%w: http=%d code=%d msg=%s", ErrAuthentication, response.StatusCode, envelope.Code, envelope.Message)
	}
	accessToken := envelope.TenantAccessToken
	if accessToken == "" {
		accessToken = envelope.TenantTokenCompat
	}
	if accessToken == "" || envelope.Expire <= 0 {
		return Token{}, fmt.Errorf("%w: token 响应缺少 tenant_access_token 或 expire", ErrAuthentication)
	}
	token := Token{AccessToken: accessToken, ExpiresAt: provider.now().Add(time.Duration(envelope.Expire) * time.Second)}
	if err := provider.store.Save(profile.Name, token); err != nil {
		return Token{}, err
	}
	return token, nil
}

// SecretFromEnvironment 按 Profile 专用变量、通用变量的顺序读取 app secret。
// 入参：profileName string 为 Profile 名称。
// 返回值：string，仅驻留进程内存的 app secret；未配置时为空。
func SecretFromEnvironment(profileName string) string {
	key := profileSecretEnvironmentKey(profileName)
	if key != "" {
		if secret := strings.TrimSpace(os.Getenv(key)); secret != "" {
			return secret
		}
	}
	return strings.TrimSpace(os.Getenv("EVERYLINE_APP_SECRET"))
}

// profileSecretEnvironmentKey 将 Profile 名编码为无碰撞的专用密钥环境变量名。
// 入参：profileName string 为配置中受限为字母、数字、点、下划线和连字符的名称。
// 返回值：string，为 EVERYLINE_APP_SECRET_ 前缀加可逆后缀；空名称返回空字符串。
func profileSecretEnvironmentKey(profileName string) string {
	if profileName == "" {
		return ""
	}
	var suffix strings.Builder
	for _, character := range []byte(profileName) {
		if character >= 'a' && character <= 'z' {
			suffix.WriteByte(character - ('a' - 'A'))
			continue
		}
		if character >= '0' && character <= '9' {
			suffix.WriteByte(character)
			continue
		}
		// 原始大写字母和所有符号都转为固定十六进制，既保留常见小写名称的可读性，也保持大小写可逆。
		_, _ = fmt.Fprintf(&suffix, "_%02X", character)
	}
	return "EVERYLINE_APP_SECRET_" + suffix.String()
}
