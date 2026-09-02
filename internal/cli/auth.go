package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/config"

	"github.com/spf13/cobra"
)

// newAuthCommand 创建 app/user 身份登录、Device 授权、状态、切换和注销命令组。
// 入参：runtime *Runtime 为凭证存储和 HTTP 依赖；root *rootOptions 为 Profile/输出 flags。
// 返回值：*cobra.Command，包含 login/init/complete/status/use/logout。
func newAuthCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "auth",
		Short: "管理 app/user 身份凭证",
		Long: `管理 app/user 身份凭证。

		身份选择优先级为：--as > Profile 默认身份 > app。
auth use 会修改 Profile 的默认身份；仅对当前命令临时指定身份请使用 --as。`,
	}
	withNotes(command,
		"app 身份使用 app secret；user 本机使用 OAuth/PKCE，远端沙箱使用 auth init/complete Device Grant。",
		"auth logout 清理当前 Profile 的 token；Device metadata 提供 revocation endpoint 时先撤销 refresh token。",
	)
	command.AddCommand(
		newAuthLoginCommand(runtime, root),
		newAuthDeviceInitCommand(runtime, root),
		newAuthDeviceCompleteCommand(runtime, root),
		newAuthStatusCommand(runtime, root),
		newAuthUseCommand(runtime, root),
		newAuthLogoutCommand(runtime, root),
	)
	return command
}

