package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeAPI 模拟上传、发起、两次状态轮询和详情返回。
type fakeAPI struct {
	statuses []Document
	index    int
	calls    []string
	infoErr  error
	queries  []TaskQuery
	contexts map[string]context.Context
}

// recordContext 保存每个工作流阶段收到的上下文，便于验证 deadline 沿链路传递。
// 入参：name string 为阶段名；ctx context.Context 为阶段上下文。
// 返回值：无。
func (api *fakeAPI) recordContext(name string, ctx context.Context) {
	if api.contexts == nil {
		api.contexts = map[string]context.Context{}
	}
	api.contexts[name] = ctx
}

// missingFileHashAPI 模拟上传响应缺少 fileHash 的场景，确认工作流不会再调用已移除的快照接口。
type missingFileHashAPI struct {
	fakeAPI
}

// urlUploadAPI 模拟 URL 上传接口只返回 fileId，业务身份由工作流输入补齐。
type urlUploadAPI struct {
	fakeAPI
}

// UploadFile 返回缺少文件指纹的上传结果。
// 入参：context 和字符串参数仅满足 API。
// 返回值：Document 为不完整上传结果；error 为 nil。
func (api *missingFileHashAPI) UploadFile(context.Context, string, string, string, string) (Document, error) {
	api.calls = append(api.calls, "upload")
	return Document{"fileId": int64(11), "businessId": "biz-1"}, nil
}

// UploadURL 返回标准 URL 上传接口的最小文件结果。
func (api *urlUploadAPI) UploadURL(context.Context, string, string) (Document, error) {
	api.calls = append(api.calls, "upload-url")
	return Document{"fileId": int64(12)}, nil
}

