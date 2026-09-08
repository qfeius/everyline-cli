package cli

import (
	"errors"
	"fmt"
	"slices"

	"git.qtech.cn/ai/everyline-cli/internal/config"

	"github.com/spf13/cobra"
)

// newConfigCommand 创建 Profile 本地管理命令组。
// 入参：runtime *Runtime 为配置和输出依赖；root *rootOptions 为公共输出 flags。
// 返回值：*cobra.Command，包含 add/list/use/show。
func newConfigCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "管理多环境 Profile",
		Long: `管理多环境 Profile。

		Profile 保存环境地址、可选的 app ID、默认身份和默认输出格式，不保存 app secret 或 access token。`,
	}
	withNotes(command,
		"Profile 保存环境地址、可选的 app ID、默认身份和默认输出格式。",
		"Profile 不保存 app secret 或 access token。",
		"config use 会修改当前默认 Profile。",
	)
	command.AddCommand(
		newConfigAddCommand(runtime, root),
		newConfigUseCommand(runtime, root),
		newConfigListCommand(runtime, root),
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
	var oauthMetadataURL string
	var oauthBusinessType string
	var oauthClientID string
	var oauthDeviceClientID string
	var oauthRedirectURL string
	var oauthScopes []string
	var oauthDeviceAuthorizationURL string
	var oauthRevocationURL string
	var oauthResource string
	var userBaseURL string
	var defaultIdentity string
	var defaultOutput string
	command := &cobra.Command{
		Use:   "add <name>",
		Short: "新增或更新 Profile",
		Long:  "新增或更新 Profile。使用 --env 创建预设环境配置，或同时提供 --base-url 和 --token-url；默认身份为 app 时必须提供 app-id，user 身份可省略。",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			// 未指定环境或自定义地址时统一使用生产环境。
			if environment == "" && baseURL == "" && tokenURL == "" {
				environment = "prod"
			}
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
				if !command.Flags().Changed("oauth-metadata-url") {
					oauthMetadataURL = preset.OAuthMetadataURL
				}
				if !command.Flags().Changed("oauth-business-type") {
					oauthBusinessType = preset.OAuthBusinessType
				}
				if !command.Flags().Changed("oauth-device-client-id") {
					oauthDeviceClientID = preset.OAuthDeviceClientID
				}
				if !command.Flags().Changed("oauth-redirect-url") {
					oauthRedirectURL = preset.OAuthRedirectURL
				}
				if !command.Flags().Changed("oauth-scope") {
					oauthScopes = slices.Clone(preset.OAuthScopes)
				}
			}
			if baseURL == "" || tokenURL == "" {
				return fmt.Errorf("必须指定 --env，或同时指定 --base-url 和 --token-url")
			}
			profile := config.Profile{
				Name:                        args[0],
				BaseURL:                     baseURL,
				UserBaseURL:                 userBaseURL,
				AuthURL:                     authURL,
				TokenURL:                    tokenURL,
				AppID:                       appID,
				OAuthMetadataURL:            oauthMetadataURL,
				OAuthBusinessType:           oauthBusinessType,
				OAuthClientID:               oauthClientID,
				OAuthDeviceClientID:         oauthDeviceClientID,
				OAuthRedirectURL:            oauthRedirectURL,
				OAuthScopes:                 oauthScopes,
				OAuthDeviceAuthorizationURL: oauthDeviceAuthorizationURL,
				OAuthRevocationURL:          oauthRevocationURL,
				OAuthResource:               oauthResource,
				DefaultIdentity:             config.IdentityApp,
				DefaultOutput:               defaultOutput,
			}
			if existing, existingErr := runtime.Profiles.Get(profile.Name); existingErr == nil {
				profile.DefaultIdentity = existing.DefaultIdentity
				if !command.Flags().Changed("default-output") {
					profile.DefaultOutput = existing.DefaultOutput
				}
				if profile.UserBaseURL == "" {
					profile.UserBaseURL = existing.UserBaseURL
				}
				if profile.AuthURL == "" {
					profile.AuthURL = existing.AuthURL
				}
				if profile.OAuthMetadataURL == "" {
					profile.OAuthMetadataURL = existing.OAuthMetadataURL
				}
				if profile.OAuthBusinessType == "" {
					profile.OAuthBusinessType = existing.OAuthBusinessType
				}
				if profile.OAuthClientID == "" {
					profile.OAuthClientID = existing.OAuthClientID
				}
				if profile.OAuthDeviceClientID == "" {
					profile.OAuthDeviceClientID = existing.OAuthDeviceClientID
				}
				if profile.OAuthRedirectURL == "" {
					profile.OAuthRedirectURL = existing.OAuthRedirectURL
				}
				if len(profile.OAuthScopes) == 0 {
					profile.OAuthScopes = slices.Clone(existing.OAuthScopes)
				}
				if profile.OAuthDeviceAuthorizationURL == "" {
					profile.OAuthDeviceAuthorizationURL = existing.OAuthDeviceAuthorizationURL
				}
				if profile.OAuthRevocationURL == "" {
					profile.OAuthRevocationURL = existing.OAuthRevocationURL
				}
				if profile.OAuthResource == "" {
					profile.OAuthResource = existing.OAuthResource
				}
			} else if !errors.Is(existingErr, config.ErrProfileNotFound) {
				return existingErr
			}
			if profile.DefaultOutput == "" {
				profile.DefaultOutput = "json"
			}
			if defaultIdentity != "" {
				identity, err := config.ParseIdentityKind(defaultIdentity)
				if err != nil {
					return err
				}
				profile.DefaultIdentity = identity
			}
			if err := profile.ValidateForIdentity(profile.DefaultIdentity); err != nil {
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
			return render(runtime, root, profile.DefaultOutput, profile)
		},
	}
	withNotes(command, "Profile 不保存 app secret 或 access token。")
	command.Flags().StringVar(&environment, "env", "", "使用预设环境：prod（省略环境和自定义地址时默认 prod）")
	command.Flags().StringVar(&baseURL, "base-url", "", "EveryLine 服务基础 URL")
	command.Flags().StringVar(&userBaseURL, "user-base-url", "", "用户身份业务基础 URL；为空时复用 base-url")
	command.Flags().StringVar(&authURL, "auth-url", "", "EveryLine 用户认证页面基础 URL")
	command.Flags().StringVar(&tokenURL, "token-url", "", "tenant token 完整 URL")
	command.Flags().StringVar(&appID, "app-id", "", "EveryLine app ID；app 身份必需，user 身份可省略")
	command.Flags().StringVar(&oauthMetadataURL, "oauth-metadata-url", "", "用户 OAuth authorization server metadata URL")
	command.Flags().StringVar(&oauthBusinessType, "oauth-business-type", "", "用户 OAuth business type")
	command.Flags().StringVar(&oauthClientID, "oauth-client-id", "", "可选用户 OAuth 浏览器 public client ID；Codex 显式登录时动态注册并替换")
	command.Flags().StringVar(&oauthDeviceClientID, "oauth-device-client-id", "", "独立 OAuth Device Grant client ID；prod 由预设提供，其他环境使用时显式配置")
	command.Flags().StringVar(&oauthRedirectURL, "oauth-redirect-url", "", "用户 OAuth loopback 回调 URL")
	command.Flags().StringSliceVar(&oauthScopes, "oauth-scope", nil, "用户 OAuth scope，可重复传入")
	command.Flags().StringVar(&oauthDeviceAuthorizationURL, "oauth-device-authorization-url", "", "可选 Device Authorization endpoint；默认从 metadata 发现")
	command.Flags().StringVar(&oauthRevocationURL, "oauth-revocation-url", "", "可选 OAuth token revocation endpoint；默认从 metadata 发现")
	command.Flags().StringVar(&oauthResource, "oauth-resource", "", "可选 OAuth resource；默认使用 user 业务基础 URL")
	command.Flags().StringVar(&defaultIdentity, "default-identity", "", "默认业务身份：app|user")
	command.Flags().StringVar(&defaultOutput, "default-output", "", "默认输出格式，省略时为 json")
	return command
}

