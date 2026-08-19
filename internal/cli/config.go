package cli

import (
	"errors"
	"fmt"

	"git.qtech.cn/ai/everyline-cli/internal/config"

	"github.com/spf13/cobra"
)

// newConfigCommand 创建 Profile 本地管理命令组。
// 入参：runtime *Runtime 为配置和输出依赖；root *rootOptions 为公共输出 flags。
// 返回值：*cobra.Command，包含 add/list/use/show。
func newConfigCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "config", Short: "管理多环境 Profile"}
	command.AddCommand(
		newConfigAddCommand(runtime, root),
		newConfigListCommand(runtime, root),
		newConfigUseCommand(runtime, root),
		newConfigShowCommand(runtime, root),
	)
	return command
}

// newConfigAddCommand 创建 config add，并拒绝把 app secret 写入 Profile。
// 入参：runtime *Runtime 为配置仓库；root *rootOptions 为输出选项。
// 返回值：*cobra.Command，可新增或覆盖 Profile。
func newConfigAddCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var environment string
	var baseURL string
	var authURL string
	var tokenURL string
	var appID string
	var userBaseURL string
	var defaultIdentity string
	var defaultOutput string
	command := &cobra.Command{
		Use:   "add <name>",
		Short: "新增或更新 Profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if environment != "" {
				// 预设环境统一提供 base/token 地址，避免同一个 Profile 混用不同环境的 URL。
				if baseURL != "" || tokenURL != "" {
					return fmt.Errorf("--env 不能与 --base-url 或 --token-url 同时使用")
				}
				preset, err := config.ResolveEnvironment(environment)
				if err != nil {
					return err
				}
				baseURL = preset.BaseURL
				if authURL == "" {
					authURL = preset.AuthURL
				}
				tokenURL = preset.TokenURL
			}
			if baseURL == "" || tokenURL == "" {
				return fmt.Errorf("必须指定 --env，或同时指定 --base-url 和 --token-url")
			}
			profile := config.Profile{
				Name:            args[0],
				BaseURL:         baseURL,
				UserBaseURL:     userBaseURL,
				AuthURL:         authURL,
				TokenURL:        tokenURL,
				AppID:           appID,
				DefaultIdentity: config.IdentityApp,
				DefaultOutput:   defaultOutput,
			}
			if existing, existingErr := runtime.Profiles.Get(profile.Name); existingErr == nil {
				profile.DefaultIdentity = existing.DefaultIdentity
				if profile.UserBaseURL == "" {
					profile.UserBaseURL = existing.UserBaseURL
				}
				if profile.AuthURL == "" {
					profile.AuthURL = existing.AuthURL
				}
			} else if !errors.Is(existingErr, config.ErrProfileNotFound) {
				return existingErr
			}
			if defaultIdentity != "" {
				identity, err := config.ParseIdentityKind(defaultIdentity)
				if err != nil {
					return err
				}
				profile.DefaultIdentity = identity
			}
			if err := profile.Validate(); err != nil {
				return err
			}
			// 同名 Profile 切换了凭据身份时，先清除旧 token，避免把它发送给新环境。
			existing, getErr := runtime.Profiles.Get(profile.Name)
			if getErr == nil && profileCredentialIdentityChanged(existing, profile) {
				if err := deleteProfileTokens(runtime, profile.Name); err != nil {
					return err
				}
			} else if getErr != nil && !errors.Is(getErr, config.ErrProfileNotFound) {
				return getErr
			}
			if err := runtime.Profiles.Add(profile); err != nil {
				return err
			}
			return render(runtime, root, "table", profile)
		},
	}
	command.Flags().StringVar(&environment, "env", "", "使用预设环境：dev|test|blue|prod")
	command.Flags().StringVar(&baseURL, "base-url", "", "智审开放平台基础 URL")
	command.Flags().StringVar(&userBaseURL, "user-base-url", "", "用户身份业务基础 URL；为空时复用 base-url")
	command.Flags().StringVar(&authURL, "auth-url", "", "智审用户认证页面基础 URL")
	command.Flags().StringVar(&tokenURL, "token-url", "", "tenant token 完整 URL")
	command.Flags().StringVar(&appID, "app-id", "", "开放平台 app ID")
	command.Flags().StringVar(&defaultIdentity, "default-identity", "", "默认业务身份：app|user")
	command.Flags().StringVar(&defaultOutput, "default-output", "table", "默认输出格式")
	_ = command.MarkFlagRequired("app-id")
	return command
}

