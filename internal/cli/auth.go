package cli

import (
	"context"
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
		"app 身份使用 app secret，user 身份使用认证页面交接的 access token。",
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

// newAuthLoginCommand 创建 auth login；app 使用 app secret，user 使用自有认证页面交接的 token。
// 入参：runtime *Runtime 为 I/O、HTTP 和 token store；root *rootOptions 为 Profile/输出 flags。
// 返回值：*cobra.Command，可获取并缓存 app 或 user token。
func newAuthLoginCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var secretFromStdin bool
	var accessTokenFromStdin bool
	command := &cobra.Command{
		Use:   "login",
		Short: "获取并安全缓存 app/user token",
		Long:  "获取并安全缓存 app 或 user 身份凭证；app 使用 app secret，user 使用认证页面交接的 access token。",
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
			if secretFromStdin && accessTokenFromStdin {
				return fmt.Errorf("--app-secret-stdin 与 --access-token-stdin 只能选择一个")
			}
			if identity == config.IdentityUser && secretFromStdin {
				return fmt.Errorf("user 身份请使用 --access-token-stdin")
			}
			if identity == config.IdentityApp && accessTokenFromStdin {
				return fmt.Errorf("app 身份请使用 --app-secret-stdin")
			}
			credential := ""
			if identity == config.IdentityUser {
				credential = strings.TrimSpace(os.Getenv("EVERYLINE_USER_ACCESS_TOKEN"))
				if accessTokenFromStdin {
					credential, err = readAccessToken(runtime.Input)
					if err != nil {
						return fmt.Errorf("%w: %v", auth.ErrUserAuthentication, err)
					}
				}
				if credential == "" {
					page := profile.AuthURLFor(identity)
					if page != "" {
						return fmt.Errorf("%w；请先打开认证页面：%s，完成后使用 --access-token-stdin 交接 token", auth.ErrUserAuthentication, page)
					}
					return auth.ErrUserAuthentication
				}
			} else {
				credential = auth.SecretFromEnvironment(profile.Name)
				if secretFromStdin {
					credential, err = readSecret(runtime.Input)
					if err != nil {
						return fmt.Errorf("%w: %v", auth.ErrCredentialsMissing, err)
					}
				}
				if credential == "" {
					return auth.ErrCredentialsMissing
				}
			}
			provider := auth.NewProvider(runtime.Tokens, runtime.HTTP, runtime.Now)
			loginContext, cancel := context.WithTimeout(command.Context(), root.Timeout)
			defer cancel()
			token, err := provider.LoginForIdentity(loginContext, profile, identity, credential)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, map[string]any{
				"profile":       profile.Name,
				"identity":      identity,
				"authenticated": true,
				"expiresAt":     token.ExpiresAt,
			})
		},
	}
	command.Flags().BoolVar(&secretFromStdin, "app-secret-stdin", false, "从 stdin 读取 app secret")
	command.Flags().BoolVar(&accessTokenFromStdin, "access-token-stdin", false, "从 everyline 自有认证页面交接用户 access token")
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
			environmentToken := "EVERYLINE_ACCESS_TOKEN"
			if identity == config.IdentityUser {
				environmentToken = "EVERYLINE_USER_ACCESS_TOKEN"
			}
			if strings.TrimSpace(os.Getenv(environmentToken)) != "" {
				return render(runtime, root, profile.DefaultOutput, map[string]any{
					"profile": profile.Name, "identity": identity, "authenticated": true, "source": "environment",
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
			authenticated := loadErr == nil && token.ValidAt(runtime.Now(), 0)
			status := map[string]any{"profile": profile.Name, "identity": identity, "authenticated": authenticated, "source": "cache"}
			if identity == config.IdentityUser && !authenticated && profile.AuthURLFor(identity) != "" {
				status["authURL"] = profile.AuthURLFor(identity)
			}
			if loadErr == nil {
				status["expiresAt"] = token.ExpiresAt
				remaining := time.Until(token.ExpiresAt)
				if runtime.Now != nil {
					remaining = token.ExpiresAt.Sub(runtime.Now())
				}
				status["expiresInSeconds"] = maxInt64(0, int64(remaining.Seconds()))
			}
			return render(runtime, root, profile.DefaultOutput, status)
		},
	}
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
