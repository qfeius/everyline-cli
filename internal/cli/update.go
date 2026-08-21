package cli

import (
	"context"
	"os"

	"git.qtech.cn/ai/everyline-cli/internal/build"
	selfupdate "git.qtech.cn/ai/everyline-cli/internal/update"

	"github.com/spf13/cobra"
)

// newUpdateCommand 创建独立二进制自更新命令。
// 入参：runtime *Runtime 为输出和网络依赖；root *rootOptions 为超时和输出 flags。
// 返回值：*cobra.Command，支持显式 manifest、dry-run 和 SHA-256 校验更新。
func newUpdateCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var manifestURL string
	var dryRun bool
	command := &cobra.Command{
		Use:   "update",
		Short: "检查并更新独立二进制",
		Long: `从显式指定的 HTTPS manifest 检查当前平台制品。

仅支持独立二进制安装；通过 npm/npx 薄包装启动时请更新 npm 包。制品下载后会校验 SHA-256，再替换当前二进制。`,
		Example: `  everyline-cli update --manifest-url https://example.com/everyline-cli/manifest.json
  everyline-cli update --manifest-url https://example.com/everyline-cli/manifest.json --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), root.Timeout)
			defer cancel()
			result, err := selfupdate.Run(ctx, build.Current().Version, manifestURL, selfupdate.Options{
				HTTPClient: runtime.HTTP,
				DryRun:     dryRun,
				Wrapper:    os.Getenv("EVERYLINE_CLI_WRAPPER") == "1",
			})
			if err != nil {
				return err
			}
			return render(runtime, root, "table", result)
		},
	}
	withNotes(command,
		"manifest 和制品地址必须是 HTTPS URL，并为当前平台提供 SHA-256。",
		"--dry-run 只校验 manifest、平台和版本，不下载或替换文件。",
		"npm/npx 薄包装不会修改包内二进制，请通过 npm 更新包。",
	)
	command.Flags().StringVar(&manifestURL, "manifest-url", "", "更新 manifest 的 HTTPS 地址")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "只检查更新，不下载或替换文件")
	_ = command.MarkFlagRequired("manifest-url")
	return command
}