// deviceAuthOutput 是 auth init/complete 的稳定机器可读状态，不包含 device code 或 token。
type deviceAuthOutput struct {
	Status                  string `json:"status"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresAt               string `json:"expires_at,omitempty"`
}

// newAuthDeviceInitCommand 创建不依赖 loopback callback 的 OAuth Device Grant 授权事务。
// 入参：runtime *Runtime 为 HTTP、Profile 和安全凭证存储；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，可输出完整浏览器授权 URL 和过期时间。
func newAuthDeviceInitCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var restart bool
	command := &cobra.Command{
		Use:   "init",
		Short: "初始化沙箱可用的用户 Device 授权",
		Long:  "初始化 OAuth Device Grant，返回宿主浏览器可直接打开的完整授权 URL；不监听 127.0.0.1 回调。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if root.Identity != "" {
				identity, err := config.ParseIdentityKind(root.Identity)
				if err != nil {
					return err
				}
				if identity != config.IdentityUser {
					return fmt.Errorf("auth init 仅支持 --as user")
				}
			}
			profile, err := selectedProfile(runtime, root)
			if err != nil {
				return err
			}
			if !profile.HasDeviceOAuthConfiguration() {
				return fmt.Errorf("Profile %q 的 Device OAuth 配置不完整", profile.Name)
			}
			store, err := requireDeviceCredentialStore(runtime)
			if err != nil {
				return err
			}
			existing, loadErr := store.Load(profile.Name)
			if loadErr != nil && !errors.Is(loadErr, auth.ErrDeviceCredentialNotFound) {
				return loadErr
			}
			if !restart && loadErr == nil && existing.Pending != nil {
				status := string(existing.Pending.Status)
				if status == "" {
					status = string(auth.DevicePending)
				}
				if !runtimeNow(runtime).Before(existing.Pending.ExpiresAt) && existing.Pending.Status == auth.DevicePending {
					status = string(auth.DevicePendingExpired)
				}
				return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{
					Status: status, VerificationURIComplete: existing.Pending.VerificationURIComplete, ExpiresAt: formatOptionalTime(existing.Pending.ExpiresAt),
				})
			}
			if !restart && loadErr == nil && existing.Token != nil && existing.Token.AccessToken != "" {
				return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{Status: "succeeded", ExpiresAt: formatOptionalTime(existing.Token.ExpiresAt)})
			}
			authContext, cancel := context.WithTimeout(command.Context(), root.Timeout)
			defer cancel()
			metadata, err := auth.DiscoverOAuthMetadata(authContext, runtime.HTTP, profile.OAuthMetadataURL)
			if err != nil {
				return err
			}
			deviceEndpoint := strings.TrimSpace(profile.OAuthDeviceAuthorizationURL)
			if deviceEndpoint == "" {
				deviceEndpoint = strings.TrimSpace(metadata.DeviceAuthorizationEndpoint)
			}
			if deviceEndpoint == "" {
				return fmt.Errorf("OAuth metadata 尚未发布 device_authorization_endpoint；请在认证服务启用 Device Grant 并提供对应 Device client，或通过 config add --oauth-device-authorization-url/--oauth-device-client-id 写入平台确认配置")
			}
			if strings.TrimSpace(metadata.TokenEndpoint) == "" {
				return fmt.Errorf("OAuth metadata 缺少 token_endpoint")
			}
			response, err := auth.StartDeviceAuthorization(authContext, runtime.HTTP, auth.DeviceAuthorizationRequest{
				Endpoint: deviceEndpoint, ClientID: profile.EffectiveOAuthDeviceClientID(),
				Scope: strings.Join(profile.OAuthScopes, " "), Resource: profile.EffectiveOAuthResource(),
			})
			if err != nil {
				return err
			}
			expiresAt := runtimeNow(runtime).Add(time.Duration(response.ExpiresIn) * time.Second)
			revocationEndpoint := strings.TrimSpace(profile.OAuthRevocationURL)
			if revocationEndpoint == "" {
				revocationEndpoint = strings.TrimSpace(metadata.RevocationEndpoint)
			}
			profile.DefaultIdentity = config.IdentityUser
			if err := runtime.Profiles.Add(profile); err != nil {
				return err
			}
			profileSnapshot := profile
			// Device Profile 只用于 user 沙箱恢复，去除无关的 app 标识，避免跨身份带入配置。
			profileSnapshot.AppID = ""
			existing.Profile = &profileSnapshot
			existing.Token = nil
			existing.Pending = &auth.DevicePendingTransaction{
				Status: auth.DevicePending, DeviceCode: response.DeviceCode,
				VerificationURIComplete: response.VerificationURIComplete,
				TokenEndpoint:           metadata.TokenEndpoint, RevocationEndpoint: revocationEndpoint,
				ClientID: profile.EffectiveOAuthDeviceClientID(), ExpiresAt: expiresAt,
			}
			if err := store.Save(profile.Name, existing); err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{
				Status: string(auth.DevicePending), VerificationURIComplete: response.VerificationURIComplete, ExpiresAt: formatOptionalTime(expiresAt),
			})
		},
	}
	command.Flags().BoolVar(&restart, "restart", false, "明确废弃已有事务并重新开始 Device 授权")
	withNotes(command,
		"把 verification_uri_complete 作为一个完整链接原样展示给用户，不拆分、不改写 query。",
		"用户完成浏览器授权后执行 auth complete；每次 complete 只检查一次。",
	)
	return command
}

// newAuthDeviceCompleteCommand 创建一次性检查 Device 授权结果的命令。
// 入参：runtime *Runtime 为 HTTP、Profile 和安全凭证存储；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，把 pending 转为 succeeded/denied/expired/uncertain 等机器可读状态。
func newAuthDeviceCompleteCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "complete",
		Short: "完成一次用户 Device 授权检查",
		Long:  "检查一次 OAuth Device Grant 状态；成功后安全保存 access/refresh token，不输出 token 内容。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if root.Identity != "" {
				identity, err := config.ParseIdentityKind(root.Identity)
				if err != nil {
					return err
				}
				if identity != config.IdentityUser {
					return fmt.Errorf("auth complete 仅支持 --as user")
				}
			}
			profile, err := selectedProfile(runtime, root)
			if err != nil {
				return err
			}
			store, err := requireDeviceCredentialStore(runtime)
			if err != nil {
				return err
			}
			credential, err := store.Load(profile.Name)
			if err != nil {
				return fmt.Errorf("没有待完成的 Device 授权，请先执行 auth init: %w", err)
			}
			if credential.Pending == nil {
				if credential.Token != nil && credential.Token.AccessToken != "" {
					return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{Status: "succeeded", ExpiresAt: formatOptionalTime(credential.Token.ExpiresAt)})
				}
				return fmt.Errorf("没有待完成的 Device 授权，请先执行 auth init")
			}
			pending := credential.Pending
			if pending.Status == auth.DevicePendingChecking || pending.Status == auth.DevicePendingUncertain {
				return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{Status: string(auth.DevicePendingUncertain)})
			}
			if pending.Status != auth.DevicePending {
				return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{Status: string(pending.Status), ExpiresAt: formatOptionalTime(pending.ExpiresAt)})
			}
			if !runtimeNow(runtime).Before(pending.ExpiresAt) {
				pending.Status = auth.DevicePendingExpired
				if err := store.Save(profile.Name, credential); err != nil {
					return err
				}
				return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{Status: string(auth.DevicePendingExpired), ExpiresAt: formatOptionalTime(pending.ExpiresAt)})
			}
			pending.Status = auth.DevicePendingChecking
			if err := store.Save(profile.Name, credential); err != nil {
				return err
			}
			authContext, cancel := context.WithTimeout(command.Context(), root.Timeout)
			defer cancel()
			token, exchangeErr := auth.CompleteDeviceAuthorization(
				authContext, runtime.HTTP, pending.TokenEndpoint, pending.ClientID, pending.DeviceCode, runtime.Now,
			)
			if exchangeErr != nil {
				status := auth.DevicePendingUncertain
				switch {
				case auth.IsDeviceGrantError(exchangeErr, "authorization_pending"), auth.IsDeviceGrantError(exchangeErr, "slow_down"):
					status = auth.DevicePending
				case auth.IsDeviceGrantError(exchangeErr, "access_denied"):
					status = auth.DevicePendingDenied
				case auth.IsDeviceGrantError(exchangeErr, "expired_token"):
					status = auth.DevicePendingExpired
				case auth.IsDeviceGrantError(exchangeErr, "invalid_grant"):
					status = auth.DevicePendingInvalidGrant
				}
				pending.Status = status
				if err := store.Save(profile.Name, credential); err != nil {
					return err
				}
				return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{Status: string(status), ExpiresAt: formatOptionalTime(pending.ExpiresAt)})
			}
			credential.Pending = nil
			credential.Token = &token
			if err := store.Save(profile.Name, credential); err != nil {
				return fmt.Errorf("保存 Device 授权结果失败，状态可能不确定，请先修复安全存储再重新开始授权: %w", err)
			}
			return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{Status: "succeeded", ExpiresAt: formatOptionalTime(token.ExpiresAt)})
		},
	}
}

// requireDeviceCredentialStore 返回已初始化的沙箱安全凭证存储并保留初始化错误。
// 入参：runtime *Runtime 为 Device store 和初始化状态。
// 返回值：auth.DeviceCredentialStore 为可用存储；error 为运行时不支持或安全配置缺失。
func requireDeviceCredentialStore(runtime *Runtime) (auth.DeviceCredentialStore, error) {
	if runtime.DeviceCredentialError != nil {
		return nil, runtime.DeviceCredentialError
	}
	if runtime.DeviceCredentials == nil {
		return nil, fmt.Errorf("Device 授权需要豆包 SKILL_SESSION_WORKSPACE/SESSION_ID 或 WorkBuddy CODEBUDDY_SESSION_ID 运行时")
	}
	return runtime.DeviceCredentials, nil
}

// runtimeNow 使用可注入时钟，未配置时回退系统当前时间。
// 入参：runtime *Runtime 为时钟依赖。
// 返回值：time.Time，为当前时间。
func runtimeNow(runtime *Runtime) time.Time {
	if runtime.Now != nil {
		return runtime.Now()
	}
	return time.Now()
}

// formatOptionalTime 把可选时间编码为 RFC3339，零值返回空字符串以便 JSON 省略。
// 入参：value time.Time 为 token 或事务过期时间。
// 返回值：string，为 RFC3339；零值为空。
func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

// newAuthLoginCommand 创建 auth login；app 使用 app secret，user 使用 OAuth/PKCE 浏览器授权。
// 入参：runtime *Runtime 为 I/O、HTTP 和 token store；root *rootOptions 为 Profile/输出 flags。
// 返回值：*cobra.Command，可获取并缓存 app 或 user token。
func newAuthLoginCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var secretFromStdin bool
	var noOpenBrowser bool
	var saveAppSecret bool
	var appIDFlag string
	var appSecretFlag string
	command := &cobra.Command{
		Use:   "login",
		Short: "获取并安全缓存 app/user token",
		Long:  "获取并安全缓存 app 或 user 身份凭证；app 使用 app secret，user 使用 OAuth/PKCE 浏览器授权。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			profile, err := selectedProfile(runtime, root)
			if err != nil {
				return err
			}
			identity, err := selectedIdentity(profile, root.Identity)
			if err != nil {
				return err
			}
			if err := validateDeviceCredentialRuntime(runtime, identity); err != nil {
				return err
			}
			appIDFlagSet := command.Flags().Changed("app-id")
			appSecretFlagSet := command.Flags().Changed("app-secret")
			if appSecretFlagSet && secretFromStdin {
				return fmt.Errorf("--app-secret 与 --app-secret-stdin 只能选择一个")
			}
			if identity == config.IdentityUser && (appIDFlagSet || appSecretFlagSet || secretFromStdin || saveAppSecret) {
				return fmt.Errorf("user 身份不能使用 app 凭证参数，请使用 --as app")
			}
			if appIDFlagSet && strings.TrimSpace(appIDFlag) == "" {
				return fmt.Errorf("%w: --app-id 不能为空", auth.ErrCredentialsMissing)
			}
			if appSecretFlagSet && strings.TrimSpace(appSecretFlag) == "" {
				return fmt.Errorf("%w: --app-secret 不能为空", auth.ErrCredentialsMissing)
			}
			appSecret := ""
			if identity == config.IdentityUser {
				if !profile.HasOAuthConfiguration() {
					page := profile.AuthURLFor(identity)
					if page != "" {
						return fmt.Errorf("%w；OAuth 配置不完整，请先打开认证页面：%s", auth.ErrUserAuthentication, page)
					}
					return fmt.Errorf("%w；OAuth 配置不完整，请通过 config add 配置用户授权参数", auth.ErrUserAuthentication)
				}
			} else {
				appID := auth.AppIDFromEnvironment(profile.Name)
				if appIDFlagSet {
					appID = strings.TrimSpace(appIDFlag)
				}
				if appID == "" {
					appID = strings.TrimSpace(profile.AppID)
				}
				if appID == "" {
					return fmt.Errorf("%w: app-id 不能为空", auth.ErrCredentialsMissing)
				}
				profile.AppID = appID
				if appSecretFlagSet {
					appSecret = strings.TrimSpace(appSecretFlag)
				} else {
					appSecret = auth.SecretFromEnvironment(profile.Name)
				}
				if secretFromStdin {
					appSecret, err = readSecret(runtime.Input)
					if err != nil {
						return fmt.Errorf("%w: %v", auth.ErrCredentialsMissing, err)
					}
				}
				if appSecret == "" && runtime.Secrets != nil {
					storedSecret, secretErr := runtime.Secrets.LoadAppSecret(profile.Name)
					if secretErr == nil {
						appSecret = strings.TrimSpace(storedSecret)
					} else if !errors.Is(secretErr, auth.ErrAppSecretNotFound) {
						return fmt.Errorf("读取本地 app secret: %w", secretErr)
					}
				}
				if appSecret == "" {
					return auth.ErrCredentialsMissing
				}
			}
			loginContext, cancel := context.WithTimeout(command.Context(), root.Timeout)
			defer cancel()
			var token auth.Token
			if identity == config.IdentityUser {
				token, err = auth.LoginUserOAuth(loginContext, profile, auth.OAuthLoginOptions{
					HTTPClient:    runtime.HTTP,
					Now:           runtime.Now,
					Output:        runtime.Error,
					OpenBrowser:   runtime.OpenBrowser,
					NoOpenBrowser: noOpenBrowser,
				})
				if err == nil {
					if runtime.DeviceCredentials != nil {
						profileSnapshot := profile
						profileSnapshot.DefaultIdentity = config.IdentityUser
						profileSnapshot.AppID = ""
						err = runtime.DeviceCredentials.Save(profile.Name, auth.DeviceCredential{Profile: &profileSnapshot, Token: &token})
					} else {
						identityStore, ok := runtime.Tokens.(interface {
							SaveForIdentity(string, config.IdentityKind, auth.Token) error
						})
						if !ok {
							return fmt.Errorf("token store 不支持 user 身份")
						}
						err = identityStore.SaveForIdentity(profile.Name, identity, token)
					}
				}
			} else {
				provider := auth.NewProvider(runtime.Tokens, runtime.HTTP, runtime.Now, runtime.Secrets)
				token, err = provider.Login(loginContext, profile, appSecret)
			}
			if err != nil {
				return err
			}
			if identity == config.IdentityApp && saveAppSecret {
				if runtime.Secrets == nil {
					return fmt.Errorf("当前运行时不支持保存 app secret")
				}
				if err := runtime.Secrets.SaveAppSecret(profile.Name, appSecret); err != nil {
					return fmt.Errorf("保存 app secret: %w", err)
				}
			}
			if identity == config.IdentityApp {
				originalProfile, profileErr := runtime.Profiles.Get(profile.Name)
				if profileErr != nil {
					return profileErr
				}
				if profile.AppID != originalProfile.AppID {
					if err := runtime.Profiles.Add(profile); err != nil {
						return err
					}
				}
			}
			result := map[string]any{
				"profile":       profile.Name,
				"identity":      identity,
				"authenticated": true,
			}
			if !token.ExpiresAt.IsZero() {
				result["expiresAt"] = token.ExpiresAt
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	command.Flags().BoolVar(&secretFromStdin, "app-secret-stdin", false, "从 stdin 读取 app secret")
	command.Flags().BoolVar(&noOpenBrowser, "no-open-browser", false, "不自动打开浏览器，仅输出用户 OAuth 授权链接")
	command.Flags().BoolVar(&saveAppSecret, "save-app-secret", false, "授权成功后将 app secret 保存到本地安全存储（macOS Keychain，其他系统 secrets.json）")
	command.Flags().StringVar(&appIDFlag, "app-id", "", "app 登录使用的 app ID；优先于环境变量和 Profile")
	command.Flags().StringVar(&appSecretFlag, "app-secret", "", "app 登录使用的 app secret；不会输出到日志，优先于 stdin、环境变量和本地保存值")
	return command
}

// newAuthStatusCommand 创建 auth status，绝不输出 token 本身。
// 入参：runtime *Runtime 为 token store 和时钟；root *rootOptions 为 Profile/输出 flags。
// 返回值：*cobra.Command，可检查缓存或环境变量凭证状态。
func newAuthStatusCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "检查当前鉴权状态",
		Long:  "检查当前 Profile 和身份的凭证状态；不会输出 token 本身。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			profile, err := selectedProfile(runtime, root)
			if err != nil {
				return err
			}
			identity, err := selectedIdentity(profile, root.Identity)
			if err != nil {
				return err
			}
			if err := validateDeviceCredentialRuntime(runtime, identity); err != nil {
				return err
			}
			if identity == config.IdentityApp && strings.TrimSpace(os.Getenv("EVERYLINE_ACCESS_TOKEN")) != "" {
				status := map[string]any{
					"profile": profile.Name, "identity": identity, "authenticated": true, "source": "environment", "expiresKnown": false,
				}
				if identity == config.IdentityApp {
					status["appSecretConfigured"] = appSecretConfigured(runtime, profile.Name)
				}
				return render(runtime, root, profile.DefaultOutput, status)
			}
			if identity == config.IdentityUser && runtime.DeviceCredentials != nil {
				credential, deviceErr := runtime.DeviceCredentials.Load(profile.Name)
				if deviceErr == nil {
					now := runtimeNow(runtime)
					status := map[string]any{"profile": profile.Name, "identity": identity, "authenticated": false, "source": "device"}
					if credential.Token != nil && credential.Token.AccessToken != "" {
						status["authenticated"] = credential.Token.ValidAt(now, 0)
						status["expiresKnown"] = !credential.Token.ExpiresAt.IsZero()
						if !credential.Token.ExpiresAt.IsZero() {
							status["expiresAt"] = credential.Token.ExpiresAt
							status["expiresInSeconds"] = maxInt64(0, int64(credential.Token.ExpiresAt.Sub(now).Seconds()))
						}
					} else if credential.Pending != nil {
						pendingStatus := credential.Pending.Status
						if pendingStatus == "" {
							pendingStatus = auth.DevicePending
						}
						if pendingStatus == auth.DevicePending && !now.Before(credential.Pending.ExpiresAt) {
							pendingStatus = auth.DevicePendingExpired
						}
						status["deviceStatus"] = pendingStatus
						status["expiresAt"] = credential.Pending.ExpiresAt
					}
					return render(runtime, root, profile.DefaultOutput, status)
				}
				if !errors.Is(deviceErr, auth.ErrDeviceCredentialNotFound) {
					return deviceErr
				}
			}
			var token auth.Token
			var loadErr error
			if identityStore, ok := runtime.Tokens.(interface {
				LoadForIdentity(string, config.IdentityKind) (auth.Token, error)
			}); ok {
				token, loadErr = identityStore.LoadForIdentity(profile.Name, identity)
			} else {
				token, loadErr = runtime.Tokens.Load(profile.Name)
			}
			now := time.Now()
			if runtime.Now != nil {
				now = runtime.Now()
			}
			authenticated := loadErr == nil && token.ValidAt(now, 0)
			status := map[string]any{"profile": profile.Name, "identity": identity, "authenticated": authenticated, "source": "cache"}
			if identity == config.IdentityApp {
				status["appSecretConfigured"] = appSecretConfigured(runtime, profile.Name)
			}
			if identity == config.IdentityUser && !authenticated && profile.AuthURLFor(identity) != "" {
				status["authURL"] = profile.AuthURLFor(identity)
			}
			if loadErr == nil && token.AccessToken != "" {
				status["expiresKnown"] = !token.ExpiresAt.IsZero()
				if !token.ExpiresAt.IsZero() {
					status["expiresAt"] = token.ExpiresAt
					status["expiresInSeconds"] = maxInt64(0, int64(token.ExpiresAt.Sub(now).Seconds()))
				}
			}
			return render(runtime, root, profile.DefaultOutput, status)
		},
	}
}

// appSecretConfigured 判断当前 Profile 是否存在 app secret，但不返回 secret 内容。
func appSecretConfigured(runtime *Runtime, profileName string) bool {
	if auth.SecretFromEnvironment(profileName) != "" {
		return true
	}
	if runtime.Secrets == nil {
		return false
	}
	_, err := runtime.Secrets.LoadAppSecret(profileName)
	return err == nil
}

// newAuthUseCommand 创建 auth use，持久化当前 Profile 的默认业务身份。
// 入参：runtime *Runtime 为 Profile 存储依赖；root *rootOptions 为 profile/身份 flags。
// 返回值：*cobra.Command，可将后续业务命令默认切换为 app 或 user。
func newAuthUseCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "use [profile]",
		Short: "选择 Profile 的默认身份",
		Long:  "设置指定 Profile 的默认业务身份；会修改本地配置。仅对当前命令临时指定身份请使用 --as。",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if root.Identity == "" {
				return fmt.Errorf("auth use 必须指定 --as app 或 --as user")
			}
			identity, err := config.ParseIdentityKind(root.Identity)
			if err != nil {
				return err
			}
			profileName := root.Profile
			if len(args) == 1 {
				profileName = args[0]
			}
			if profileName == "" {
				profile, currentErr := runtime.Profiles.Current()
				if currentErr != nil {
					return currentErr
				}
				profileName = profile.Name
			}
			if err := runtime.Profiles.SetDefaultIdentity(profileName, identity); err != nil {
				return err
			}
			return render(runtime, root, "table", map[string]any{"profile": profileName, "identity": identity})
		},
	}
}

// newAuthLogoutCommand 创建 auth logout，按能力撤销 Device refresh token 后删除本地凭证。
// 入参：runtime *Runtime 为 token store；root *rootOptions 为 Profile/输出 flags。
// 返回值：*cobra.Command，可幂等注销。
func newAuthLogoutCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "删除当前 Profile 的 token 缓存",
		Long:  "删除当前 Profile 和身份对应的 token 缓存；不会删除 Profile 配置。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			profile, err := selectedProfile(runtime, root)
			if err != nil {
				return err
			}
			identity, err := selectedIdentity(profile, root.Identity)
			if err != nil {
				return err
			}
			if identity == config.IdentityUser && runtime.DeviceCredentials != nil {
				credential, deviceErr := runtime.DeviceCredentials.Load(profile.Name)
				if deviceErr == nil {
					if credential.Token != nil && credential.Token.RefreshToken != "" {
						revocationEndpoint := strings.TrimSpace(profile.OAuthRevocationURL)
						if revocationEndpoint == "" {
							authContext, cancel := context.WithTimeout(command.Context(), root.Timeout)
							defer cancel()
							metadata, discoverErr := auth.DiscoverOAuthMetadata(authContext, runtime.HTTP, profile.OAuthMetadataURL)
							if discoverErr != nil {
								return discoverErr
							}
							revocationEndpoint = strings.TrimSpace(metadata.RevocationEndpoint)
						}
						if revocationEndpoint != "" {
							authContext, cancel := context.WithTimeout(command.Context(), root.Timeout)
							defer cancel()
							if err := auth.RevokeOAuthRefreshToken(authContext, runtime.HTTP, revocationEndpoint, profile.EffectiveOAuthDeviceClientID(), credential.Token.RefreshToken); err != nil {
								return err
							}
						}
					}
					if err := runtime.DeviceCredentials.Delete(profile.Name); err != nil {
						return err
					}
				} else if !errors.Is(deviceErr, auth.ErrDeviceCredentialNotFound) {
					return deviceErr
				}
			}
			if identityStore, ok := runtime.Tokens.(interface {
				DeleteForIdentity(string, config.IdentityKind) error
			}); ok {
				err = identityStore.DeleteForIdentity(profile.Name, identity)
			} else if identity == config.IdentityApp {
				err = runtime.Tokens.Delete(profile.Name)
			} else {
				err = fmt.Errorf("token store 不支持 user 身份")
			}
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, map[string]any{"profile": profile.Name, "identity": identity, "authenticated": false})
		},
	}
}

// maxInt64 返回两个 int64 中较大值，用于避免输出负数剩余秒数。
// 入参：left/right int64 为候选值。
// 返回值：int64，较大值。
func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