// UploadFile 返回带完整文件身份的上传结果。
// 入参：context 和字符串参数仅满足 API。
// 返回值：Document 为上传结果；error 为 nil。
func (api *fakeAPI) UploadFile(ctx context.Context, _ string, _ string, _ string, _ string) (Document, error) {
	api.recordContext("upload", ctx)
	api.calls = append(api.calls, "upload")
	return Document{"fileId": int64(11), "businessId": "biz-1", "fileHash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, nil
}

// UploadURL 在本测试中不应调用。
// 入参：context 和字符串参数仅满足 API。
// 返回值：Document 为空；error 为 nil。
func (api *fakeAPI) UploadURL(context.Context, string, string) (Document, error) {
	api.calls = append(api.calls, "upload-url")
	return Document{}, nil
}

// ExtractSubjects 返回一个主体候选。
// 入参：context 和 StartRequest 仅满足 API。
// 返回值：Document 为主体候选；error 为 nil。
func (api *fakeAPI) ExtractSubjects(ctx context.Context, _ StartRequest) (Document, error) {
	api.recordContext("subjects", ctx)
	api.calls = append(api.calls, "subjects")
	return Document{"counterparts": []any{"甲方"}}, nil
}

// Start 返回 running 任务快照。
// 入参：context 和 StartRequest 仅满足 API。
// 返回值：Document 为任务快照；error 为 nil。
func (api *fakeAPI) Start(ctx context.Context, _ StartRequest) (Document, error) {
	api.recordContext("start", ctx)
	api.calls = append(api.calls, "start")
	return Document{"taskId": int64(88), "status": "running"}, nil
}

// StartFeishu 仅满足 API 接口，工作流不调用字段捷径入口。
func (api *fakeAPI) StartFeishu(context.Context, FeishuStartRequest) (Document, error) {
	api.calls = append(api.calls, "start-feishu")
	return Document{"taskId": int64(89), "status": "running"}, nil
}

// Status 按顺序返回预设状态。
// 入参：context 和 TaskQuery 仅满足 API。
// 返回值：Document 为当前状态；error 为 nil。
func (api *fakeAPI) Status(ctx context.Context, query TaskQuery) (Document, error) {
	api.recordContext("status", ctx)
	api.calls = append(api.calls, "status")
	api.queries = append(api.queries, query)
	status := api.statuses[api.index]
	api.index++
	return status, nil
}

// Info 返回最终展示详情。
// 入参：context 和 TaskQuery 仅满足 API。
// 返回值：Document 为最终详情；error 为 nil。
func (api *fakeAPI) Info(ctx context.Context, query TaskQuery) (Document, error) {
	api.recordContext("info", ctx)
	api.calls = append(api.calls, "info")
	api.queries = append(api.queries, query)
	if api.infoErr != nil {
		return nil, api.infoErr
	}
	return Document{"taskId": int64(88), "status": "success", "result": []any{}}, nil
}

// instantClock 让轮询测试无需真实等待。
type instantClock struct{}

// After 立即返回，不阻塞测试。
// 入参：context 和 delay 仅满足 Clock。
// 返回值：error 始终为 nil。
func (instantClock) After(context.Context, time.Duration) error { return nil }

type failingClock struct{ err error }

func (clock failingClock) After(context.Context, time.Duration) error { return clock.err }

type fakeFeishuAPI struct {
	snapshots []Document
	index     int
	queries   []FeishuTaskQuery
}

// FeishuInfo 返回预设字段捷径任务详情，供轮询器测试数值状态转换。
func (api *fakeFeishuAPI) FeishuInfo(_ context.Context, query FeishuTaskQuery) (Document, error) {
	api.queries = append(api.queries, query)
	snapshot := api.snapshots[api.index]
	api.index++
	return snapshot, nil
}

// UploadFile 满足 Workflow API，用于构造字段捷径等待器测试实例。
func (api *fakeFeishuAPI) UploadFile(context.Context, string, string, string, string) (Document, error) {
	return nil, nil
}

// UploadURL 满足 Workflow API，用于构造字段捷径等待器测试实例。
func (api *fakeFeishuAPI) UploadURL(context.Context, string, string) (Document, error) {
	return nil, nil
}

// ExtractSubjects 满足 Workflow API，用于构造字段捷径等待器测试实例。
func (api *fakeFeishuAPI) ExtractSubjects(context.Context, StartRequest) (Document, error) {
	return nil, nil
}

// Start 满足 Workflow API，用于构造字段捷径等待器测试实例。
func (api *fakeFeishuAPI) Start(context.Context, StartRequest) (Document, error) { return nil, nil }

// StartFeishu 满足 Workflow API，用于构造字段捷径等待器测试实例。
func (api *fakeFeishuAPI) StartFeishu(context.Context, FeishuStartRequest) (Document, error) {
	return nil, nil
}

// Status 满足 Workflow API，用于构造字段捷径等待器测试实例。
func (api *fakeFeishuAPI) Status(context.Context, TaskQuery) (Document, error) { return nil, nil }

// Info 满足 Workflow API，用于构造字段捷径等待器测试实例。
func (api *fakeFeishuAPI) Info(context.Context, TaskQuery) (Document, error) { return nil, nil }

// TestWorkflowRun 验证上传、主体提取、发起、轮询、详情的固定顺序。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestWorkflowRun(t *testing.T) {
	api := &fakeAPI{statuses: []Document{{"status": "running"}, {"status": "success"}}}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	result, err := workflow.Run(context.Background(), RunSpec{
		Source:          RunSource{Type: "file", Path: "contract.pdf", Name: "合同.pdf"},
		BusinessID:      "biz-1",
		AppType:         AppTypeCLM,
		Config:          map[string]any{},
		ExtractSubjects: true,
		Wait:            true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := StringValue(result.Final, "status"); status != "success" {
		t.Fatalf("result=%#v", result)
	}
	expected := []string{"upload", "subjects", "start", "status", "status", "info"}
	if len(api.calls) != len(expected) {
		t.Fatalf("calls=%#v", api.calls)
	}
	for index := range expected {
		if api.calls[index] != expected[index] {
			t.Fatalf("calls=%#v", api.calls)
		}
	}
	for _, query := range api.queries {
		if query.VisibilityScope != VisibilityScopeContractResult {
			t.Fatalf("query=%#v，CLM/CR 链路应使用 contractResult", query)
		}
	}
}

// TestWorkflowWaitFeishu 验证字段捷径详情接口按数值状态轮询并返回完整终态快照。
func TestWorkflowWaitFeishu(t *testing.T) {
	api := &fakeFeishuAPI{snapshots: []Document{{"smartAuditId": 88, "taskStatus": 0}, {"smartAuditId": 88, "taskStatus": 1, "result": []any{}}}}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	snapshot, err := workflow.WaitFeishu(context.Background(), FeishuTaskQuery{SmartAuditID: 88, ReviewPosition: "legal"})
	if err != nil {
		t.Fatal(err)
	}
	if status := FeishuTaskStatus(snapshot); status != "success" {
		t.Fatalf("snapshot=%#v status=%q", snapshot, status)
	}
	if len(api.queries) != 2 || api.queries[0].SmartAuditID != 88 || api.queries[0].ReviewPosition != "legal" {
		t.Fatalf("queries=%#v", api.queries)
	}
}

// TestWorkflowWaitFeishuPreservesFailure 验证字段捷径失败终态返回原始快照和统一任务错误。
func TestWorkflowWaitFeishuPreservesFailure(t *testing.T) {
	api := &fakeFeishuAPI{snapshots: []Document{{"smartAuditId": 88, "taskStatus": 2, "msg": "规则失败"}}}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	snapshot, err := workflow.WaitFeishu(context.Background(), FeishuTaskQuery{SmartAuditID: 88})
	if !errors.Is(err, ErrTaskFailed) || snapshot == nil {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
}

// TestRunSpecCLMRequiresBusinessID 验证 review-run Schema 在上传前拦截缺少业务对象 ID 的输入。
func TestRunSpecCLMRequiresBusinessID(t *testing.T) {
	spec := RunSpec{
		Source:  RunSource{Type: "file", Path: "contract.pdf", Name: "合同.pdf"},
		AppType: AppTypeCLM,
		Config:  map[string]any{},
	}
	if err := spec.Validate(); err == nil || !strings.Contains(err.Error(), "businessId") {
		t.Fatalf("err=%v", err)
	}
}

// TestRunSpecURLRejectsInvalidFileHash 验证 review-run Schema 不接受非 SHA-256 指纹。
func TestRunSpecURLRejectsInvalidFileHash(t *testing.T) {
	spec := RunSpec{
		Source:     RunSource{Type: "url", FileURL: "https://files.example.com/contract.pdf", Name: "合同.pdf"},
		BusinessID: "biz-url",
		FileHash:   "not-a-sha256",
		AppType:    AppTypeThirdParty,
		Config:     map[string]any{},
	}
	if err := spec.Validate(); err == nil || !strings.Contains(err.Error(), "fileHash") {
		t.Fatalf("err=%v", err)
	}
}

// TestWorkflowRunRequiresUploadedFileHash 验证上传结果缺少 fileHash 时直接失败，不再回退调用文件快照。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestWorkflowRunRequiresUploadedFileHash(t *testing.T) {
	api := &missingFileHashAPI{}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	result, err := workflow.Run(context.Background(), RunSpec{
		Source:  RunSource{Type: "file", Path: "contract.pdf", Name: "合同.pdf"},
		AppType: AppTypeThirdParty,
		Config:  map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), "上传响应缺少 businessId 或 fileHash") {
		t.Fatalf("err=%v，期望上传响应缺少 fileHash 时失败", err)
	}
	if fileID, ok := Int64Value(result.Upload, "fileId"); !ok || fileID != 11 {
		t.Fatalf("upload=%#v，错误返回应保留上传响应", result.Upload)
	}
	if len(api.calls) != 1 || api.calls[0] != "upload" {
		t.Fatalf("calls=%#v，缺少 fileHash 时不应继续发起审查", api.calls)
	}
}

// TestWorkflowRunPreservesStatusWhenInfoFails 验证详情接口失败时仍保留已确认的成功状态快照。
func TestWorkflowRunPreservesStatusWhenInfoFails(t *testing.T) {
	api := &fakeAPI{
		statuses: []Document{{"status": "success"}},
		infoErr:  fmt.Errorf("info unavailable"),
	}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	result, err := workflow.Run(context.Background(), RunSpec{
		Source:     RunSource{Type: "file", Path: "contract.pdf", Name: "合同.pdf"},
		BusinessID: "biz-1",
		AppType:    AppTypeThirdParty,
		Config:     map[string]any{},
		Wait:       true,
	})
	if err == nil || !strings.Contains(err.Error(), "info unavailable") {
		t.Fatalf("err=%v", err)
	}
	if status, _ := StringValue(result.Final, "status"); status != "success" {
		t.Fatalf("final=%#v，详情失败时应保留成功状态", result.Final)
	}
}

// TestWorkflowRunSharesDeadlineAcrossStages 验证一键工作流的统一 deadline 会传递到上传、轮询和详情请求。
func TestWorkflowRunSharesDeadlineAcrossStages(t *testing.T) {
	api := &fakeAPI{statuses: []Document{{"status": "success"}}}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	_, err := workflow.Run(context.Background(), RunSpec{
		Source:     RunSource{Type: "file", Path: "contract.pdf", Name: "合同.pdf"},
		BusinessID: "biz-1",
		AppType:    AppTypeThirdParty,
		Config:     map[string]any{},
		Wait:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"upload", "start", "status", "info"} {
		ctx := api.contexts[stage]
		if ctx == nil {
			t.Fatalf("stage=%s 未收到上下文", stage)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatalf("stage=%s 未继承工作流 deadline", stage)
		}
	}
}

// TestWorkflowWaitPreservesUnknownStatusSnapshot 验证未知状态错误仍携带远端快照。
func TestWorkflowWaitPreservesUnknownStatusSnapshot(t *testing.T) {
	api := &fakeAPI{statuses: []Document{{"status": "queued", "message": "等待调度"}}}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	snapshot, err := workflow.Wait(context.Background(), TaskQuery{TaskID: 88})
	if err == nil || !strings.Contains(err.Error(), "未知任务状态") {
		t.Fatalf("err=%v", err)
	}
	if status, _ := StringValue(snapshot, "status"); status != "queued" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
}

// TestWorkflowWaitReportsMissingTaskClearly 验证后端返回空状态时给出可行动的任务可见性提示。
func TestWorkflowWaitReportsMissingTaskClearly(t *testing.T) {
	api := &fakeAPI{statuses: []Document{{"taskId": int64(88)}}}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	snapshot, err := workflow.Wait(context.Background(), TaskQuery{TaskID: 88})
	if err == nil || !strings.Contains(err.Error(), "任务不存在或当前用户无权访问") {
		t.Fatalf("err=%v", err)
	}
	if taskID, ok := Int64Value(snapshot, "taskId"); !ok || taskID != 88 {
		t.Fatalf("snapshot=%#v", snapshot)
	}
}

// TestWorkflowWaitPreservesSnapshotOnCancellation 验证轮询被取消时仍返回最后一次状态快照。
func TestWorkflowWaitPreservesSnapshotOnCancellation(t *testing.T) {
	api := &fakeAPI{statuses: []Document{{"taskId": int64(88), "status": "running"}}}
	workflow := NewWorkflow(api, failingClock{err: context.Canceled}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	snapshot, err := workflow.Wait(context.Background(), TaskQuery{TaskID: 88})
	if err == nil || !strings.Contains(err.Error(), "等待任务终态") {
		t.Fatalf("err=%v", err)
	}
	if status, _ := StringValue(snapshot, "status"); status != "running" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
}

// TestWorkflowRunURLUsesExplicitFileIdentity 验证 URL 上传响应缺少业务身份时可使用调用方提供的元数据继续链路。
func TestWorkflowRunURLUsesExplicitFileIdentity(t *testing.T) {
	api := &urlUploadAPI{}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	result, err := workflow.Run(context.Background(), RunSpec{
		Source:     RunSource{Type: "url", FileURL: "https://files.example.com/contract.pdf", Name: "合同.pdf"},
		BusinessID: "biz-url",
		FileHash:   "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		AppType:    AppTypeThirdParty,
		Config:     map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if taskID, ok := Int64Value(result.Start, "taskId"); !ok || taskID != 88 {
		t.Fatalf("start=%#v", result.Start)
	}
	expected := []string{"upload-url", "start"}
	if len(api.calls) != len(expected) {
		t.Fatalf("calls=%#v", api.calls)
	}
	for index := range expected {
		if api.calls[index] != expected[index] {
			t.Fatalf("calls=%#v", api.calls)
		}
	}
}

// TestWorkflowRunURLRequiresFileIdentity 验证 URL 链路缺少业务身份时在 Schema 层提前拦截。
func TestWorkflowRunURLRequiresFileIdentity(t *testing.T) {
	api := &urlUploadAPI{}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	_, err := workflow.Run(context.Background(), RunSpec{
		Source:  RunSource{Type: "url", FileURL: "https://files.example.com/contract.pdf", Name: "合同.pdf"},
		AppType: AppTypeThirdParty,
		Config:  map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), "businessId") {
		t.Fatalf("err=%v", err)
	}
	if len(api.calls) != 0 {
		t.Fatalf("calls=%#v", api.calls)
	}
}

// TestWorkflowRunReturnsFailedSnapshot 验证远端任务失败时工作流返回可识别错误，并保留失败终态供调用方诊断。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestWorkflowRunReturnsFailedSnapshot(t *testing.T) {
	api := &fakeAPI{statuses: []Document{{"status": "fail", "message": "规则执行失败"}}}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	result, err := workflow.Run(context.Background(), RunSpec{
		Source:     RunSource{Type: "file", Path: "contract.pdf", Name: "合同.pdf"},
		BusinessID: "biz-1",
		AppType:    AppTypeThirdParty,
		Config:     map[string]any{},
		Wait:       true,
	})
	if !errors.Is(err, ErrTaskFailed) {
		t.Fatalf("err=%v，期望 ErrTaskFailed", err)
	}
	if status, _ := StringValue(result.Final, "status"); status != "fail" {
		t.Fatalf("final=%#v，期望保留失败终态", result.Final)
	}
	for _, call := range api.calls {
		if call == "info" {
			t.Fatalf("失败终态后不应继续查询详情: calls=%#v", api.calls)
		}
	}
}
