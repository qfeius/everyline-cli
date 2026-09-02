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
	Profile  string
	Identity string
	Output   string
	Raw      bool
	Timeout  time.Duration
	Verbose  bool
	NoColor  bool
}

// NewRootCommand 创建完整 Cobra 命令树，并注入运行时依赖。
// 入参：runtime *Runtime 为存储、HTTP、时钟和 I/O 依赖。
// 返回值：*cobra.Command，可被应用层执行或测试。
func NewRootCommand(runtime *Runtime) *cobra.Command {
	// 帮助命令按业务流程展示，而不是按名称排序；各命令组显式维护自己的顺序。
	cobra.EnableCommandSorting = false
	options := &rootOptions{}
	command := &cobra.Command{
		Use:   "everyline-cli",
		Short: "EveryLine 命令行工具",
		Long: `EveryLine 命令行工具。

用于合同文件准备、智能审查任务执行、审查清单管理和审查规则管理。

完整审查流程推荐使用 review run；需要分步控制时，使用 review file、review subject 和 review task。`,
		Example: `  everyline-cli review run --input review-run.json --output json
  everyline-cli review task result --task-id 123 --output json`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.SetIn(runtime.Input)
	command.SetOut(runtime.Output)
	command.SetErr(runtime.Error)
	command.SetHelpTemplate(everylineHelpTemplate)
	command.PersistentFlags().StringVar(&options.Profile, "profile", "", "覆盖当前 Profile，仅对本次命令生效")
	command.PersistentFlags().StringVar(&options.Identity, "as", "", "覆盖 Profile 默认身份，仅支持 app|user")
	command.PersistentFlags().StringVarP(&options.Output, "output", "o", "", "输出格式：json|yaml|table|raw")
	command.PersistentFlags().BoolVar(&options.Raw, "raw", false, "输出紧凑 JSON，等价于 --output raw")
	command.PersistentFlags().DurationVar(&options.Timeout, "timeout", 30*time.Second, "普通远端请求超时")
	command.PersistentFlags().BoolVar(&options.Verbose, "verbose", false, "将工作流进度写入 stderr，不污染 stdout")
	command.PersistentFlags().BoolVar(&options.NoColor, "no-color", false, "禁用彩色输出")
	command.PersistentPreRun = func(command *cobra.Command, args []string) {
		maybeWarnNewVersion(command.Context(), runtime, options, command)
	}
	command.AddGroup(
		&cobra.Group{ID: "business", Title: "Review"},
		&cobra.Group{ID: "cli", Title: "CLI Management"},
	)
	command.SetHelpCommandGroupID("cli")
	command.SetCompletionCommandGroupID("cli")
	withNotes(command,
		"使用 everyline-cli <command> --help 查看具体命令参数。",
		"推荐使用 review run 执行上传、发起、等待和获取结果的一键流程。",
		"已有 task-id 时使用 review task result 获取最终审查结果。",
		"--verbose 的工作流进度输出到 stderr，不污染结构化 stdout。",
	)
	reviewCommand := newReviewCommand(runtime, options)
	reviewCommand.GroupID = "business"
	checklistCommand := newChecklistCommand(runtime, options)
	checklistCommand.GroupID = "business"
	ruleCommand := newRuleCommand(runtime, options)
	ruleCommand.GroupID = "business"
	configCommand := newConfigCommand(runtime, options)
	configCommand.GroupID = "cli"
	authCommand := newAuthCommand(runtime, options)
	authCommand.GroupID = "cli"
	command.AddCommand(
		reviewCommand,
		checklistCommand,
		ruleCommand,
		configCommand,
		authCommand,
	)
	command.InitDefaultCompletionCmd()
	versionCommand := newVersionCommand(runtime, options)
	versionCommand.GroupID = "cli"
	updateCommand := newUpdateCommand(runtime, options)
	updateCommand.GroupID = "cli"
	command.AddCommand(versionCommand, updateCommand)
	return command
}

// selectedIdentity 按显式 --as、Profile 默认身份和兼容默认值解析业务身份。
// 入参：profile config.Profile 为当前 Profile；requested string 为 --as 参数。
// 返回值：config.IdentityKind 为最终身份；error 为未知身份。
func selectedIdentity(profile config.Profile, requested string) (config.IdentityKind, error) {
	if requested != "" {
		return config.ParseIdentityKind(requested)
	}
	if profile.DefaultIdentity != "" {
		return config.ParseIdentityKind(string(profile.DefaultIdentity))
	}
	return config.IdentityApp, nil
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
		formatName = "json"
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
