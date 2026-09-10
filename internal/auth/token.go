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
	ErrCredentialsMissing          = errors.New("缺少应用密钥或有效 token")
	ErrAuthentication              = errors.New("鉴权失败")
	ErrUserAuthentication          = errors.New("用户 OAuth 认证未完成")
	ErrUserSessionExpired          = errors.New("登录已失效")
	ErrUserTokenRefreshUnavailable = errors.New("user token 不支持刷新")
	ErrUserTokenChanged            = errors.New("user token 已被并发更新")
)

const OperationTenantAccessTokenInternal = "tenantAccessTokenInternal"

const userTokenRefreshWindow = 5 * time.Minute

// Token 是缓存中的访问凭证及其可选生命周期信息。
type Token struct {
	AccessToken   string    `json:"access_token" yaml:"-"`
	TokenType     string    `json:"token_type,omitempty" yaml:"-"`
	RefreshToken  string    `json:"refresh_token,omitempty" yaml:"-"`
	OAuthClientID string    `json:"oauth_client_id,omitempty" yaml:"-"`
	Scope         string    `json:"scope,omitempty" yaml:"-"`
	IssuedAt      time.Time `json:"issued_at,omitempty" yaml:"-"`
	ExpiresAt     time.Time `json:"expires_at" yaml:"expires_at"`
}

// ValidAt 判断 token 在预留刷新窗口后是否仍有效。
// 入参：now time.Time 为当前时间；refreshBefore time.Duration 为提前刷新窗口。
// 返回值：bool，有非空 token 且未进入刷新窗口时为 true。
func (token Token) ValidAt(now time.Time, refreshBefore time.Duration) bool {
	if token.AccessToken == "" {
		return false
	}
	// 缺少服务端生命周期信息的 token 仍可使用；未知不等于已过期。
	if token.ExpiresAt.IsZero() {
		return true
	}
	return now.Add(refreshBefore).Before(token.ExpiresAt)
}

// ExpiredAt 判断 token 是否已被服务端返回的过期时间明确判定为过期。
// 入参：now time.Time 为当前时间。
// 返回值：bool，只有已知 expires_at 且当前时间不早于它时才为 true。
func (token Token) ExpiredAt(now time.Time) bool {
	return token.AccessToken != "" && !token.ExpiresAt.IsZero() && !now.Before(token.ExpiresAt)
}

// TokenStore 定义访问 token 缓存的最小读写能力。
type TokenStore interface {
	Load(string) (Token, error)
	Save(string, Token) error
	Delete(string) error
}

// Provider 统一处理环境变量覆盖、本地缓存和服务端 token 获取。
type Provider struct {
	store       TokenStore
	secretStore AppSecretStore
	httpClient  *http.Client
	now         func() time.Time
	deviceStore DeviceCredentialStore
}

// NewProvider 创建 token 提供器。
// 入参：store TokenStore 为安全缓存；httpClient *http.Client 为 token 专用客户端；now func() time.Time 为时钟；secretStores 为可选 app secret 仓库。
// 返回值：*Provider，可用于登录和读取 token。
func NewProvider(store TokenStore, httpClient *http.Client, now func() time.Time, secretStores ...AppSecretStore) *Provider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	var secretStore AppSecretStore
	if len(secretStores) > 0 {
		secretStore = secretStores[0]
	}
	return &Provider{store: store, secretStore: secretStore, httpClient: httpClient, now: now}
}

// WithDeviceCredentials 为 Provider 接入豆包或 WorkBuddy 的安全 Device 凭证存储。
// 入参：store DeviceCredentialStore 为按会话隔离的安全存储。
// 返回值：*Provider 为同一实例，便于链式装配。
func (provider *Provider) WithDeviceCredentials(store DeviceCredentialStore) *Provider {
	provider.deviceStore = store
	return provider
}

// userAuthenticationError 根据凭证运行时返回准确的首次 user 授权命令。
// 入参：profileName string 为当前 Profile 名称。
// 返回值：error，保留 ErrUserAuthentication 分类，并分别指向 Device Grant 或本机 OAuth。
func (provider *Provider) userAuthenticationError(profileName string) error {
	if provider.deviceStore != nil {
		return fmt.Errorf("%w；请执行 auth init --profile %s --as user --output json", ErrUserAuthentication, profileName)
	}
	return fmt.Errorf("%w；请执行 auth login --profile %s --as user", ErrUserAuthentication, profileName)
}

