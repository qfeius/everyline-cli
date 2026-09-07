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
		"app 身份使用 app secret；Codex/人工本地 user 使用 OAuth/PKCE，豆包/WorkBuddy（含本地电脑）使用 auth init/complete Device Grant。",
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
	// VerificationLinkText 固定待授权入口文案，供豆包与 WorkBuddy 直接用于链接按钮。
	VerificationLinkText string `json:"verification_link_text,omitempty"`
	ExpiresAt            string `json:"expires_at,omitempty"`
	Reused               bool   `json:"reused,omitempty"`
}

// ensureBrowserOAuthClient 通过 metadata 声明的动态注册端点获取本次 Codex PKCE 使用的浏览器 client_id。
// 入参：ctx context.Context 控制请求；runtime *Runtime 提供 HTTP 客户端；profile config.Profile 为当前配置；registrationEndpoint string 为 metadata 声明的注册端点；force bool 表示是否替换历史缓存值。
// 返回值：config.Profile 为仅在内存补齐浏览器 client 后的配置；error 为注册参数、网络或协议错误。
func ensureBrowserOAuthClient(ctx context.Context, runtime *Runtime, profile config.Profile, registrationEndpoint string, force bool) (config.Profile, error) {
	if !force && strings.TrimSpace(profile.OAuthClientID) != "" {
		return profile, nil
	}
	if !profile.HasOAuthClientRegistrationConfiguration() {
		return profile, fmt.Errorf("Profile %q 的 OAuth client 动态注册配置不完整", profile.Name)
	}
	clientID, err := auth.RegisterOAuthClient(ctx, runtime.HTTP, auth.OAuthClientRegistrationRequest{
		Endpoint: strings.TrimSpace(registrationEndpoint), ClientName: "EveryLine CLI", RedirectURI: profile.OAuthRedirectURL,
		Scope: strings.Join(profile.OAuthScopes, " "),
	})
	if err != nil {
		return profile, err
	}
	// 浏览器 client 与 Device client 始终分开，Codex 动态注册不得覆盖豆包/WorkBuddy 的固定 Device client。
	profile.OAuthClientID = clientID
	return profile, nil
}

