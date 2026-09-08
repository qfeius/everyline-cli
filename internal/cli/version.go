package cli

import (
	"context"
	"encoding/json"
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
	build.Info            `yaml:",inline"`
	LatestVersion         string `json:"latestVersion" yaml:"latestVersion"`
	IsLatest              *bool  `json:"isLatest" yaml:"isLatest"`
	UpdateRequired        bool   `json:"updateRequired" yaml:"updateRequired"`
	UpdateCommand         string `json:"updateCommand" yaml:"updateCommand"`
	CheckError            string `json:"checkError,omitempty" yaml:"checkError,omitempty"`
	FirstInstall          bool   `json:"firstInstall" yaml:"firstInstall"`
	AuthorizationRequired bool   `json:"authorizationRequired" yaml:"authorizationRequired"`
	NextAction            string `json:"nextAction,omitempty" yaml:"nextAction,omitempty"`
}

// deferredUpdateNotice 是写入 stderr 的机器可读延迟更新状态。
type deferredUpdateNotice struct {
	Code           string `json:"code"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	UpdateCommand  string `json:"updateCommand"`
	UpdateAfter    string `json:"updateAfter"`
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
			explicitManifestURL := strings.TrimSpace(manifestURL)
			resolvedManifestURL := resolvedUpdateManifestURL(explicitManifestURL)
			result := inspectVersion(command.Context(), runtime, resolvedManifestURL)
			// 显式覆盖必须保留在建议命令中，避免检查 B 源后实际从环境中的 A 源更新。
			if explicitManifestURL != "" {
				result.UpdateCommand = versionUpdateCommand(resolvedManifestURL, true)
			}
			return render(runtime, root, "json", result)
		},
	}
	command.Flags().StringVar(&manifestURL, "manifest-url", "", "覆盖更新 manifest 的 HTTPS 地址")
	withNotes(command,
		"检查失败不会使 version 命令失败，isLatest 返回 null，避免误报已是最新版。",
		"npm 安装版从 npm latest 检查更新，无需 manifest；独立二进制更新地址按 --manifest-url、EVERYLINE_CLI_UPDATE_MANIFEST_URL、构建内置值的顺序选择。",
	)
	return command
}

// inspectVersion 获取版本检查结果；网络或配置失败会写入结构化结果而不改变命令退出状态。
// 入参：ctx context.Context 控制请求；runtime *Runtime 提供 HTTP；manifestURL string 为已解析更新地址。
// 返回值：versionOutput，检查未知时 IsLatest 为 nil。
func inspectVersion(ctx context.Context, runtime *Runtime, manifestURL string) versionOutput {
	result := versionOutput{Info: build.Current()}
	if state, required, err := pendingFirstInstallAuthorization(runtime); err == nil {
		result.FirstInstall = state.FirstInstall
		result.AuthorizationRequired = required
		if required {
			result.NextAction = "authorize"
		}
	}
	if manifestURL == "" && os.Getenv("EVERYLINE_CLI_WRAPPER") != "1" {
		result.CheckError = "未配置更新 manifest URL"
		return result
	}
	checkContext, cancel := context.WithTimeout(ctx, versionCheckTimeout)
	defer cancel()
	var checked selfupdate.CheckResult
	var err error
	// npm 安装版始终查询 npm，避免 manifest 与 npm 实际可安装版本不一致。
	if os.Getenv("EVERYLINE_CLI_WRAPPER") == "1" {
		checked, err = selfupdate.CheckNPM(checkContext, result.Version, runtime.HTTP)
	} else {
		checked, err = selfupdate.Check(checkContext, result.Version, manifestURL, runtime.HTTP)
	}
	if err != nil {
		result.CheckError = err.Error()
		result.UpdateCommand = versionUpdateCommand(manifestURL, false)
		return result
	}
	result.LatestVersion = checked.LatestVersion
	result.IsLatest = &checked.IsLatest
	result.UpdateRequired = !checked.IsLatest
	result.UpdateCommand = versionUpdateCommand(manifestURL, false)
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

/*
versionUpdateCommand 根据安装方式返回用户可直接执行的更新命令。
入参：manifestURL string 为独立二进制更新源；explicit bool 表示该地址来自本次命令显式覆盖。
返回值：string，npm 包使用 npm 命令，独立二进制使用 update 命令。
*/
func versionUpdateCommand(manifestURL string, explicit bool) string {
	if os.Getenv("EVERYLINE_CLI_WRAPPER") == "1" {
		// npm 包同时携带 CLI 与 Skill，显式允许 everyline-cli 的 postinstall 才能重新登记两个宿主。
		return "npm install -g --foreground-scripts --allow-scripts=@qfeius/everyline-cli @qfeius/everyline-cli@latest --registry https://registry.npmjs.org"
	}
	if explicit && strings.TrimSpace(manifestURL) != "" {
		return "everyline-cli update --manifest-url " + strings.TrimSpace(manifestURL)
	}
	if strings.TrimSpace(build.UpdateManifestURL) != "" || strings.TrimSpace(os.Getenv("EVERYLINE_CLI_UPDATE_MANIFEST_URL")) != "" {
		return "everyline-cli update"
	}
	if strings.TrimSpace(manifestURL) == "" {
		return ""
	}
	return "everyline-cli update --manifest-url " + strings.TrimSpace(manifestURL)
}

// maybeDeferRequiredUpdate 在业务命令前检测新版，并把更新延迟到当前完整业务流程结束。
// 入参：ctx context.Context 控制取消；runtime/root 为运行和输出依赖；command 为即将执行的叶子命令。
// 返回值：无；存在新版时只输出 UPDATE_PENDING，不阻断当前业务命令。
func maybeDeferRequiredUpdate(ctx context.Context, runtime *Runtime, root *rootOptions, command *cobra.Command) {
	if !isBusinessCommand(command) {
		return
	}
	// dry-run/print-input 承诺只执行本地校验，因此连更新 manifest 也不能访问。
	for _, flagName := range []string{"dry-run", "print-input"} {
		if command.Flags().Lookup(flagName) == nil {
			continue
		}
		enabled, err := command.Flags().GetBool(flagName)
		if err == nil && enabled {
			return
		}
	}
	manifestURL := resolvedUpdateManifestURL("")
	if manifestURL == "" && os.Getenv("EVERYLINE_CLI_WRAPPER") != "1" {
		return
	}
	result := inspectVersion(ctx, runtime, manifestURL)
	if result.IsLatest == nil {
		if root.Verbose {
			_, _ = fmt.Fprintf(runtime.Error, "[version] 检查更新失败：%s\n", result.CheckError)
		}
		return
	}
	if *result.IsLatest {
		return
	}
	// CLI 只知道当前单条命令，跨上传、发起和结果轮询的完整流程由 Skill 统一在终态后执行更新。
	notice := deferredUpdateNotice{
		Code:           "UPDATE_PENDING",
		CurrentVersion: result.Version,
		LatestVersion:  result.LatestVersion,
		UpdateCommand:  result.UpdateCommand,
		UpdateAfter:    "current_business_workflow",
	}
	payload, _ := json.Marshal(notice)
	_, _ = fmt.Fprintln(runtime.Error, string(payload))
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