// UserSessionExpiredError 根据凭证运行时返回准确的 user 重新授权命令，供业务 Client 保留同一身份恢复流程。
// 入参：profileName string 为当前 Profile 名称。
// 返回值：error，保留 ErrUserSessionExpired 分类，并分别指向 Device Grant 或本机 OAuth。
func (provider *Provider) UserSessionExpiredError(profileName string) error {
	if provider.deviceStore != nil {
		return fmt.Errorf("%w；请重新执行 auth init --restart --profile %s --as user --output json", ErrUserSessionExpired, profileName)
	}
	return fmt.Errorf("%w；请重新执行 auth login --profile %s --as user", ErrUserSessionExpired, profileName)
}

// Token 优先读取显式环境变量，其次读取有效缓存，最后用环境变量中的 app secret 重新获取 app token。
// 入参：ctx context.Context 控制请求取消；profile config.Profile 指定凭证和 token 地址。
// 返回值：Token 为可用凭证；error 在缺少凭证或远端失败时非 nil。
func (provider *Provider) Token(ctx context.Context, profile config.Profile) (Token, error) {
	return provider.TokenForIdentity(ctx, profile, config.IdentityApp)
}

/*
TokenForIdentity 按 user/app 身份读取凭证；user 凭证进入五分钟刷新窗口后，刷新失败时提供原运行时的手动授权入口。
入参：ctx context.Context 控制远端请求；profile config.Profile 指定环境；identity config.IdentityKind 为业务身份。
返回值：Token 为可用访问凭证；error 为凭证缺失、过期或刷新失败，并保留原始错误。
*/
func (provider *Provider) TokenForIdentity(ctx context.Context, profile config.Profile, identity config.IdentityKind) (Token, error) {
	parsedIdentity, err := config.ParseIdentityKind(string(identity))
	if err != nil {
		return Token{}, err
	}
	identity = parsedIdentity
	if identity == config.IdentityUser {
		if provider.deviceStore != nil {
			credential, loadErr := provider.deviceStore.Load(profile.Name)
			if loadErr == nil && credential.Token != nil && credential.Token.AccessToken != "" {
				if credential.Token.ValidAt(provider.now(), userTokenRefreshWindow) {
					return *credential.Token, nil
				}
				refreshed, refreshErr := provider.refreshDeviceUserToken(ctx, profile, credential.Token.AccessToken)
				if refreshErr == nil {
					return refreshed, nil
				}
				// 与合同 CLI 一致：进入刷新窗口后不再回退旧凭证，保留错误并提示手动授权。
				if !errors.Is(refreshErr, ErrUserSessionExpired) {
					return Token{}, fmt.Errorf("%w；刷新失败: %w", provider.UserSessionExpiredError(profile.Name), refreshErr)
				}
				return Token{}, refreshErr
			}
			if loadErr == nil && credential.Pending != nil {
				return Token{}, fmt.Errorf("%w；Device 授权尚未完成，请执行 auth complete --profile %s --as user", ErrUserAuthentication, profile.Name)
			}
			if loadErr != nil && !errors.Is(loadErr, ErrDeviceCredentialNotFound) {
				return Token{}, loadErr
			}
			// 豆包本地电脑也可能存在 Codex 的 OAuth 缓存，Device 会话缺少凭证时仍需独立授权。
			return Token{}, provider.userAuthenticationError(profile.Name)
		}
		if identityStore, ok := provider.store.(interface {
			LoadForIdentity(string, config.IdentityKind) (Token, error)
		}); ok {
			if cached, loadErr := identityStore.LoadForIdentity(profile.Name, identity); loadErr == nil {
				if cached.ValidAt(provider.now(), userTokenRefreshWindow) {
					return cached, nil
				}
				if cached.RefreshToken != "" {
					refreshed, refreshErr := provider.refreshFileUserToken(ctx, profile, cached.AccessToken)
					if refreshErr == nil {
						return refreshed, nil
					}
					// 浏览器凭证采用同一刷新窗口，刷新失败时保留原因并提供授权入口。
					if !errors.Is(refreshErr, ErrUserSessionExpired) {
						return Token{}, fmt.Errorf("%w；刷新失败: %w", provider.UserSessionExpiredError(profile.Name), refreshErr)
					}
					return Token{}, refreshErr
				}
				if cached.AccessToken != "" {
					return Token{}, provider.UserSessionExpiredError(profile.Name)
				}
			}
		}
		return Token{}, provider.userAuthenticationError(profile.Name)
	}

	if accessToken := strings.TrimSpace(os.Getenv("EVERYLINE_ACCESS_TOKEN")); accessToken != "" {
		return Token{AccessToken: accessToken, TokenType: "Bearer"}, nil
	}
	if cached, err := provider.store.Load(profile.Name); err == nil && cached.ValidAt(provider.now(), time.Minute) {
		return cached, nil
	}
	secret := SecretFromEnvironment(profile.Name)
	if secret == "" && provider.secretStore != nil {
		storedSecret, secretErr := provider.secretStore.LoadAppSecret(profile.Name)
		if secretErr == nil {
			secret = storedSecret
		} else if !errors.Is(secretErr, ErrAppSecretNotFound) {
			return Token{}, fmt.Errorf("读取本地 app secret: %w", secretErr)
		}
	}
	if secret == "" {
		return Token{}, ErrCredentialsMissing
	}
	return provider.Login(ctx, profile, secret)
}

