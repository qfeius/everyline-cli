package review

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeAPI 模拟上传、发起、两次状态轮询和详情返回。
type fakeAPI struct {
	statuses []Document
	index    int
	calls    []string
}

// UploadFile 返回带完整文件身份的上传结果。
// 入参：context 和字符串参数仅满足 API。
// 返回值：Document 为上传结果；error 为 nil。
func (api *fakeAPI) UploadFile(context.Context, string, string, string, string) (Document, error) {
	api.calls = append(api.calls, "upload")
	return Document{"fileId": int64(11), "businessId": "biz-1", "fileHash": "hash-1"}, nil
}

// UploadURL 在本测试中不应调用。
// 入参：context 和字符串参数仅满足 API。
// 返回值：Document 为空；error 为 nil。
func (api *fakeAPI) UploadURL(context.Context, string, string) (Document, error) {
	api.calls = append(api.calls, "upload-url")
	return Document{}, nil
}

// Snapshot 在上传已返回 fileHash 时不应调用。
// 入参：context、fileID、appType、businessID 仅满足 API。
// 返回值：Document 为快照；error 为 nil。
func (api *fakeAPI) Snapshot(context.Context, int64, string, string) (Document, error) {
	api.calls = append(api.calls, "snapshot")
	return Document{"fileHash": "hash-snapshot"}, nil
}

// ExtractSubjects 返回一个主体候选。
// 入参：context 和 StartRequest 仅满足 API。
// 返回值：Document 为主体候选；error 为 nil。
func (api *fakeAPI) ExtractSubjects(context.Context, StartRequest) (Document, error) {
	api.calls = append(api.calls, "subjects")
	return Document{"counterparts": []any{"甲方"}}, nil
}

// Start 返回 running 任务快照。
// 入参：context 和 StartRequest 仅满足 API。
// 返回值：Document 为任务快照；error 为 nil。
func (api *fakeAPI) Start(context.Context, StartRequest) (Document, error) {
	api.calls = append(api.calls, "start")
	return Document{"taskId": int64(88), "status": "running"}, nil
}

// Status 按顺序返回预设状态。
// 入参：context 和 TaskQuery 仅满足 API。
// 返回值：Document 为当前状态；error 为 nil。
func (api *fakeAPI) Status(context.Context, TaskQuery) (Document, error) {
	api.calls = append(api.calls, "status")
	status := api.statuses[api.index]
	api.index++
	return status, nil
}

// Info 返回最终展示详情。
// 入参：context 和 TaskQuery 仅满足 API。
// 返回值：Document 为最终详情；error 为 nil。
func (api *fakeAPI) Info(context.Context, TaskQuery) (Document, error) {
	api.calls = append(api.calls, "info")
	return Document{"taskId": int64(88), "status": "success", "result": []any{}}, nil
}

// instantClock 让轮询测试无需真实等待。
type instantClock struct{}

// After 立即返回，不阻塞测试。
// 入参：context 和 delay 仅满足 Clock。
// 返回值：error 始终为 nil。
func (instantClock) After(context.Context, time.Duration) error { return nil }

// TestWorkflowRun 验证上传、主体提取、发起、轮询、详情的固定顺序。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestWorkflowRun(t *testing.T) {
	api := &fakeAPI{statuses: []Document{{"status": "running"}, {"status": "success"}}}
	workflow := NewWorkflow(api, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	result, err := workflow.Run(context.Background(), RunSpec{
		Source:          RunSource{Type: "file", Path: "contract.pdf", Name: "合同.pdf"},
		BusinessID:      "biz-1",
		AppType:         AppTypeThirdParty,
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
