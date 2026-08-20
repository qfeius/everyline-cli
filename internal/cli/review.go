package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
	"git.qtech.cn/ai/everyline-cli/internal/review"

	"github.com/spf13/cobra"
)

// newReviewCommand 创建核心审查命令树。
// 入参：runtime *Runtime 为 HTTP、凭证和输出依赖；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，包含 run/file/subject/task。
func newReviewCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "review",
		Short: "执行合同智能审查",
		Long: `执行合同智能审查。

推荐使用 review run 执行上传、发起、等待的一键审查流程。
需要分步控制时，使用 review file、review subject 和 review task。
审查清单和审查规则管理请使用 checklist 和 rule。`,
		Example: `  everyline-cli review run --input review-run.json --output json
  everyline-cli review task result --task-id 123 --output json`,
	}
	withNotes(command,
		"review run 适合从文件准备开始的一键审查流程。",
		"已有 task-id 时使用 review task result 获取最终结果。",
		"review file、review subject 和 review task 用于需要分步控制的场景。",
	)
	command.AddCommand(
		newReviewRunCommand(runtime, root),
		newReviewFileCommand(runtime, root),
		newReviewSubjectCommand(runtime, root),
		newReviewTaskCommand(runtime, root),
	)
	return command
}

// newReviewFileCommand 创建文件上传和 URL 上传命令组。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，包含 upload/upload-url。
func newReviewFileCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "file",
		Short: "准备审查输入文件",
		Long: `准备审查输入文件。

本组命令只负责上传文件，不会自动发起审查任务。
支持本地文件上传和通过 URL 上传。`,
	}
	withNotes(command, "upload 和 upload-url 只准备审查文件，不会自动发起审查任务。")
	command.AddCommand(
		newReviewFileUploadCommand(runtime, root),
		newReviewFileUploadURLCommand(runtime, root),
	)
	return command
}

// newReviewFileUploadCommand 创建 multipart 本地上传命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持文件校验、dry-run 和实际上传。
func newReviewFileUploadCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var filePath string
	var name string
	var dryRun bool
	var printInput bool
	command := &cobra.Command{
		Use:   "upload",
		Short: "上传本地合同文件",
		Long:  "上传本地合同文件，返回平台文件信息；不会自动发起审查任务。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if err := review.ValidateUploadFile(filePath); err != nil {
				return err
			}
			if err := review.ValidateFileName(name); err != nil {
				return err
			}
			input := map[string]any{"file": filePath, "name": name}
			if dryRun || printInput {
				return render(runtime, root, "json", input)
			}
			service, profile, err := buildReviewService(runtime, root)
			if err != nil {
				return err
			}
			progress(runtime, root, "正在上传合同文件...")
			result, err := service.UploadFile(command.Context(), filePath, name, "", "")
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	command.Flags().StringVar(&filePath, "file", "", "本地合同路径")
	command.Flags().StringVar(&name, "name", "", "业务文件名（含扩展名）")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "只校验并输出请求，不调用远端")
	command.Flags().BoolVar(&printInput, "print-input", false, "输出规范化请求，不调用远端")
	_ = command.MarkFlagRequired("file")
	_ = command.MarkFlagRequired("name")
	return command
}

// newReviewFileUploadURLCommand 创建 JSON URL 上传命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 fileUrl/fileName 精确契约。
func newReviewFileUploadURLCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var fileURL string
	var name string
	var dryRun bool
	var printInput bool
	command := &cobra.Command{
		Use:   "upload-url",
		Short: "通过 URL 上传合同文件",
		Long:  "通过 HTTP/HTTPS URL 上传合同文件，返回平台文件信息；不会自动发起审查任务。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			parsed, err := url.ParseRequestURI(fileURL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("file-url 必须是完整的 http/https URL")
			}
			if err := review.ValidateFileName(name); err != nil {
				return err
			}
			input := map[string]any{"fileUrl": fileURL, "fileName": name}
			if dryRun || printInput {
				return render(runtime, root, "json", input)
			}
			service, profile, err := buildReviewService(runtime, root)
			if err != nil {
				return err
			}
			progress(runtime, root, "正在通过 URL 上传合同文件...")
			result, err := service.UploadURL(command.Context(), fileURL, name)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	command.Flags().StringVar(&fileURL, "file-url", "", "合同文件 http/https URL")
	command.Flags().StringVar(&name, "name", "", "业务文件名（含扩展名）")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "只校验并输出请求，不调用远端")
	command.Flags().BoolVar(&printInput, "print-input", false, "输出规范化请求，不调用远端")
	_ = command.MarkFlagRequired("file-url")
	_ = command.MarkFlagRequired("name")
	return command
}

