package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrTaskFailed 表示远端审查任务已进入明确的失败终态。
var ErrTaskFailed = errors.New("审查任务失败")

// Clock 隔离真实时间与测试假时钟。
type Clock interface {
	After(context.Context, time.Duration) error
}

// RealClock 使用可取消定时器驱动生产轮询。
type RealClock struct{}

// After 等待指定时长或上层取消。
// 入参：ctx context.Context 控制取消；delay time.Duration 为等待时长。
// 返回值：error，取消时为 ctx.Err()，正常等待为 nil。
func (RealClock) After(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// WorkflowOptions 控制轮询间隔和总截止时间。
type WorkflowOptions struct {
	Interval time.Duration
	Deadline time.Duration
}

// ReviewWorkflow 是面向 CLI 的高阶深接口。
type ReviewWorkflow interface {
	Run(context.Context, RunSpec) (RunResult, error)
}

// Workflow 隐藏上传、主体提取、发起、轮询和详情编排。
type Workflow struct {
	api     API
	clock   Clock
	options WorkflowOptions
}

// NewWorkflow 创建审查工作流。
// 入参：api API 为领域操作；clock Clock 为可测试时钟；options WorkflowOptions 为轮询参数。
// 返回值：*Workflow，可执行一键审查、等待和结果获取。
func NewWorkflow(api API, clock Clock, options WorkflowOptions) *Workflow {
	if clock == nil {
		clock = RealClock{}
	}
	if options.Interval <= 0 {
		options.Interval = 2 * time.Second
	}
	if options.Deadline <= 0 {
		options.Deadline = 10 * time.Minute
	}
	return &Workflow{api: api, clock: clock, options: options}
}

// Run 执行上传、主体提取、发起审查，并按 spec.wait 获取终态详情。
// 入参：ctx context.Context 控制取消；spec RunSpec 为稳定工作流输入。
// 返回值：RunResult 汇总阶段结果；error 为校验、远端或轮询失败。
func (workflow *Workflow) Run(ctx context.Context, spec RunSpec) (RunResult, error) {
	// 统一为整条工作流创建截止上下文，确保上传、提取、发起和详情查询共享同一份时间预算。
	workflowContext, cancel := context.WithTimeout(ctx, workflow.options.Deadline)
	defer cancel()
	spec = spec.Normalize()
	if err := spec.Validate(); err != nil {
		return RunResult{}, err
	}
	var upload Document
	var err error
	if spec.Source.Type == "file" {
		upload, err = workflow.api.UploadFile(workflowContext, spec.Source.Path, spec.Source.Name, spec.AppType, spec.BusinessID)
	} else {
		upload, err = workflow.api.UploadURL(workflowContext, spec.Source.FileURL, spec.Source.Name)
	}
	if err != nil {
		return RunResult{}, err
	}

	result := RunResult{Upload: upload}
	fileID, ok := Int64Value(upload, "fileId")
	if !ok || fileID <= 0 {
		return result, fmt.Errorf("上传响应缺少有效 fileId")
	}
	businessID, _ := StringValue(upload, "businessId")
	businessID = strings.TrimSpace(businessID)
	if businessID == "" {
		businessID = strings.TrimSpace(spec.BusinessID)
	}
	fileHash, _ := StringValue(upload, "fileHash")
	fileHash = normalizeSHA256(fileHash)
	if fileHash == "" {
		fileHash = normalizeSHA256(spec.FileHash)
	}
	if businessID == "" || fileHash == "" {
		if spec.Source.Type == "url" {
			return result, fmt.Errorf("URL 来源需要在工作流输入中提供 businessId 和 fileHash（文件内容的 SHA-256）")
		}
		return result, fmt.Errorf("上传响应缺少 businessId 或 fileHash")
	}
	if err := ValidateFileHash(fileHash); err != nil {
		return result, fmt.Errorf("上传响应中的 fileHash 无效: %w", err)
	}
	startRequest := StartRequest{
		BusinessID:   businessID,
		AppType:      spec.AppType,
		FileID:       fileID,
		FileHash:     fileHash,
		Config:       spec.Config,
		TriggerScene: "manual",
	}
	if spec.ExtractSubjects {
		result.Subjects, err = workflow.api.ExtractSubjects(workflowContext, startRequest)
		if err != nil {
			return result, err
		}
	}
	result.Start, err = workflow.api.Start(workflowContext, startRequest)
	if err != nil {
		return result, err
	}
	if !spec.Wait {
		return result, nil
	}
	taskID, ok := Int64Value(result.Start, "taskId")
	if !ok || taskID <= 0 {
		return result, fmt.Errorf("发起审查响应缺少有效 taskId")
	}
	query := TaskQuery{
		TaskID:          taskID,
		BusinessID:      businessID,
		AppType:         spec.AppType,
		VisibilityScope: workflowVisibilityScope(spec.AppType),
	}
	result.Final, err = workflow.WaitForResult(workflowContext, query)
	if err != nil {
		return result, err
	}
	return result, nil
}

// workflowVisibilityScope 让 CLM/CR 的一键链路使用业务对象视角读取可复用任务。
func workflowVisibilityScope(appType string) string {
	if appType == AppTypeCLM || appType == AppTypeCR {
		return VisibilityScopeContractResult
	}
	return ""
}

// Wait 持续查询 status，直到 success/fail 或 deadline/cancel。
// 入参：ctx context.Context 控制取消；query TaskQuery 为任务身份。
// 返回值：Document 为最后状态快照；error 为任务失败、超时、取消或 API 失败。
func (workflow *Workflow) Wait(ctx context.Context, query TaskQuery) (Document, error) {
	waitContext, cancel := context.WithTimeout(ctx, workflow.options.Deadline)
	defer cancel()
	for {
		snapshot, err := workflow.api.Status(waitContext, query)
		if err != nil {
			return nil, err
		}
		status, _ := StringValue(snapshot, "status")
		if status == "success" {
			return snapshot, nil
		}
		if status == "" {
			return snapshot, fmt.Errorf("任务不存在或当前用户无权访问")
		}
		if status == "fail" {
			failureMessage, _ := StringValue(snapshot, "message")
			if failureMessage != "" {
				return snapshot, fmt.Errorf("%w: %s", ErrTaskFailed, failureMessage)
			}
			return snapshot, ErrTaskFailed
		}
		if status != "running" {
			return snapshot, fmt.Errorf("未知任务状态: %q", status)
		}
		if err := workflow.clock.After(waitContext, workflow.options.Interval); err != nil {
			return snapshot, fmt.Errorf("等待任务终态: %w", err)
		}
	}
}

// WaitForResult 等待任务成功并获取一次最终详情。
// 入参：ctx context.Context 控制取消；query TaskQuery 为任务身份。
// 返回值：Document 为最终详情或失败时的最后状态快照；error 为任务失败、超时、取消或详情查询失败。
func (workflow *Workflow) WaitForResult(ctx context.Context, query TaskQuery) (Document, error) {
	resultContext, cancel := context.WithTimeout(ctx, workflow.options.Deadline)
	defer cancel()

	snapshot, err := workflow.Wait(resultContext, query)
	if err != nil {
		return snapshot, err
	}
	info, err := workflow.api.Info(resultContext, query)
	if err != nil {
		return snapshot, err
	}
	return info, nil
}
