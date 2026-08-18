package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/auth"

	"github.com/spf13/cobra"
)

// newAuthCommand 创建 tenant token 登录、状态和注销命令组。
// 入参：runtime *Runtime 为凭证存储和 HTTP 依赖；root *rootOptions 为 Profile/输出 flags。
// 返回值：*cobra.Command，包含 login/status/logout。
func newAuthCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "管理 tenant 访问凭证"}
	command.AddCommand(
		newAuthLoginCommand(runtime, root),
		newAuthStatusCommand(runtime, root),
		newAuthLogoutCommand(runtime, root),
	)
	return command
}

// newAuthLoginCommand 创建 auth login，只从环境变量或 stdin 接收 app secret。
// 入参：runtime *Runtime 为 I/O、HTTP 和 token store；root *rootOptions 为 Profile/输出 flags。
// 返回值：*cobra.Command，可获取并缓存 tenant token。
func newAuthLoginCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var secretFromStdin bool
	command := &cobra.Command{
		Use:   "login",
		Short: "获取并安全缓存 tenant token",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			profile, err := selectedProfile(runtime, root)
			if err != nil {
				return err
			}
			secret := auth.SecretFromEnvironment(profile.Name)
			if secretFromStdin {
				secret, err = readSecret(runtime.Input)
				if err != nil {
					return fmt.Errorf("%w: %v", auth.ErrCredentialsMissing, err)
				}
			}
			if secret == "" {
				return auth.ErrCredentialsMissing
			}
			provider := auth.NewProvider(runtime.Tokens, runtime.HTTP, runtime.Now)
			loginContext, cancel := context.WithTimeout(command.Context(), root.Timeout)
			defer cancel()
			token, err := provider.Login(loginContext, profile, secret)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, map[string]any{
				"profile":       profile.Name,
				"authenticated": true,
				"expiresAt":     token.ExpiresAt,
			})
		},
	}
	command.Flags().BoolVar(&secretFromStdin, "app-secret-stdin", false, "从 stdin 读取 app secret")
	return command
}

// newAuthStatusCommand 创建 auth status，绝不输出 token 本身。
// 入参：runtime *Runtime 为 token store 和时钟；root *rootOptions 为 Profile/输出 flags。
// 返回值：*cobra.Command，可检查缓存或环境变量凭证状态。
func newAuthStatusCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "检查当前鉴权状态",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			profile, err := selectedProfile(runtime, root)
			if err != nil {
				return err
			}
			if strings.TrimSpace(os.Getenv("EVERYLINE_ACCESS_TOKEN")) != "" {
				return render(runtime, root, profile.DefaultOutput, map[string]any{
					"profile": profile.Name, "authenticated": true, "source": "environment",
				})
			}
			token, loadErr := runtime.Tokens.Load(profile.Name)
			authenticated := loadErr == nil && token.ValidAt(runtime.Now(), 0)
			status := map[string]any{"profile": profile.Name, "authenticated": authenticated, "source": "cache"}
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

// newAuthLogoutCommand 创建 auth logout，仅删除当前 Profile 的 token 缓存。
// 入参：runtime *Runtime 为 token store；root *rootOptions 为 Profile/输出 flags。
// 返回值：*cobra.Command，可幂等注销。
func newAuthLogoutCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "删除当前 Profile 的 token 缓存",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			profile, err := selectedProfile(runtime, root)
			if err != nil {
				return err
			}
			if err := runtime.Tokens.Delete(profile.Name); err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, map[string]any{"profile": profile.Name, "authenticated": false})
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