// newReviewSubjectCommand 创建无副作用主体提取命令组。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，包含 extract。
func newReviewSubjectCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "subject",
		Short: "提取已上传合同的主体信息",
		Long:  "提取已上传合同的主体信息；本组命令不代替完整合同审查流程。",
	}
	command.AddCommand(newReviewSubjectExtractCommand(runtime, root))
	return command
}

// newReviewSubjectExtractCommand 创建主体提取命令，并复用文件身份四元组校验。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 dry-run 和实际调用。
func newReviewSubjectExtractCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	request := review.StartRequest{}
	var dryRun bool
	var printInput bool
	command := &cobra.Command{
		Use:   "extract",
		Short: "无副作用提取合同参与方",
		Long:  "提取合同参与方信息，不执行完整合同审查。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if err := review.ValidateSubjectIdentity(request); err != nil {
				return err
			}
			// dry-run/print-input 必须展示与真实主体提取请求一致的字符串 fileId。
			input := map[string]any{"businessId": request.BusinessID, "fileId": strconv.FormatInt(request.FileID, 10)}
			if request.AppType != "" {
				input["appType"] = request.AppType
			}
			if request.FileHash != "" {
				input["fileHash"] = request.FileHash
			}
			if dryRun || printInput {
				return render(runtime, root, "json", input)
			}
			service, profile, err := buildReviewService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.ExtractSubjects(command.Context(), request)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	command.Flags().StringVar(&request.BusinessID, "business-id", "", "业务对象 ID")
	command.Flags().Int64Var(&request.FileID, "file-id", 0, "平台文件 ID")
	command.Flags().StringVar(&request.FileHash, "file-hash", "", "服务端文件指纹")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "只校验并输出请求，不调用远端")
	command.Flags().BoolVar(&printInput, "print-input", false, "输出规范化请求，不调用远端")
	_ = command.MarkFlagRequired("business-id")
	_ = command.MarkFlagRequired("file-id")
	return command
}

// newReviewTaskCommand 创建普通任务的发起、查询、详情和结果命令组。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，包含 start/status/info/result。
func newReviewTaskCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "task",
		Short: "管理审查任务生命周期",
		Long: `管理审查任务生命周期。

支持发起任务、查询状态、查询详情，以及等待并获取最终结果。`,
	}
	withNotes(command,
		"start 只负责发起任务，不上传文件。",
		"result 会轮询任务状态，成功后自动获取最终审查详情。",
		"status 只查询一次当前状态，不等待任务完成。",
		"info 只查询一次任务详情，不负责轮询。",
		"使用 visibility-scope=contractResult 时，CLI 使用默认接入类型查询。",
	)
	command.AddCommand(
		newReviewTaskStartCommand(runtime, root),
		newReviewTaskResultCommand(runtime, root),
		newReviewTaskStatusCommand(runtime, root),
		newReviewTaskInfoCommand(runtime, root),
	)
	return command
}

// newReviewTaskStartCommand 创建复杂 JSON 输入的普通发起审查命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 --input/--data 和 dry-run。
func newReviewTaskStartCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var inputPath string
	var inline string
	var dryRun bool
	var printInput bool
	command := &cobra.Command{
		Use:   "start",
		Short: "发起智审任务",
		Long: `使用 JSON 请求发起智审任务；本命令不会上传文件。

请求字段（仅支持以下字段）：
- businessId string（必填）：上传接口返回的业务对象 ID。
- fileId integer（必填）：平台文件 ID；保持当前 CLI 输入方式。
- fileHash string（必填，使用上传接口返回值）：文件指纹。
- config object（必填）：审查配置。
- config.selectedPosition string（必填）：审查立场。
- config.selectedAuditRole string（必填）：审查角色。
- config.reviewStrength integer（必填，0|1|2）：审查强度。
- config.selectedCheckListIds array<string>（可选）：指定审查清单 ID，非空时优先级最高。
- config.matchContractTypeRulePackage boolean（可选）：是否匹配合同类型规则包；仅 true 生效。

CLI 仅接受以上字段，其他字段会按未知字段拒绝。`,
		Example: `  everyline-cli review task start --data '{"businessId":"biz-001","fileId":123,"fileHash":"<upload.fileHash>","config":{"selectedPosition":"甲方","selectedAuditRole":"甲方","reviewStrength":1,"selectedCheckListIds":["2001001"],"matchContractTypeRulePackage":true}}' --dry-run
  everyline-cli review task start --input review-start.json --output json`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			var input review.StartInput
			if err := readJSONInput(inputPath, inline, &input); err != nil {
				return err
			}
			request := input.ToRequest().Normalize()
			if err := request.Validate(); err != nil {
				return err
			}
			if dryRun || printInput {
				return render(runtime, root, "json", request.ContractPayload())
			}
			service, profile, err := buildReviewService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.Start(command.Context(), request)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	withNotes(command,
		"需要通过 --input 或 --data 提供 JSON 请求；本命令不会上传文件。",
		"fileHash 应直接填写上传接口返回的文件指纹。",
		"config 及 selectedPosition、selectedAuditRole、reviewStrength 均为必填。",
		"selectedCheckListIds 和 matchContractTypeRulePackage 为可选规则来源参数。",
	)
	addJSONInputFlags(command, &inputPath, &inline)
	command.Flags().BoolVar(&dryRun, "dry-run", false, "只校验并输出请求，不调用远端")
	command.Flags().BoolVar(&printInput, "print-input", false, "输出规范化请求，不调用远端")
	return command
}

