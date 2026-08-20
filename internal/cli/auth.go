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

// newAuthCommand 创建 app/user 身份登录、状态、切换和注销命令组。
// 入参：runtime *Runtime 为凭证存储和 HTTP 依赖；root *rootOptions 为 Profile/输出 flags。
// 返回值：*cobra.Command，包含 login/status/use/logout。
func newAuthCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "auth",
		Short: "管理 app/user 身份凭证",
		Long: `管理 app/user 身份凭证。

		身份选择优先级为：--as > Profile 默认身份 > app。
auth use 会修改 Profile 的默认身份；仅对当前命令临时指定身份请使用 --as。`,
	}
	withNotes(command,
		"app 身份使用 app secret，user 身份使用 OAuth/PKCE 浏览器授权。",
		"auth logout 只删除当前 Profile 的 token 缓存。",
	)
	command.AddCommand(
		newAuthLoginCommand(runtime, root),
		newAuthStatusCommand(runtime, root),
		newAuthUseCommand(runtime, root),
		newAuthLogoutCommand(runtime, root),
	)
	return command
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
					identityStore, ok := runtime.Tokens.(interface {
						SaveForIdentity(string, config.IdentityKind, auth.Token) error
					})
					if !ok {
						return fmt.Errorf("token store 不支持 user 身份")
					}
					err = identityStore.SaveForIdentity(profile.Name, identity, token)
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
			if identity == config.IdentityApp && strings.TrimSpace(os.Getenv("EVERYLINE_ACCESS_TOKEN")) != "" {
				status := map[string]any{
					"profile": profile.Name, "identity": identity, "authenticated": true, "source": "environment", "expiresKnown": false,
				}
				if identity == config.IdentityApp {
					status["appSecretConfigured"] = appSecretConfigured(runtime, profile.Name)
				}
				return render(runtime, root, profile.DefaultOutput, status)
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

// newAuthLogoutCommand 创建 auth logout，仅删除当前 Profile 的 token 缓存。
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
