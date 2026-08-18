package cli

import (
	"git.qtech.cn/ai/everyline-cli/internal/build"

	"github.com/spf13/cobra"
)

// newVersionCommand 创建稳定的版本输出命令。
// 入参：runtime *Runtime 为输出依赖；root *rootOptions 为输出 flags。
// 返回值：*cobra.Command，可输出 ldflags 注入的构建信息。
func newVersionCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "显示版本信息",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return render(runtime, root, "table", build.Current())
		},
	}
}