/*
newAuthDeviceInitCommand 复用有效凭证或 Device 授权事务，为过期凭证生成新链接，并恢复首次安装未提交的本地状态。
入参：runtime *Runtime 为 HTTP、Profile 和安全凭证存储；root *rootOptions 为公共 flags。
返回值：*cobra.Command，可输出完整浏览器授权 URL、过期时间或已完成状态。
*/
func newAuthDeviceInitCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var restart bool
	command := &cobra.Command{
		Use:   "init",
		Short: "初始化沙箱可用的用户 Device 授权",
		Long:  "初始化 OAuth Device Grant，返回宿主浏览器可直接打开的完整授权 URL；同一 Profile 已有可复用事务时不重复创建，不监听 127.0.0.1 回调。",
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
			var result deviceAuthOutput
			// Device 初始化与 complete/refresh 共用 Profile 级跨进程锁，避免两个 Agent 调用同时创建两笔授权。
			if err := store.WithRefreshLock(profile.Name, func() error {
				// 锁内重读安装门禁，避免等待 complete 时沿用已经失效的首次安装状态并再次发起授权。
				installState, firstInstallRequired, err := pendingFirstInstallAuthorization(runtime)
				if err != nil {
					return err
				}
				existing, loadErr := store.Load(profile.Name)
				if loadErr != nil && !errors.Is(loadErr, auth.ErrDeviceCredentialNotFound) {
					return loadErr
				}
				// 当前事件的 token 仍有效时才补做本地提交；过期后继续创建新授权事务。
				if firstInstallRequired && loadErr == nil && existing.Pending == nil && existing.Token != nil && existing.Token.ValidAt(runtimeNow(runtime), 0) && existing.TokenFirstInstallEventID == installState.EventID {
					if err := completeFirstInstallAuthorization(runtime, existing.TokenFirstInstallEventID); err != nil {
						return err
					}
					result = deviceAuthOutput{Status: "succeeded", ExpiresAt: formatOptionalTime(existing.Token.ExpiresAt)}
					return nil
				}
				forceRestart := restart || firstInstallRequired
				if loadErr == nil && existing.Pending != nil {
					status := existing.Pending.EffectiveStatus(runtimeNow(runtime))
					sameFirstInstallTransaction := firstInstallRequired && installState.EventID != "" && existing.Pending.FirstInstallEventID == installState.EventID
					activeTransaction := status == auth.DevicePending || status == auth.DevicePendingChecking || status == auth.DevicePendingUncertain
					// 同一首次安装事件即使重放 --restart，也复用仍活跃的事务；终态只有显式 --restart 才新建。
					if sameFirstInstallTransaction && (activeTransaction || !restart) {
						forceRestart = false
					}
					if !forceRestart {
						result = deviceAuthOutput{
							Status: string(status), VerificationURIComplete: existing.Pending.VerificationURIComplete,
							ExpiresAt: formatOptionalTime(existing.Pending.ExpiresAt), Reused: true,
						}
						return nil
					}
				}
				// 只有仍有效的 token 才表示已登录；到期后继续创建新事务，让用户从新链接手动授权。
				if !forceRestart && loadErr == nil && existing.Token != nil && existing.Token.ValidAt(runtimeNow(runtime), 0) {
					result = deviceAuthOutput{Status: "succeeded", ExpiresAt: formatOptionalTime(existing.Token.ExpiresAt)}
					return nil
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
					return fmt.Errorf("OAuth metadata 尚未发布 device_authorization_endpoint；请在认证服务启用 Device Grant 并发布该端点，或通过 config add --oauth-device-authorization-url 写入平台确认配置")
				}
				if strings.TrimSpace(metadata.TokenEndpoint) == "" {
					return fmt.Errorf("OAuth metadata 缺少 token_endpoint")
				}
				deviceClientID := profile.EffectiveOAuthDeviceClientID()
				response, err := auth.StartDeviceAuthorization(authContext, runtime.HTTP, auth.DeviceAuthorizationRequest{
					Endpoint: deviceEndpoint, ClientID: deviceClientID,
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
				// 首次安装强制新授权时保留旧 token 作为回滚材料，但业务门禁不会使用它；成功后由新 token 覆盖。
				if !firstInstallRequired {
					existing.Token = nil
					existing.TokenFirstInstallEventID = ""
				}
				firstInstallEventID := ""
				if firstInstallRequired {
					firstInstallEventID = installState.EventID
				}
				existing.Pending = &auth.DevicePendingTransaction{
					Status: auth.DevicePending, DeviceCode: response.DeviceCode,
					VerificationURIComplete: response.VerificationURIComplete,
					TokenEndpoint:           metadata.TokenEndpoint, RevocationEndpoint: revocationEndpoint,
					ClientID: deviceClientID, ExpiresAt: expiresAt, FirstInstallEventID: firstInstallEventID,
				}
				if err := store.Save(profile.Name, existing); err != nil {
					return err
				}
				result = deviceAuthOutput{
					Status: string(auth.DevicePending), VerificationURIComplete: response.VerificationURIComplete, ExpiresAt: formatOptionalTime(expiresAt),
				}
				return nil
			}); err != nil {
				return err
			}
			// 新建和复用的待授权事务使用同一文案；终态或结果不确定时不再提示用户点击。
			if result.Status == string(auth.DevicePending) && result.VerificationURIComplete != "" {
				result.VerificationLinkText = "点击授权"
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	command.Flags().BoolVar(&restart, "restart", false, "明确废弃已有事务并重新开始 Device 授权")
	withNotes(command,
		"Device Grant 只使用 Profile 或 dev/test 预设中的独立 Device client，不动态注册且不复用 Codex 浏览器 client。",
		"豆包本地电脑同样使用 Device Grant；首次 auth status 前固定 SESSION_ID 和初始工作目录，后续每条命令显式复用。",
		"待授权时 verification_link_text 固定为点击授权；豆包与 WorkBuddy 都以该字段作为按钮或 Markdown 链接文字。",
		"把 verification_uri_complete 作为一个完整链接原样展示给用户，不拆分、不改写 query；链接目标保留完整 URL，展示文字使用点击授权。",
		"并发初始化和同一首次安装事件重放会复用已有事务，并在结构化输出中标记 reused=true。",
		"user token 过期后重新执行 auth init 会生成新授权链接；尚未过期的 token 和已有待完成事务继续复用。",
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
			return completeDeviceAuthorization(command.Context(), runtime, root, profile, store)
		},
	}
}

/*
completeDeviceAuthorization 在 Profile 级跨进程锁内兑换一次 Device code，并幂等完成对应安装事件的本地提交。
入参：ctx context.Context 控制远端请求；runtime *Runtime 提供 HTTP、时钟和输出；root *rootOptions 提供超时与格式；profile config.Profile 为当前配置；store auth.DeviceCredentialStore 为会话凭证存储。
返回值：error，状态读取、OAuth 兑换、凭证保存、安装门禁提交或结果渲染失败时非 nil。
*/
func completeDeviceAuthorization(ctx context.Context, runtime *Runtime, root *rootOptions, profile config.Profile, store auth.DeviceCredentialStore) error {
	return store.WithRefreshLock(profile.Name, func() error {
		credential, err := store.Load(profile.Name)
		if err != nil {
			if errors.Is(err, auth.ErrDeviceCredentialNotFound) {
				return fmt.Errorf("没有待完成的 Device 授权；auth init 与 auth complete 必须复用同一 Device 会话标识（WorkBuddy CODEBUDDY_SESSION_ID；豆包 SESSION_ID 与初始工作目录）；恢复 auth init 使用的原标识后重新执行 auth complete，仅在 CLI 明确返回 denied、expired 或 invalid_grant 后开始新事务: %w", err)
			}
			return fmt.Errorf("读取待完成的 Device 授权: %w", err)
		}
		if credential.Pending == nil {
			if credential.Token != nil && credential.Token.AccessToken != "" {
				if !credential.Token.ValidAt(runtimeNow(runtime), 0) {
					return fmt.Errorf("%w；Device 凭证已过期，请执行 auth init --restart --profile %s --as user", auth.ErrUserAuthentication, profile.Name)
				}
				// token 与事件证明在同一份加密凭证中；重试只修复门禁，既不兑换旧 code 也不接受无证明的旧 token。
				if err := completeFirstInstallAuthorization(runtime, credential.TokenFirstInstallEventID); err != nil {
					return err
				}
				return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{Status: "succeeded", ExpiresAt: formatOptionalTime(credential.Token.ExpiresAt)})
			}
			return fmt.Errorf("没有待完成的 Device 授权；auth init 与 auth complete 必须复用同一 Device 会话标识（WorkBuddy CODEBUDDY_SESSION_ID；豆包 SESSION_ID 与初始工作目录）；恢复 auth init 使用的原标识后重新执行 auth complete，仅在 CLI 明确返回 denied、expired 或 invalid_grant 后开始新事务")
		}
		pending := credential.Pending
		// 先落盘到期状态，再处理 uncertain/checking，避免异常事务永久停留在不可兑换的状态。
		if status := pending.EffectiveStatus(runtimeNow(runtime)); status != pending.Status {
			pending.Status = status
			if err := store.Save(profile.Name, credential); err != nil {
				return err
			}
		}
		if pending.Status == auth.DevicePendingChecking || pending.Status == auth.DevicePendingUncertain {
			return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{Status: string(auth.DevicePendingUncertain)})
		}
		if pending.Status != auth.DevicePending {
			return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{Status: string(pending.Status), ExpiresAt: formatOptionalTime(pending.ExpiresAt)})
		}
		pending.Status = auth.DevicePendingChecking
		if err := store.Save(profile.Name, credential); err != nil {
			return err
		}
		authContext, cancel := context.WithTimeout(ctx, root.Timeout)
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
		// 先与 token 原子保存事件证明；门禁提交失败或进程退出后，下一次命令仍可恢复这一提交。
		credential.TokenFirstInstallEventID = pending.FirstInstallEventID
		if err := store.Save(profile.Name, credential); err != nil {
			return fmt.Errorf("保存 Device 授权结果失败，状态可能不确定，请先修复安全存储再重新开始授权: %w", err)
		}
		if err := completeFirstInstallAuthorization(runtime, credential.TokenFirstInstallEventID); err != nil {
			return err
		}
		return render(runtime, root, profile.DefaultOutput, deviceAuthOutput{Status: "succeeded", ExpiresAt: formatOptionalTime(token.ExpiresAt)})
	})
}

