package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/build"
	selfupdate "git.qtech.cn/ai/everyline-cli/internal/update"

	"github.com/spf13/cobra"
)

const versionCheckTimeout = 2 * time.Second

// versionOutput 在构建信息之外稳定返回最新版本判断和可执行更新命令。
type versionOutput struct {
	build.Info    `yaml:",inline"`
	LatestVersion string `json:"latestVersion" yaml:"latestVersion"`
	IsLatest      *bool  `json:"isLatest" yaml:"isLatest"`
	UpdateCommand string `json:"updateCommand" yaml:"updateCommand"`
	CheckError    string `json:"checkError,omitempty" yaml:"checkError,omitempty"`
}

// newVersionCommand 创建带非阻断在线检查的版本输出命令。
// 入参：runtime *Runtime 为输出和网络依赖；root *rootOptions 为输出 flags。
// 返回值：*cobra.Command，可输出当前版本、最新版本判断和更新命令。
func newVersionCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var manifestURL string
	command := &cobra.Command{
		Use:   "version",
		Short: "显示版本并检查更新",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			result := inspectVersion(command.Context(), runtime, resolvedUpdateManifestURL(manifestURL))
			return render(runtime, root, "json", result)
		},
	}
	command.Flags().StringVar(&manifestURL, "manifest-url", "", "覆盖更新 manifest 的 HTTPS 地址")
	withNotes(command,
		"检查失败不会使 version 命令失败，isLatest 返回 null，避免误报已是最新版。",
		"更新地址按 --manifest-url、EVERYLINE_CLI_UPDATE_MANIFEST_URL、构建内置值的顺序选择。",
	)
	return command
}

// inspectVersion 获取版本检查结果；网络或配置失败会写入结构化结果而不改变命令退出状态。
// 入参：ctx context.Context 控制请求；runtime *Runtime 提供 HTTP；manifestURL string 为已解析更新地址。
// 返回值：versionOutput，检查未知时 IsLatest 为 nil。
func inspectVersion(ctx context.Context, runtime *Runtime, manifestURL string) versionOutput {
	result := versionOutput{Info: build.Current()}
	if manifestURL == "" {
		result.CheckError = "未配置更新 manifest URL"
		return result
	}
	checkContext, cancel := context.WithTimeout(ctx, versionCheckTimeout)
	defer cancel()
	checked, err := selfupdate.Check(checkContext, result.Version, manifestURL, runtime.HTTP)
	if err != nil {
		result.CheckError = err.Error()
		result.UpdateCommand = versionUpdateCommand(manifestURL)
		return result
	}
	result.LatestVersion = checked.LatestVersion
	result.IsLatest = &checked.IsLatest
	result.UpdateCommand = versionUpdateCommand(manifestURL)
	return result
}

// resolvedUpdateManifestURL 按命令参数、环境变量和发布构建值选择更新源。
// 入参：explicit string 为命令显式覆盖值。
// 返回值：string，为去除首尾空白后的 manifest 地址；未配置时为空。
func resolvedUpdateManifestURL(explicit string) string {
	for _, candidate := range []string{explicit, os.Getenv("EVERYLINE_CLI_UPDATE_MANIFEST_URL"), build.UpdateManifestURL} {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	return ""
}

// versionUpdateCommand 根据安装方式返回用户可直接执行的更新命令。
// 入参：manifestURL string 为独立二进制更新源。
// 返回值：string，npm 包使用 npm 命令，独立二进制使用 update 命令。
func versionUpdateCommand(manifestURL string) string {
	if os.Getenv("EVERYLINE_CLI_WRAPPER") == "1" {
		return "npm install -g everyline-cli@latest"
	}
	if strings.TrimSpace(build.UpdateManifestURL) != "" || strings.TrimSpace(os.Getenv("EVERYLINE_CLI_UPDATE_MANIFEST_URL")) != "" {
		return "everyline-cli update"
	}
	if strings.TrimSpace(manifestURL) == "" {
		return ""
	}
	return "everyline-cli update --manifest-url " + strings.TrimSpace(manifestURL)
}

// maybeWarnNewVersion 在业务命令前做短时非阻断检查，仅发现新版本时写入 stderr。
// 入参：ctx context.Context 控制取消；runtime/root 为运行和输出依赖；command 为即将执行的叶子命令。
// 返回值：无，检查失败只在 verbose 模式提示且不阻断业务。
func maybeWarnNewVersion(ctx context.Context, runtime *Runtime, root *rootOptions, command *cobra.Command) {
	if !isBusinessCommand(command) {
		return
	}
	manifestURL := resolvedUpdateManifestURL("")
	if manifestURL == "" {
		return
	}
	result := inspectVersion(ctx, runtime, manifestURL)
	if result.IsLatest == nil {
		if root.Verbose {
			_, _ = fmt.Fprintf(runtime.Error, "[version] 检查更新失败：%s\n", result.CheckError)
		}
		return
	}
	if !*result.IsLatest {
		_, _ = fmt.Fprintf(runtime.Error, "[version] 发现新版本 %s（当前 %s），更新命令：%s\n", result.LatestVersion, result.Version, result.UpdateCommand)
	}
}

// isBusinessCommand 判断叶子命令是否属于 review、checklist 或 rule 业务组。
// 入参：command *cobra.Command 为即将执行的命令。
// 返回值：bool，属于三个业务顶级命令之一时为 true。
func isBusinessCommand(command *cobra.Command) bool {
	current := command
	for current.Parent() != nil && current.Parent().Parent() != nil {
		current = current.Parent()
	}
	switch current.Name() {
	case "review", "checklist", "rule":
		return true
	default:
		return false
	}
}
