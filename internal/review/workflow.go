package review

import (
	"context"
	"fmt"
	"time"
)

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

// Workflow 隐藏上传、快照、主体提取、发起、轮询和详情编排。
type Workflow struct {
	api     API
	clock   Clock
	options WorkflowOptions
}

// NewWorkflow 创建审查工作流。
// 入参：api API 为领域操作；clock Clock 为可测试时钟；options WorkflowOptions 为轮询参数。
// 返回值：*Workflow，可执行一键审查和等待。
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

// Run 执行上传、必要时快照/主体提取、发起审查，并按 spec.wait 获取终态详情。
// 入参：ctx context.Context 控制取消；spec RunSpec 为稳定工作流输入。
// 返回值：RunResult 汇总阶段结果；error 为校验、远端或轮询失败。
func (workflow *Workflow) Run(ctx context.Context, spec RunSpec) (RunResult, error) {
	spec = spec.Normalize()
	if err := spec.Validate(); err != nil {
		return RunResult{}, err
	}
	var upload Document
	var err error
	if spec.Source.Type == "file" {
		upload, err = workflow.api.UploadFile(ctx, spec.Source.Path, spec.Source.Name, spec.AppType, spec.BusinessID)
	} else {
		upload, err = workflow.api.UploadURL(ctx, spec.Source.FileURL, spec.Source.Name)
	}
	if err != nil {
		return RunResult{}, err
	}

	fileID, ok := Int64Value(upload, "fileId")
	if !ok || fileID <= 0 {
		return RunResult{}, fmt.Errorf("上传响应缺少有效 fileId")
	}
	businessID, _ := StringValue(upload, "businessId")
	if businessID == "" {
		businessID = spec.BusinessID
	}
	fileHash, _ := StringValue(upload, "fileHash")
	if fileHash == "" {
		snapshot, snapshotErr := workflow.api.Snapshot(ctx, fileID, spec.AppType, businessID)
		if snapshotErr != nil {
			return RunResult{}, snapshotErr
		}
		fileHash, _ = StringValue(snapshot, "fileHash")
	}
	if businessID == "" || fileHash == "" {
		return RunResult{}, fmt.Errorf("上传/快照响应缺少 businessId 或 fileHash")
	}
	startRequest := StartRequest{
		BusinessID:   businessID,
		AppType:      spec.AppType,
		FileID:       fileID,
		FileHash:     fileHash,
		Config:       spec.Config,
		TriggerScene: "manual",
	}
	result := RunResult{Upload: upload}
	if spec.ExtractSubjects {
		result.Subjects, err = workflow.api.ExtractSubjects(ctx, startRequest)
		if err != nil {
			return result, err
		}
	}
	result.Start, err = workflow.api.Start(ctx, startRequest)
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
	query := TaskQuery{TaskID: taskID, BusinessID: businessID, AppType: spec.AppType}
	if _, err := workflow.Wait(ctx, query); err != nil {
		return result, err
	}
	result.Final, err = workflow.api.Info(ctx, query)
	return result, err
}

// Wait 持续查询 status，直到 success/fail 或 deadline/cancel。
// 入参：ctx context.Context 控制取消；query TaskQuery 为任务身份。
// 返回值：Document 为最后状态快照；error 为超时、取消或 API 失败。
func (workflow *Workflow) Wait(ctx context.Context, query TaskQuery) (Document, error) {
	waitContext, cancel := context.WithTimeout(ctx, workflow.options.Deadline)
	defer cancel()
	for {
		snapshot, err := workflow.api.Status(waitContext, query)
		if err != nil {
			return nil, err
		}
		status, _ := StringValue(snapshot, "status")
		if status == "success" || status == "fail" {
			return snapshot, nil
		}
		if status != "running" {
			return nil, fmt.Errorf("未知任务状态: %q", status)
		}
		if err := workflow.clock.After(waitContext, workflow.options.Interval); err != nil {
			return nil, fmt.Errorf("等待任务终态: %w", err)
		}
	}
}