// profileCredentialIdentityChanged 判断覆盖 Profile 是否改变了 token 的适用身份。
// 入参：before/after config.Profile 分别为现有与新配置。
// 返回值：bool，app/user 业务基址、token 地址或 app ID 任一变化时为 true。
func profileCredentialIdentityChanged(before config.Profile, after config.Profile) bool {
	return before.BaseURL != after.BaseURL || before.UserBaseURL != after.UserBaseURL || before.TokenURL != after.TokenURL || before.AppID != after.AppID ||
		before.OAuthMetadataURL != after.OAuthMetadataURL || before.OAuthBusinessType != after.OAuthBusinessType || before.OAuthClientID != after.OAuthClientID ||
		before.OAuthDeviceClientID != after.OAuthDeviceClientID || before.OAuthRedirectURL != after.OAuthRedirectURL || !slices.Equal(before.OAuthScopes, after.OAuthScopes) ||
		before.OAuthDeviceAuthorizationURL != after.OAuthDeviceAuthorizationURL || before.OAuthRevocationURL != after.OAuthRevocationURL || before.OAuthResource != after.OAuthResource
}

// deleteProfileTokens 清理 Profile 下 app/user 两套 token，防止连接配置更新后残留旧身份凭证。
// 入参：runtime *Runtime 为 token 存储依赖；profileName string 为 Profile 名称。
// 返回值：error，任一身份清理失败时非 nil。
func deleteProfileTokens(runtime *Runtime, profileName string) error {
	if runtime.DeviceCredentials != nil {
		if err := runtime.DeviceCredentials.Delete(profileName); err != nil {
			return err
		}
	}
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
		Long:  "列出全部 Profile，并标记当前 Profile；不会输出密钥或 token。",
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

// identityName 返回 Profile 的可读身份，空值按兼容默认 app 处理。
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
		Short: "切换当前默认 Profile",
		Long:  "切换当前默认 Profile；仅影响后续未显式指定 --profile 的命令。",
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
		Long:  "显示指定或当前 Profile 的配置；不会输出 app secret 或 access token。",
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
