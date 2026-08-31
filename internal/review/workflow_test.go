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
	info     Document
	queries  []TaskQuery
	contexts map[string]context.Context
}

// validReviewConfig 返回与主体提取夹具一致的合法审查配置。
// 入参：无。
// 返回值：map[string]any，包含同一主体候选的名称、角色、强度和规则来源。
func validReviewConfig() map[string]any {
	return map[string]any{
		"selectedPosition":             "中北大学",
		"selectedAuditRole":            "甲方",
		"reviewStrength":               1,
		"matchContractTypeRulePackage": true,
	}
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

// urlUploadAPI 模拟 V3 URL 上传接口返回完整文件身份。
type urlUploadAPI struct {
	fakeAPI
}

// missingURLIdentityAPI 模拟异常 V3 URL 上传响应，验证工作流不会发送不完整 startReview。
type missingURLIdentityAPI struct {
	fakeAPI
}

// UploadFile 返回缺少文件指纹的上传结果。
// 入参：context 和字符串参数仅满足 API。
// 返回值：Document 为不完整上传结果；error 为 nil。
func (api *missingFileHashAPI) UploadFile(context.Context, string, string, string, string) (Document, error) {
	api.calls = append(api.calls, "upload")
	return Document{"fileId": int64(11), "businessId": "biz-1"}, nil
}

// UploadURL 返回 V3 URL 上传接口的完整文件身份。
func (api *urlUploadAPI) UploadURL(context.Context, string, string) (Document, error) {
	api.calls = append(api.calls, "upload-url")
	return Document{
		"fileId":     int64(12),
		"businessId": "biz-url",
		"fileHash":   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, nil
}

// UploadURL 返回缺少业务身份的异常上传结果。
// 入参：context 和字符串参数仅满足 API。
// 返回值：Document 仅含 fileId；error 为 nil。
func (api *missingURLIdentityAPI) UploadURL(context.Context, string, string) (Document, error) {
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
	return Document{"counterparts": []any{map[string]any{"name": "中北大学", "role": "甲方"}}}, nil
}

// Start 返回 running 任务快照。
// 入参：context 和 StartRequest 仅满足 API。
// 返回值：Document 为任务快照；error 为 nil。
func (api *fakeAPI) Start(ctx context.Context, _ StartRequest) (Document, error) {
	api.recordContext("start", ctx)
	api.calls = append(api.calls, "start")
	return Document{"taskId": int64(88), "status": "running"}, nil
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
	if api.info != nil {
		return api.info, nil
	}
	return Document{"taskId": int64(88), "status": "success", "url": "https://review.example.com/tasks/88", "result": []any{}}, nil
}

// instantClock 让轮询测试无需真实等待。
type instantClock struct{}

// After 立即返回，不阻塞测试。
// 入参：context 和 delay 仅满足 Clock。
// 返回值：error 始终为 nil。
func (instantClock) After(context.Context, time.Duration) error { return nil }

type failingClock struct{ err error }

func (clock failingClock) After(context.Context, time.Duration) error { return clock.err }

// TestWorkflowRun 验证上传、主体提取、发起、轮询、详情的固定顺序。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestWorkflowRun(t *testing.T) {
	api := &fakeAPI{statuses: []Document{{"status": "running"}, {"status": "success"}}}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	result, err := workflow.Run(context.Background(), RunSpec{
		Source:          RunSource{Type: "file", Path: "contract.pdf", Name: "合同.pdf"},
		BusinessID:      "biz-1",
		Config:          validReviewConfig(),
		ExtractSubjects: true,
		Wait:            true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := StringValue(result.Final, "status"); status != "success" {
		t.Fatalf("result=%#v", result)
	}
	// Workflow 必须原样保留主体候选的 name/role，避免 Agent 只能取得角色而缺少公司名称。
	counterparts, ok := result.Subjects["counterparts"].([]any)
	if !ok || len(counterparts) != 1 {
		t.Fatalf("subjects=%#v，期望一个结构化主体候选", result.Subjects)
	}
	firstCounterpart, ok := counterparts[0].(map[string]any)
	if !ok || firstCounterpart["name"] != "中北大学" || firstCounterpart["role"] != "甲方" {
		t.Fatalf("subjects=%#v，主体候选必须保留同一组 name/role", result.Subjects)
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
		if query.VisibilityScope != "" || query.AppType != "" {
			t.Fatalf("query=%#v，review run 不应注入 appType 或 contractResult", query)
		}
	}
}

// TestWorkflowWaitForResultPollsThenLoadsInfo 验证已有任务的结果流程只轮询状态，并在成功后加载一次详情。
// 该测试覆盖从 Run 中抽取的共享结果编排，属于行为回归验证，不单独计为 RED。
func TestWorkflowWaitForResultPollsThenLoadsInfo(t *testing.T) {
	api := &fakeAPI{statuses: []Document{{"status": "running"}, {"status": "success"}}}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	result, err := workflow.WaitForResult(context.Background(), TaskQuery{
		TaskID:          88,
		BusinessID:      "biz-1",
		AppType:         AppTypeCLM,
		VisibilityScope: VisibilityScopeContractResult,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := StringValue(result, "status"); status != "success" {
		t.Fatalf("result=%#v，期望返回最终详情", result)
	}
	expected := []string{"status", "status", "info"}
	if len(api.calls) != len(expected) {
		t.Fatalf("calls=%#v，期望 status -> status -> info", api.calls)
	}
	for index := range expected {
		if api.calls[index] != expected[index] {
			t.Fatalf("calls=%#v，期望 status -> status -> info", api.calls)
		}
	}
	for _, query := range api.queries {
		if query.VisibilityScope != VisibilityScopeContractResult || query.BusinessID != "biz-1" || query.AppType != AppTypeCLM {
			t.Fatalf("query=%#v，详情查询必须复用完整任务上下文", query)
		}
	}
}

// TestWorkflowWaitForResultAllowsMissingDetailLink 验证 OAuth 详情没有预览链接时仍保留成功详情。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；成功语义、详情内容或可选链接字段不符合预期时通过 t.Fatal 报告。
func TestWorkflowWaitForResultAllowsMissingDetailLink(t *testing.T) {
	api := &fakeAPI{
		statuses: []Document{{"taskId": int64(88), "status": "success"}},
		info:     Document{"taskId": int64(88), "status": "success", "result": []any{"risk-card"}},
	}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	result, err := workflow.WaitForResult(context.Background(), TaskQuery{TaskID: 88})
	if err != nil {
		t.Fatalf("err=%v，OAuth 详情缺少预览链接时仍应成功", err)
	}
	if status, _ := StringValue(result, "status"); status != "success" || len(result["result"].([]any)) != 1 {
		t.Fatalf("result=%#v，缺少链接时应返回完整详情", result)
	}
	if _, exists := result["reviewDetailUrl"]; exists {
		t.Fatalf("result=%#v，后端未返回链接时不应伪造 reviewDetailUrl", result)
	}
}

// TestRunSpecRequiresReviewConfig 验证 review-run Schema 要求发起审查配置完整。
func TestRunSpecRequiresReviewConfig(t *testing.T) {
	spec := RunSpec{
		Source: RunSource{Type: "file", Path: "contract.pdf", Name: "合同.pdf"},
		Config: map[string]any{},
	}
	if err := spec.Validate(); err == nil || !strings.Contains(err.Error(), "selectedPosition") {
		t.Fatalf("err=%v", err)
	}
}

// TestRunSpecURLRejectsInvalidFileHash 验证 review-run Schema 不接受非 SHA-256 指纹。
func TestRunSpecURLRejectsInvalidFileHash(t *testing.T) {
	spec := RunSpec{
		Source:     RunSource{Type: "url", FileURL: "https://files.example.com/contract.pdf", Name: "合同.pdf"},
		BusinessID: "biz-url",
		FileHash:   "not-a-sha256",
		Config:     validReviewConfig(),
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
		Source: RunSource{Type: "file", Path: "contract.pdf", Name: "合同.pdf"},
		Config: validReviewConfig(),
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
		Config:     validReviewConfig(),
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
		Config:     validReviewConfig(),
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

// TestWorkflowRunURLUsesUploadedFileIdentity 验证 URL 一键审查直接使用 V3 上传响应继续链路。
func TestWorkflowRunURLUsesUploadedFileIdentity(t *testing.T) {
	api := &urlUploadAPI{}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	result, err := workflow.Run(context.Background(), RunSpec{
		Source: RunSource{Type: "url", FileURL: "https://files.example.com/contract.pdf", Name: "合同.pdf"},
		Config: validReviewConfig(),
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

// TestWorkflowRunURLRejectsIncompleteUploadIdentity 验证异常 V3 响应缺少业务身份时停止发起审查。
func TestWorkflowRunURLRejectsIncompleteUploadIdentity(t *testing.T) {
	api := &missingURLIdentityAPI{}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	_, err := workflow.Run(context.Background(), RunSpec{
		Source: RunSource{Type: "url", FileURL: "https://files.example.com/contract.pdf", Name: "合同.pdf"},
		Config: validReviewConfig(),
	})
	if err == nil || !strings.Contains(err.Error(), "V3 URL 上传响应缺少 businessId 或 fileHash") {
		t.Fatalf("err=%v", err)
	}
	if len(api.calls) != 1 || api.calls[0] != "upload-url" {
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
		Config:     validReviewConfig(),
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