// newReviewTaskStatusCommand 创建轻量状态查询命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，可查询 task/status。
func newReviewTaskStatusCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	query := review.TaskQuery{}
	command := &cobra.Command{
		Use:   "status",
		Short: "查询审查任务状态",
		Long:  "查询审查任务当前状态；不会等待任务完成。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			service, profile, err := buildReviewService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.Status(command.Context(), query)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	withNotes(command, "只发起一次状态查询；需要等待并获取详情时使用 review task result。")
	addTaskQueryFlags(command, &query)
	return command
}

// newReviewTaskInfoCommand 创建终态详情查询命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，可查询 task/info。
func newReviewTaskInfoCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	query := review.TaskQuery{}
	command := &cobra.Command{
		Use:   "info",
		Short: "查询审查任务详情",
		Long:  "查询审查任务详情。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			service, profile, err := buildReviewService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.Info(command.Context(), query)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	withNotes(command, "只发起一次详情查询；不会轮询任务状态。")
	addTaskQueryFlags(command, &query)
	return command
}

// newReviewTaskResultCommand 创建 CLI 侧结果工作流，不映射新的远端接口。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 interval/deadline。
func newReviewTaskResultCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	query := review.TaskQuery{}
	var interval time.Duration
	var deadline time.Duration
	command := &cobra.Command{
		Use:   "result",
		Short: "等待审查完成并获取最终结果",
		Long: `等待已有审查任务进入终态。

任务成功后，CLI 会自动获取并输出最终审查结果；这是 CLI 本地编排，不对应新的远端接口。`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			service, profile, err := buildReviewService(runtime, root)
			if err != nil {
				return err
			}
			workflow := review.NewWorkflow(service, review.RealClock{}, review.WorkflowOptions{
				Interval: interval,
				Deadline: deadline,
				OnStatus: func(snapshot review.Document) {
					status, ok := review.StringValue(snapshot, "status")
					if !ok {
						status = "unknown"
					}
					_, _ = fmt.Fprintf(runtime.Error, "[review task result] status=%s\n", status)
				},
			})
			_, _ = fmt.Fprintln(runtime.Error, "[review task result] waiting for task status...")
			progress(runtime, root, "正在等待审查任务完成并获取最终结果...")
			result, err := workflow.WaitForResult(command.Context(), query)
			if err != nil {
				if renderErr := render(runtime, root, profile.DefaultOutput, result); renderErr != nil {
					return fmt.Errorf("%w；输出阶段结果失败: %v", err, renderErr)
				}
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	withNotes(command,
		"内部轮询 status，任务成功后调用一次 info。",
		"超时、取消或详情获取失败时返回最后可用的任务快照和错误。",
	)
	addTaskQueryFlags(command, &query)
	command.Flags().DurationVar(&interval, "interval", 2*time.Second, "轮询间隔")
	command.Flags().DurationVar(&deadline, "deadline", 10*time.Minute, "最长等待时间")
	return command
}

// newReviewRunCommand 创建一键编排命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，按稳定 RunSpec 执行完整闭环。
func newReviewRunCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var inputPath string
	var inline string
	var interval time.Duration
	var deadline time.Duration
	var dryRun bool
	var printInput bool
	command := &cobra.Command{
		Use:   "run",
		Short: "上传、发起并可选等待获取最终详情",
		Long: `一键执行完整合同审查流程。

根据 JSON 输入上传本地文件或 URL 文件，发起审查任务，
并根据 wait 配置决定是否等待任务进入终态。

请求字段：
- source object（必填）：type=file 时提供 path/name；type=url 时提供 fileUrl/name。
- config object（必填）：审查配置。
  - config.selectedPosition string（必填）：审查立场。
  - config.selectedAuditRole string（必填）：审查角色。
  - config.reviewStrength integer（必填，0、1、2）：审查强度。
  - config.selectedCheckListIds array<string>（可选）：指定审查清单 ID，非空时优先级最高。
  - config.matchContractTypeRulePackage boolean（可选）：是否匹配合同类型规则包；仅 true 生效。
- businessId string：URL 来源必填；文件来源优先使用上传接口返回值。
- fileHash string：URL 来源必填；fileHash 使用上传接口返回值。
- extractSubjects boolean：可选，是否先提取合同参与方。
- wait boolean：可选，是否等待任务完成并获取最终详情。`,
		Example: `  everyline-cli review run --input review-run.json --output json
  everyline-cli review run --data '{"source":{"type":"file","path":"./contract.pdf","name":"合同.pdf"},"config":{"selectedPosition":"甲方","selectedAuditRole":"甲方","reviewStrength":1,"selectedCheckListIds":["2001001"],"matchContractTypeRulePackage":true},"wait":true}' --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			var spec review.RunSpec
			if err := readJSONInput(inputPath, inline, &spec); err != nil {
				return err
			}
			spec = spec.Normalize()
			if err := spec.Validate(); err != nil {
				return err
			}
			if spec.Source.Type == "file" {
				if err := review.ValidateUploadFile(spec.Source.Path); err != nil {
					return err
				}
			} else {
				parsed, err := url.ParseRequestURI(spec.Source.FileURL)
				if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					return fmt.Errorf("source.fileUrl 必须是完整的 http/https URL")
				}
			}
			if err := review.ValidateFileName(spec.Source.Name); err != nil {
				return err
			}
			if dryRun || printInput {
				return render(runtime, root, "json", spec)
			}
			service, profile, err := buildReviewService(runtime, root)
			if err != nil {
				return err
			}
			workflow := review.NewWorkflow(service, review.RealClock{}, review.WorkflowOptions{Interval: interval, Deadline: deadline})
			progress(runtime, root, "正在执行一键合同审查工作流...")
			result, err := workflow.Run(command.Context(), spec)
			if err != nil {
				if renderErr := render(runtime, root, profile.DefaultOutput, result); renderErr != nil {
					return fmt.Errorf("%w；输出阶段结果失败: %v", err, renderErr)
				}
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	withNotes(command,
		"输入中的 wait=true 才会等待任务终态并返回最终详情。",
		"config 及其三个必填子字段必须提供。",
		"URL 来源需要额外提供 businessId 和上传接口返回的 fileHash。",
		"CLI 仅接受以上字段，其他字段会按未知字段拒绝。",
	)
	addJSONInputFlags(command, &inputPath, &inline)
	command.Flags().DurationVar(&interval, "interval", 2*time.Second, "轮询间隔")
	command.Flags().DurationVar(&deadline, "deadline", 10*time.Minute, "最长等待时间")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "只校验并输出工作流输入")
	command.Flags().BoolVar(&printInput, "print-input", false, "输出规范化工作流输入")
	return command
}

// buildReviewService 装配 Profile、TokenProvider、HTTP Adapter 和领域 Service。
// 入参：runtime *Runtime 为依赖；root *rootOptions 为 Profile 和超时 flags。
// 返回值：*review.Service 为领域客户端；config.Profile 为渲染默认值；error 为配置失败。
func buildReviewService(runtime *Runtime, root *rootOptions) (*review.Service, config.Profile, error) {
	profile, err := selectedProfile(runtime, root)
	if err != nil {
		return nil, config.Profile{}, err
	}
	identity, err := selectedIdentity(profile, root.Identity)
	if err != nil {
		return nil, config.Profile{}, err
	}
	provider := auth.NewProvider(runtime.Tokens, runtime.HTTP, runtime.Now, runtime.Secrets)
	client := openplatform.NewClientForIdentity(profile, provider, runtime.HTTP, identity)
	return review.NewService(client, root.Timeout), profile, nil
}

// addJSONInputFlags 为复杂写操作统一注册 --input/--data。
// 入参：command *cobra.Command 为目标命令；inputPath/inline *string 接收 flag 值。
// 返回值：无。
func addJSONInputFlags(command *cobra.Command, inputPath *string, inline *string) {
	command.Flags().StringVar(inputPath, "input", "", "JSON 输入文件")
	command.Flags().StringVar(inline, "data", "", "内联 JSON 输入")
}

// addTaskQueryFlags 为 status/info/result 注册一致的任务查询参数。
// 入参：command *cobra.Command 为目标命令；query *review.TaskQuery 接收值。
// 返回值：无。
func addTaskQueryFlags(command *cobra.Command, query *review.TaskQuery) {
	command.Flags().Int64Var(&query.TaskID, "task-id", 0, "审查任务 ID")
	command.Flags().StringVar(&query.BusinessID, "business-id", "", "业务对象 ID")
	command.Flags().StringVar(&query.VisibilityScope, "visibility-scope", "", "可选可见性范围：contractResult")
	_ = command.MarkFlagRequired("task-id")
}