// RefreshForIdentity 在服务端可信地拒绝当前 user access token 后，仅在原凭证运行时内强制刷新一次。
// 入参：ctx context.Context 控制刷新；profile config.Profile 为 OAuth 配置；identity config.IdentityKind 为身份；rejectedAccessToken string 为刚被拒绝的 token。
// 返回值：Token 为刷新或并发更新后的凭证；error 为不支持、失效或刷新失败。
func (provider *Provider) RefreshForIdentity(ctx context.Context, profile config.Profile, identity config.IdentityKind, rejectedAccessToken string) (Token, error) {
	if identity != config.IdentityUser || strings.TrimSpace(rejectedAccessToken) == "" {
		return Token{}, fmt.Errorf("仅 user 身份支持 OAuth token 刷新")
	}
	if provider.deviceStore != nil {
		credential, err := provider.deviceStore.Load(profile.Name)
		if err == nil && credential.Token != nil && credential.Token.AccessToken != "" {
			if credential.Token.AccessToken != rejectedAccessToken {
				return *credential.Token, nil
			}
			return provider.refreshDeviceUserToken(ctx, profile, rejectedAccessToken)
		}
		if err != nil && !errors.Is(err, ErrDeviceCredentialNotFound) {
			return Token{}, err
		}
		// Device 会话丢失时保留原授权协议，不刷新同名 Profile 的浏览器 token。
		return Token{}, provider.userAuthenticationError(profile.Name)
	}
	return provider.refreshFileUserToken(ctx, profile, rejectedAccessToken)
}

// refreshDeviceUserToken 在安全存储的跨进程锁内刷新 Device user token。
// 入参：ctx context.Context 控制请求；profile config.Profile 为 OAuth 配置；expectedAccessToken string 为调用方看到的旧 token。
// 返回值：Token 为刷新或并发更新后的凭证；error 为凭证缺失、invalid_grant 或网络失败。
func (provider *Provider) refreshDeviceUserToken(ctx context.Context, profile config.Profile, expectedAccessToken string) (Token, error) {
	var result Token
	err := provider.deviceStore.WithRefreshLock(profile.Name, func() error {
		credential, err := provider.deviceStore.Load(profile.Name)
		if err != nil {
			return err
		}
		if credential.Token == nil || credential.Token.AccessToken == "" {
			return provider.userAuthenticationError(profile.Name)
		}
		if credential.Token.AccessToken != expectedAccessToken {
			// 另一进程已在本锁之前完成刷新；直接复用其结果，避免合法并发请求无故失败。
			result = *credential.Token
			return nil
		}
		clientID := strings.TrimSpace(credential.Token.OAuthClientID)
		if clientID == "" {
			clientID = profile.EffectiveOAuthDeviceClientID()
		}
		refreshed, err := provider.refreshOAuthUserToken(ctx, profile, *credential.Token, clientID)
		if err != nil {
			if IsDeviceGrantError(err, "invalid_grant") {
				credential.Token = nil
				if saveErr := provider.deviceStore.Save(profile.Name, credential); saveErr != nil {
					return fmt.Errorf("清理已拒绝的 Device 凭证: %w", saveErr)
				}
				return provider.UserSessionExpiredError(profile.Name)
			}
			return err
		}
		credential.Token = &refreshed
		if err := provider.deviceStore.Save(profile.Name, credential); err != nil {
			return fmt.Errorf("保存刷新的 Device 凭证: %w", err)
		}
		result = refreshed
		return nil
	})
	return result, err
}