// requireDeviceCredentialStore 返回已初始化的沙箱安全凭证存储并保留初始化错误。
// 入参：runtime *Runtime 为 Device store 和初始化状态。
// 返回值：auth.DeviceCredentialStore 为可用存储；error 为运行时不支持或安全配置缺失。
func requireDeviceCredentialStore(runtime *Runtime) (auth.DeviceCredentialStore, error) {
	if runtime.DeviceCredentialError != nil {
		return nil, runtime.DeviceCredentialError
	}
	if runtime.DeviceCredentials == nil {
		return nil, fmt.Errorf("Device 授权需要豆包 SKILL_SESSION_WORKSPACE/SESSION_ID 或 WorkBuddy CODEBUDDY_SESSION_ID 运行时；豆包本地电脑也使用 Device Grant，请在首次 auth status 前固定 SESSION_ID 和初始工作目录，并在后续每条命令中显式复用")
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

/*
newAuthLoginCommand 创建 app secret 或本地 OAuth 登录命令，并为人工浏览器授权提供独立默认等待时间。
入参：runtime *Runtime 为 I/O、HTTP 和 token store；root *rootOptions 为 Profile、输出和显式超时 flags。
返回值：*cobra.Command，可获取并缓存 app 或 user token；豆包/WorkBuddy user 引导到 Device Grant。
*/
func newAuthLoginCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var secretFromStdin bool
	var noOpenBrowser bool
	var saveAppSecret bool
	var appIDFlag string
	var appSecretFlag string
	command := &cobra.Command{
		Use:   "login",
		Short: "获取并安全缓存 app/user token",
		Long:  "获取并安全缓存 app 或 user 身份凭证；app 使用 app secret，Codex/人工本地 user 使用 OAuth/PKCE loopback 授权。豆包/WorkBuddy（含本地电脑）使用 auth init/complete Device Grant。",
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
			if identity == config.IdentityUser && runtime.DeviceCredentials != nil {
				// 授权协议由宿主决定；豆包在本地执行时也保持与 WorkBuddy 一致的 Device Grant。
				return fmt.Errorf("%w；当前豆包/WorkBuddy 运行时使用 Device Grant，请执行 auth init --profile %s --as user --output json", auth.ErrUserAuthentication, profile.Name)
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
			browserOAuthClientRegistered := false
			if identity == config.IdentityUser {
				if strings.TrimSpace(profile.OAuthMetadataURL) != "" && profile.HasOAuthClientRegistrationConfiguration() {
					registrationContext, cancel := context.WithTimeout(command.Context(), root.Timeout)
					defer cancel()
					// 浏览器 client 的注册端点以 authorization server metadata 为准，与合同 CLI 的发现流程保持一致。
					metadata, discoveryErr := auth.DiscoverOAuthMetadata(registrationContext, runtime.HTTP, profile.OAuthMetadataURL)
					if discoveryErr != nil {
						return discoveryErr
					}
					if strings.TrimSpace(metadata.RegistrationEndpoint) == "" {
						return fmt.Errorf("OAuth metadata 缺少 registration_endpoint")
					}
					// 显式登录总是创建当前环境的 public client，避免升级后继续使用历史环境留下的无效 client_id。
					profile, err = ensureBrowserOAuthClient(registrationContext, runtime, profile, metadata.RegistrationEndpoint, true)
					if err != nil {
						return err
					}
					browserOAuthClientRegistered = true
				}
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
					appSecret, err = readSecret(runtime.Input, runtime.Error)
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
			// user 登录包含人工阅读和浏览器确认，默认给出三分钟；显式 --timeout 仍由调用方决定。
			loginTimeout := root.Timeout
			if identity == config.IdentityUser && !command.Flags().Changed("timeout") {
				loginTimeout = 3 * time.Minute
			}
			loginContext, cancel := context.WithTimeout(command.Context(), loginTimeout)
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
			if identity == config.IdentityUser && browserOAuthClientRegistered {
				// token 已绑定本次签发 client；先保存 token 再更新 Profile，授权失败时不会污染旧配置。
				if err := runtime.Profiles.Add(profile); err != nil {
					return err
				}
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
			if err := completeFirstInstallAuthorization(runtime); err != nil {
				return err
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
	command.Flags().BoolVar(&secretFromStdin, "app-secret-stdin", false, "从 stdin 读取 app secret；交互终端隐藏输入并按回车结束")
	command.Flags().BoolVar(&noOpenBrowser, "no-open-browser", false, "不自动打开浏览器，仅输出用户 OAuth 授权链接")
	command.Flags().BoolVar(&saveAppSecret, "save-app-secret", false, "授权成功后将 app secret 保存到本地安全存储（macOS Keychain，其他系统 secrets.json）")
	command.Flags().StringVar(&appIDFlag, "app-id", "", "app 登录使用的 app ID；优先于环境变量和 Profile")
	command.Flags().StringVar(&appSecretFlag, "app-secret", "", "app 登录使用的 app secret；不会输出到日志，优先于 stdin、环境变量和本地保存值")
	withNotes(command,
		"user 浏览器登录默认等待 3 分钟，显式 --timeout 可覆盖；动态注册和 app 登录仍使用普通请求超时。",
		"Codex 本地 user 每次显式登录都会读取 OAuth metadata 的 registration_endpoint，动态注册 public client，并用返回的 client_id 完成本次 OAuth/PKCE。",
		"豆包本地电脑与 WorkBuddy 同样使用 auth init/complete；豆包在首次 auth status 前固定 SESSION_ID 和初始工作目录。",
		"动态注册只更新浏览器 OAuth client，不覆盖显式 Device Grant client。",
	)
	return command
}

/*
newAuthStatusCommand 查询当前身份及凭证运行时的状态，统一识别 Device 事务到期且不继承浏览器缓存。
入参：runtime *Runtime 为 token store 和时钟；root *rootOptions 为 Profile/输出 flags。
返回值：*cobra.Command，可检查缓存或环境变量凭证状态，输出不包含 token。
*/
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
			if installState, firstInstallRequired, err := pendingFirstInstallAuthorization(runtime); err != nil {
				return err
			} else if firstInstallRequired {
				return render(runtime, root, profile.DefaultOutput, map[string]any{
					"profile":               profile.Name,
					"identity":              identity,
					"authenticated":         false,
					"source":                "first_install",
					"firstInstall":          installState.FirstInstall,
					"authorizationRequired": true,
					"nextAction":            firstInstallAuthorizationCommand(runtime, profile, identity),
				})
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
						status["deviceStatus"] = credential.Pending.EffectiveStatus(now)
						status["expiresAt"] = credential.Pending.ExpiresAt
					}
					return render(runtime, root, profile.DefaultOutput, status)
				}
				if !errors.Is(deviceErr, auth.ErrDeviceCredentialNotFound) {
					return deviceErr
				}
				// Device 会话尚未授权时直接返回，防止本地 OAuth 旧 token 让 Agent 跳过 Device 授权。
				return render(runtime, root, profile.DefaultOutput, map[string]any{
					"profile": profile.Name, "identity": identity, "authenticated": false, "source": "device",
				})
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

// newAuthLogoutCommand 创建 auth logout，在刷新锁内删除本地凭证并尽力撤销 Device refresh token。
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
			var deviceCleanupErr error
			var revocationErr error
			if identity == config.IdentityUser && runtime.DeviceCredentials != nil {
				// 撤销与删除必须和 refresh 共用同一把锁，确保进行中的刷新先结束且后续刷新看见已删除状态。
				deviceCleanupErr = runtime.DeviceCredentials.WithRefreshLock(profile.Name, func() error {
					credential, deviceErr := runtime.DeviceCredentials.Load(profile.Name)
					if deviceErr != nil {
						if errors.Is(deviceErr, auth.ErrDeviceCredentialNotFound) {
							return nil
						}
						return errors.Join(deviceErr, runtime.DeviceCredentials.Delete(profile.Name))
					}
					if credential.Token != nil && credential.Token.RefreshToken != "" {
						revocationErr = revokeDeviceRefreshToken(command.Context(), runtime, profile, credential.Token.RefreshToken, root.Timeout)
					}
					return runtime.DeviceCredentials.Delete(profile.Name)
				})
			}
			cacheCleanupErr := deleteCachedIdentityToken(runtime, profile.Name, identity)
			if cleanupErr := errors.Join(deviceCleanupErr, cacheCleanupErr); cleanupErr != nil {
				return cleanupErr
			}
			if revocationErr != nil {
				return fmt.Errorf("本地凭证已清理，但远端 refresh token 撤销失败: %w", revocationErr)
			}
			return render(runtime, root, profile.DefaultOutput, map[string]any{"profile": profile.Name, "identity": identity, "authenticated": false})
		},
	}
}

// revokeDeviceRefreshToken 在一个总超时内发现撤销端点并尽力撤销当前 Device refresh token。
// 入参：ctx context.Context 控制请求；runtime *Runtime 提供 HTTP 客户端；profile config.Profile 提供 OAuth 配置；refreshToken string 为待撤销凭证；timeout time.Duration 为总时间预算。
// 返回值：error，metadata 发现或远端撤销失败时非 nil；服务端未提供撤销端点时为 nil。
func revokeDeviceRefreshToken(ctx context.Context, runtime *Runtime, profile config.Profile, refreshToken string, timeout time.Duration) error {
	authContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	revocationEndpoint := strings.TrimSpace(profile.OAuthRevocationURL)
	if revocationEndpoint == "" {
		metadata, err := auth.DiscoverOAuthMetadata(authContext, runtime.HTTP, profile.OAuthMetadataURL)
		if err != nil {
			return err
		}
		revocationEndpoint = strings.TrimSpace(metadata.RevocationEndpoint)
	}
	if revocationEndpoint == "" {
		return nil
	}
	return auth.RevokeOAuthRefreshToken(authContext, runtime.HTTP, revocationEndpoint, profile.EffectiveOAuthDeviceClientID(), refreshToken)
}

// deleteCachedIdentityToken 在 user 刷新锁内删除兼容 token 缓存，保持 app 身份的既有删除行为。
// 入参：runtime *Runtime 提供 token store；profileName string 为 Profile 名称；identity config.IdentityKind 为待清理身份。
// 返回值：error，存储能力不足、加锁或删除失败时非 nil。
func deleteCachedIdentityToken(runtime *Runtime, profileName string, identity config.IdentityKind) error {
	identityStore, ok := runtime.Tokens.(interface {
		DeleteForIdentity(string, config.IdentityKind) error
	})
	if !ok {
		if identity == config.IdentityApp {
			return runtime.Tokens.Delete(profileName)
		}
		return fmt.Errorf("token store 不支持 user 身份")
	}
	if identity != config.IdentityUser {
		return identityStore.DeleteForIdentity(profileName, identity)
	}
	lockingStore, ok := runtime.Tokens.(interface {
		DeleteForIdentity(string, config.IdentityKind) error
		WithRefreshLock(string, config.IdentityKind, func() error) error
	})
	if !ok {
		return identityStore.DeleteForIdentity(profileName, identity)
	}
	return lockingStore.WithRefreshLock(profileName, identity, func() error {
		return lockingStore.DeleteForIdentity(profileName, identity)
	})
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