// profileCredentialIdentityChanged 判断覆盖 Profile 是否改变了 token 的适用身份。
// 入参：before/after config.Profile 分别为现有与新配置。
// 返回值：bool，app/user 业务基址、token 地址或 app ID 任一变化时为 true。
func profileCredentialIdentityChanged(before config.Profile, after config.Profile) bool {
	return before.BaseURL != after.BaseURL || before.UserBaseURL != after.UserBaseURL || before.TokenURL != after.TokenURL || before.AppID != after.AppID
}

// deleteProfileTokens 清理 Profile 下 app/user 两套 token，防止连接配置更新后残留旧身份凭证。
// 入参：runtime *Runtime 为 token 存储依赖；profileName string 为 Profile 名称。
// 返回值：error，任一身份清理失败时非 nil。
func deleteProfileTokens(runtime *Runtime, profileName string) error {
	if identityStore, ok := runtime.Tokens.(interface {
		DeleteForIdentity(string, config.IdentityKind) error
	}); ok {
		if err := identityStore.DeleteForIdentity(profileName, config.IdentityApp); err != nil {
			return err
		}
		return identityStore.DeleteForIdentity(profileName, config.IdentityUser)
	}
	return runtime.Tokens.Delete(profileName)
}

// newConfigListCommand 创建 config list，并显式标记当前 Profile。
// 入参：runtime *Runtime 为配置仓库；root *rootOptions 为输出选项。
// 返回值：*cobra.Command，可列出稳定排序的 Profile。
func newConfigListCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出全部 Profile",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			profiles, err := runtime.Profiles.List()
			if err != nil {
				return err
			}
			currentName := ""
			if current, currentErr := runtime.Profiles.Current(); currentErr == nil {
				currentName = current.Name
			} else if !errors.Is(currentErr, config.ErrNoActiveProfile) {
				return currentErr
			}
			rows := make([]map[string]any, 0, len(profiles))
			for _, profile := range profiles {
				rows = append(rows, map[string]any{
					"name":             profile.Name,
					"current":          profile.Name == currentName,
					"base_url":         profile.BaseURL,
					"token_url":        profile.TokenURL,
					"app_id":           profile.AppID,
					"default_identity": identityName(profile.DefaultIdentity),
					"user_base_url":    profile.UserBaseURL,
					"auth_url":         profile.AuthURL,
					"default_output":   profile.DefaultOutput,
				})
			}
			return render(runtime, root, "table", rows)
		},
	}
}

// identityName 返回 Profile 的可读身份，空值按历史默认 app 处理。
// 入参：identity config.IdentityKind 为 Profile 默认身份。
// 返回值：string，为稳定输出文本。
func identityName(identity config.IdentityKind) string {
	if identity == "" {
		return string(config.IdentityApp)
	}
	return string(identity)
}

// newConfigUseCommand 创建 config use，切换默认 Profile。
// 入参：runtime *Runtime 为配置仓库；root *rootOptions 为输出选项。
// 返回值：*cobra.Command，可持久化当前 Profile。
func newConfigUseCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "选择默认 Profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := runtime.Profiles.Use(args[0]); err != nil {
				return err
			}
			return render(runtime, root, "table", map[string]any{"profile": args[0], "current": true})
		},
	}
}

// newConfigShowCommand 创建 config show，可按参数、--profile 或当前选择读取。
// 入参：runtime *Runtime 为配置仓库；root *rootOptions 为输出选项。
// 返回值：*cobra.Command，可输出一个不含密钥的 Profile。
func newConfigShowCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show [name]",
		Short: "显示一个 Profile",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			var profile config.Profile
			var err error
			if len(args) == 1 {
				profile, err = runtime.Profiles.Get(args[0])
			} else {
				profile, err = selectedProfile(runtime, root)
			}
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, profile)
		},
	}
}