// refreshFileUserToken 在兼容 token store 的跨进程锁内刷新 authorization-code user token。
// 入参：ctx context.Context 控制请求；profile config.Profile 为 OAuth 配置；expectedAccessToken string 为调用方看到的旧 token。
// 返回值：Token 为刷新或并发更新后的凭证；error 为 store 能力、invalid_grant 或网络失败。
func (provider *Provider) refreshFileUserToken(ctx context.Context, profile config.Profile, expectedAccessToken string) (Token, error) {
	identityStore, ok := provider.store.(interface {
		LoadForIdentity(string, config.IdentityKind) (Token, error)
		SaveForIdentity(string, config.IdentityKind, Token) error
		DeleteForIdentityIfAccessTokenMatches(string, config.IdentityKind, string) error
		WithRefreshLock(string, config.IdentityKind, func() error) error
	})
	if !ok {
		return Token{}, fmt.Errorf("token store 不支持 user token 刷新")
	}
	var result Token
	err := identityStore.WithRefreshLock(profile.Name, config.IdentityUser, func() error {
		current, err := identityStore.LoadForIdentity(profile.Name, config.IdentityUser)
		if err != nil {
			return provider.userAuthenticationError(profile.Name)
		}
		if current.AccessToken != expectedAccessToken {
			// 另一进程已更新兼容缓存；把当前 token 作为本次刷新结果交给调用层重放请求。
			result = current
			return nil
		}
		clientID := strings.TrimSpace(current.OAuthClientID)
		if clientID == "" {
			clientID = profile.OAuthClientID
		}
		refreshed, err := provider.refreshOAuthUserToken(ctx, profile, current, clientID)
		if err != nil {
			if IsDeviceGrantError(err, "invalid_grant") {
				if deleteErr := identityStore.DeleteForIdentityIfAccessTokenMatches(profile.Name, config.IdentityUser, expectedAccessToken); deleteErr != nil {
					return deleteErr
				}
				return provider.UserSessionExpiredError(profile.Name)
			}
			return err
		}
		if err := identityStore.SaveForIdentity(profile.Name, config.IdentityUser, refreshed); err != nil {
			return err
		}
		result = refreshed
		return nil
	})
	return result, err
}

// refreshOAuthUserToken 发现 token endpoint 并提交标准 refresh_token grant。
// 入参：ctx context.Context 控制请求；profile config.Profile 为 metadata 配置；current Token 为当前凭证；clientID string 为签发当前 token 的 OAuth client。
// 返回值：Token 为刷新结果；error 为配置、发现或 token endpoint 错误。
func (provider *Provider) refreshOAuthUserToken(ctx context.Context, profile config.Profile, current Token, clientID string) (Token, error) {
	if current.RefreshToken == "" {
		return Token{}, fmt.Errorf("%w：缺少 refresh_token", ErrUserTokenRefreshUnavailable)
	}
	metadata, err := DiscoverOAuthMetadata(ctx, provider.httpClient, profile.OAuthMetadataURL)
	if err != nil {
		return Token{}, err
	}
	if strings.TrimSpace(metadata.TokenEndpoint) == "" {
		return Token{}, fmt.Errorf("OAuth metadata 缺少 token_endpoint")
	}
	if len(metadata.GrantTypes) > 0 && !containsFold(metadata.GrantTypes, "refresh_token") {
		return Token{}, fmt.Errorf("%w：OAuth metadata 未声明 refresh_token grant", ErrUserTokenRefreshUnavailable)
	}
	return RefreshOAuthToken(ctx, provider.httpClient, metadata.TokenEndpoint, clientID, current.RefreshToken, provider.now)
}

