package cli

import (
	"context"
	"fmt"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/output"

	"github.com/spf13/cobra"
)

// rootOptions 保存所有子命令共享的稳定 flags。
type rootOptions struct {
	Profile string
	Output  string
	Raw     bool
	Timeout time.Duration
	Verbose bool
	NoColor bool
}

// NewRootCommand 创建完整 Cobra 命令树，并注入运行时依赖。
// 入参：runtime *Runtime 为存储、HTTP、时钟和 I/O 依赖。
// 返回值：*cobra.Command，可被应用层执行或测试。
func NewRootCommand(runtime *Runtime) *cobra.Command {
	options := &rootOptions{}
	command := &cobra.Command{
		Use:           "everyline-cli",
		Short:         "智审开放平台命令行客户端",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.SetIn(runtime.Input)
	command.SetOut(runtime.Output)
	command.SetErr(runtime.Error)
	command.PersistentFlags().StringVar(&options.Profile, "profile", "", "使用指定 Profile")
	command.PersistentFlags().StringVarP(&options.Output, "output", "o", "", "输出格式：json|yaml|table")
	command.PersistentFlags().BoolVar(&options.Raw, "raw", false, "输出紧凑原始 JSON")
	command.PersistentFlags().DurationVar(&options.Timeout, "timeout", 30*time.Second, "普通远端请求超时")
	command.PersistentFlags().BoolVar(&options.Verbose, "verbose", false, "将工作流进度写入 stderr")
	command.PersistentFlags().BoolVar(&options.NoColor, "no-color", false, "禁用彩色输出")
	command.AddCommand(
		newConfigCommand(runtime, options),
		newAuthCommand(runtime, options),
		newReviewCommand(runtime, options),
		newChecklistCommand(runtime, options),
		newRuleCommand(runtime, options),
		newVersionCommand(runtime, options),
	)
	command.InitDefaultCompletionCmd()
	return command
}

// Execute 运行命令并返回错误，便于 main 统一输出和映射退出码。
// 入参：ctx context.Context 控制取消；runtime *Runtime 为依赖；args []string 为不含程序名的参数。
// 返回值：error，命令成功时为 nil。
func Execute(ctx context.Context, runtime *Runtime, args []string) error {
	command := NewRootCommand(runtime)
	command.SetArgs(args)
	return command.ExecuteContext(ctx)
}

// selectedProfile 按 --profile 或默认选择读取 Profile。
// 入参：runtime *Runtime 为配置仓库；options *rootOptions 保存用户覆盖。
// 返回值：config.Profile 为环境配置；error 为未配置或不存在。
func selectedProfile(runtime *Runtime, options *rootOptions) (config.Profile, error) {
	var profile config.Profile
	var err error
	if options.Profile != "" {
		profile, err = runtime.Profiles.Get(options.Profile)
	} else {
		profile, err = runtime.Profiles.Current()
	}
	if err != nil {
		return config.Profile{}, err
	}
	// 重新校验磁盘内容，防止手工修改配置绕过 HTTPS 等写入期约束。
	if err := profile.Validate(); err != nil {
		return config.Profile{}, err
	}
	return profile, nil
}

// render 根据 --raw、--output、Profile 默认值的优先级渲染业务结果。
// 入参：runtime *Runtime 为输出依赖；options *rootOptions 为 flags；defaultOutput string 为当前 Profile 默认值；value any 为业务结果。
// 返回值：error，格式或渲染失败时非 nil。
func render(runtime *Runtime, options *rootOptions, defaultOutput string, value any) error {
	formatName := options.Output
	if formatName == "" {
		formatName = defaultOutput
	}
	if formatName == "" {
		formatName = "table"
	}
	if options.Raw {
		formatName = "raw"
	}
	format, err := output.ParseFormat(formatName)
	if err != nil {
		return err
	}
	return runtime.Renderer.Render(runtime.Output, format, value)
}

// progress 在 verbose 模式下只向 stderr 写进度，不污染 stdout 业务协议。
// 入参：runtime *Runtime 为 stderr；options *rootOptions 为 flags；message string 为进度文本。
// 返回值：无。
func progress(runtime *Runtime, options *rootOptions, message string) {
	if options.Verbose {
		_, _ = fmt.Fprintln(runtime.Error, message)
	}
}
