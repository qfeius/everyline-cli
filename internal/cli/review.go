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
	command := &cobra.Command{Use: "review", Short: "执行合同智能审查"}
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
	command := &cobra.Command{Use: "file", Short: "准备审查文件"}
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
	var appType string
	var businessID string
	var dryRun bool
	var printInput bool
	command := &cobra.Command{
		Use:   "upload",
		Short: "上传本地合同文件",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if err := review.ValidateUploadFile(filePath); err != nil {
				return err
			}
			if err := review.ValidateFileName(name); err != nil {
				return err
			}
			if err := review.ValidateUploadBusinessContext(appType, businessID); err != nil {
				return err
			}
			input := map[string]any{"file": filePath, "name": name, "appType": appType}
			if businessID != "" {
				input["businessId"] = businessID
			}
			if dryRun || printInput {
				return render(runtime, root, "json", input)
			}
			service, profile, err := buildReviewService(runtime, root)
			if err != nil {
				return err
			}
			progress(runtime, root, "正在上传合同文件...")
			result, err := service.UploadFile(command.Context(), filePath, name, appType, businessID)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	command.Flags().StringVar(&filePath, "file", "", "本地合同路径")
	command.Flags().StringVar(&name, "name", "", "业务文件名（含扩展名）")
	command.Flags().StringVar(&appType, "app-type", review.AppTypeThirdParty, "接入类型：CLM|CR|THIRD_PARTY")
	command.Flags().StringVar(&businessID, "business-id", "", "可选业务对象 ID")
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
	command := &cobra.Command{Use: "subject", Short: "提取合同主体"}
	command.AddCommand(newReviewSubjectExtractCommand(runtime, root))
	return command
}

// newReviewSubjectExtractCommand 创建主体提取命令，并复用文件身份四元组校验。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 dry-run 和实际调用。
func newReviewSubjectExtractCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var request review.StartRequest
	var dryRun bool
	var printInput bool
	command := &cobra.Command{
		Use:   "extract",
		Short: "无副作用提取合同参与方",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if err := review.ValidateSubjectIdentity(request); err != nil {
				return err
			}
			// dry-run/print-input 必须展示与真实主体提取请求一致的字符串 fileId。
			input := map[string]any{"businessId": request.BusinessID, "appType": request.AppType, "fileId": strconv.FormatInt(request.FileID, 10)}
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
	command.Flags().StringVar(&request.AppType, "app-type", review.AppTypeThirdParty, "接入类型")
	command.Flags().Int64Var(&request.FileID, "file-id", 0, "平台文件 ID")
	command.Flags().StringVar(&request.FileHash, "file-hash", "", "服务端文件指纹")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "只校验并输出请求，不调用远端")
	command.Flags().BoolVar(&printInput, "print-input", false, "输出规范化请求，不调用远端")
	_ = command.MarkFlagRequired("business-id")
	_ = command.MarkFlagRequired("file-id")
	return command
}

// newReviewTaskCommand 创建普通任务的发起、查询、详情和等待命令组。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，包含 start/status/info/wait。
func newReviewTaskCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "task", Short: "管理审查任务"}
	command.AddCommand(
		newReviewTaskStartCommand(runtime, root),
		newReviewTaskStatusCommand(runtime, root),
		newReviewTaskInfoCommand(runtime, root),
		newReviewTaskWaitCommand(runtime, root),
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
		Short: "发起普通 V3 智审任务",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			var request review.StartRequest
			if err := readJSONInput(inputPath, inline, &request); err != nil {
				return err
			}
			request = request.Normalize()
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
	addTaskQueryFlags(command, &query)
	return command
}

// newReviewTaskWaitCommand 创建 CLI 侧轮询器，不映射新的远端接口。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 interval/deadline。
func newReviewTaskWaitCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	query := review.TaskQuery{}
	var interval time.Duration
	var deadline time.Duration
	command := &cobra.Command{
		Use:   "wait",
		Short: "轮询直到审查任务终态",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			service, profile, err := buildReviewService(runtime, root)
			if err != nil {
				return err
			}
			workflow := review.NewWorkflow(service, review.RealClock{}, review.WorkflowOptions{Interval: interval, Deadline: deadline})
			progress(runtime, root, "正在等待审查任务进入终态...")
			result, err := workflow.Wait(command.Context(), query)
			if err != nil {
				if renderErr := render(runtime, root, profile.DefaultOutput, result); renderErr != nil {
					return fmt.Errorf("%w；输出阶段结果失败: %v", err, renderErr)
				}
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
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
		Args:  cobra.NoArgs,
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
	provider := auth.NewProvider(runtime.Tokens, runtime.HTTP, runtime.Now)
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

// addTaskQueryFlags 为 status/info/wait 注册一致的任务查询参数。
// 入参：command *cobra.Command 为目标命令；query *review.TaskQuery 接收值。
// 返回值：无。
func addTaskQueryFlags(command *cobra.Command, query *review.TaskQuery) {
	command.Flags().Int64Var(&query.TaskID, "task-id", 0, "审查任务 ID")
	command.Flags().StringVar(&query.BusinessID, "business-id", "", "业务对象 ID")
	command.Flags().StringVar(&query.AppType, "app-type", "", "可选接入类型")
	command.Flags().StringVar(&query.VisibilityScope, "visibility-scope", "", "可选可见性范围：contractResult")
	_ = command.MarkFlagRequired("task-id")
}