// InvalidateForIdentity 清理当前凭证运行时中已失效且仍与请求一致的 token，保留其他运行时及身份的凭证。
// 入参：profileName string 为 Profile 名称；identity config.IdentityKind 为需要失效的业务身份；rejectedAccessToken string 为服务端拒绝的 access token。
// 返回值：error，身份非法、token store 不支持身份隔离或删除失败时非 nil。
func (provider *Provider) InvalidateForIdentity(profileName string, identity config.IdentityKind, rejectedAccessToken string) error {
	parsedIdentity, err := config.ParseIdentityKind(string(identity))
	if err != nil {
		return err
	}
	if strings.TrimSpace(rejectedAccessToken) == "" {
		return fmt.Errorf("被拒绝的 access token 为空")
	}
	if parsedIdentity == config.IdentityUser && provider.deviceStore != nil {
		credential, loadErr := provider.deviceStore.Load(profileName)
		if loadErr == nil && credential.Token != nil && credential.Token.AccessToken == rejectedAccessToken {
			credential.Token = nil
			return provider.deviceStore.Save(profileName, credential)
		}
		if loadErr != nil && !errors.Is(loadErr, ErrDeviceCredentialNotFound) {
			return loadErr
		}
		// 当前 Device token 已移除或替换时结束，避免清理同名 Profile 的浏览器凭证。
		return nil
	}
	if identityStore, ok := provider.store.(interface {
		DeleteForIdentityIfAccessTokenMatches(string, config.IdentityKind, string) error
	}); ok {
		return identityStore.DeleteForIdentityIfAccessTokenMatches(profileName, parsedIdentity, rejectedAccessToken)
	}
	return fmt.Errorf("token store 不支持按 access token 安全失效身份")
}

// Login 使用 appId/appSecret 获取 tenant token，并仅缓存 token 而不保存 app secret。
// 入参：ctx context.Context 控制请求取消；profile config.Profile 提供 token URL 和默认 app ID；appSecret string 为仅驻留内存的密钥。
// 返回值：Token 为新凭证；error 在协议或网络失败时非 nil。
func (provider *Provider) Login(ctx context.Context, profile config.Profile, appSecret string) (Token, error) {
	if strings.TrimSpace(appSecret) == "" {
		return Token{}, ErrCredentialsMissing
	}
	appID := strings.TrimSpace(profile.AppID)
	if environmentAppID := AppIDFromEnvironment(profile.Name); environmentAppID != "" {
		appID = environmentAppID
	}
	contractInput := map[string]string{"appId": appID, "appSecret": appSecret}
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
	issuedAt := provider.now()
	token := Token{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		IssuedAt:    issuedAt,
		ExpiresAt:   issuedAt.Add(time.Duration(envelope.Expire) * time.Second),
	}
	if err := provider.store.Save(profile.Name, token); err != nil {
		return Token{}, err
	}
	return token, nil
}

// AppIDFromEnvironment 按 Profile 专用变量、通用变量的顺序读取 app ID。
// 入参：profileName string 为 Profile 名称。
// 返回值：string，仅驻留进程内存的 app ID；未配置时为空。
func AppIDFromEnvironment(profileName string) string {
	key := profileAppIDEnvironmentKey(profileName)
	if key != "" {
		if appID := strings.TrimSpace(os.Getenv(key)); appID != "" {
			return appID
		}
	}
	return strings.TrimSpace(os.Getenv("EVERYLINE_APP_ID"))
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

// profileAppIDEnvironmentKey 将 Profile 名编码为 app ID 专用环境变量名。
// 入参：profileName string 为配置中受限为字母、数字、点、下划线和连字符的名称。
// 返回值：string，为 EVERYLINE_APP_ID_ 前缀加可逆后缀；空名称返回空字符串。
func profileAppIDEnvironmentKey(profileName string) string {
	return profileEnvironmentKey("EVERYLINE_APP_ID_", profileName)
}

// profileSecretEnvironmentKey 将 Profile 名编码为无碰撞的专用密钥环境变量名。
// 入参：profileName string 为配置中受限为字母、数字、点、下划线和连字符的名称。
// 返回值：string，为 EVERYLINE_APP_SECRET_ 前缀加可逆后缀；空名称返回空字符串。
func profileSecretEnvironmentKey(profileName string) string {
	return profileEnvironmentKey("EVERYLINE_APP_SECRET_", profileName)
}

// profileEnvironmentKey 将 Profile 名编码为指定前缀的无碰撞环境变量名。
// 入参：prefix string 为环境变量前缀；profileName string 为 Profile 名称。
// 返回值：string，为前缀加可逆后缀；空名称返回空字符串。
func profileEnvironmentKey(prefix string, profileName string) string {
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
	return prefix + suffix.String()
}
