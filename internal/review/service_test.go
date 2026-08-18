package review

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/contracts"
	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
)

// recordingClient 记录 Service 生成的 HTTP Adapter 请求。
type recordingClient struct {
	request openplatform.Request
	data    string
}

// Do 保存请求并返回测试指定的 envelope.data。
// 入参：context.Context 控制取消；request openplatform.Request 为待记录契约。
// 返回值：openplatform.Response 为固定 data；error 在请求偏离契约目录时非 nil。
func (client *recordingClient) Do(_ context.Context, request openplatform.Request) (openplatform.Response, error) {
	client.request = request
	if err := contracts.ValidateRequest(request.OperationID, request.Method, request.Path); err != nil {
		return openplatform.Response{}, err
	}
	return openplatform.Response{Data: json.RawMessage(client.data)}, nil
}

// TestServiceStartContract 验证 startReview 的 operation、method、path 和 JSON 字段。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestServiceStartContract(t *testing.T) {
	client := &recordingClient{data: `{"taskId":88,"status":"running"}`}
	service := NewService(client, 5*time.Second)
	request := StartRequest{BusinessID: "biz-1", AppType: AppTypeThirdParty, FileID: 11, FileHash: "sha256", Config: map[string]any{}, TriggerScene: "manual"}
	if _, err := service.Start(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if client.request.OperationID != OperationStartReview || client.request.Method != "POST" || client.request.Path != pathStartReview {
		t.Fatalf("request=%#v", client.request)
	}
	var body map[string]any
	if err := json.Unmarshal(client.request.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["businessId"] != "biz-1" || body["appType"] != AppTypeThirdParty || body["fileHash"] != "sha256" {
		t.Fatalf("body=%#v", body)
	}
}

// TestServiceStartFeishuContract 验证飞书 V3 路径和签名额度字段完整透传。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestServiceStartFeishuContract(t *testing.T) {
	client := &recordingClient{data: `{"taskId":89,"status":"running"}`}
	service := NewService(client, 5*time.Second)
	request := FeishuStartRequest{
		StartRequest: StartRequest{BusinessID: "biz-1", AppType: AppTypeThirdParty, FileID: 11, FileHash: "sha256", Config: map[string]any{}},
		HasQuota:     true, BaseSignature: "payload.signature", PackID: "pack-1",
	}
	if _, err := service.StartFeishu(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if client.request.OperationID != OperationStartFeishu || client.request.Method != "POST" || client.request.Path != pathStartFeishu {
		t.Fatalf("request=%#v", client.request)
	}
	var body map[string]any
	if err := json.Unmarshal(client.request.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["hasQuota"] != true || body["baseSignature"] != "payload.signature" || body["packID"] != "pack-1" {
		t.Fatalf("body=%#v", body)
	}
}

// TestServiceOperationMappings 验证其余 MVP operation ID、method 和 path 不发生契约漂移。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestServiceOperationMappings(t *testing.T) {
	tests := []struct {
		name        string
		operationID string
		method      string
		path        string
		invoke      func(*Service) error
	}{
		{
			name: "upload-url", operationID: OperationUploadFileURL, method: "POST", path: pathUploadFileURL,
			invoke: func(service *Service) error {
				_, err := service.UploadURL(context.Background(), "https://files.example.com/contract.pdf", "合同.pdf")
				return err
			},
		},
		{
			name: "snapshot", operationID: OperationFileSnapshot, method: "GET", path: pathFileSnapshot,
			invoke: func(service *Service) error {
				_, err := service.Snapshot(context.Background(), 12, AppTypeThirdParty, "biz-1")
				return err
			},
		},
		{
			name: "subjects", operationID: OperationExtractSubjects, method: "POST", path: pathExtractSubjects,
			invoke: func(service *Service) error {
				_, err := service.ExtractSubjects(context.Background(), StartRequest{BusinessID: "biz-1", AppType: AppTypeThirdParty, FileID: 12, FileHash: "hash"})
				return err
			},
		},
		{
			name: "status", operationID: OperationTaskStatus, method: "GET", path: pathTaskStatus,
			invoke: func(service *Service) error {
				_, err := service.Status(context.Background(), TaskQuery{TaskID: 88, BusinessID: "biz-1", AppType: AppTypeThirdParty})
				return err
			},
		},
		{
			name: "info", operationID: OperationTaskInfo, method: "GET", path: pathTaskInfo,
			invoke: func(service *Service) error {
				_, err := service.Info(context.Background(), TaskQuery{TaskID: 88})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingClient{data: `{"ok":true}`}
			if err := test.invoke(NewService(client, time.Second)); err != nil {
				t.Fatal(err)
			}
			if client.request.OperationID != test.operationID || client.request.Method != test.method || client.request.Path != test.path {
				t.Fatalf("request=%#v", client.request)
			}
		})
	}
}

// TestServiceUploadMultipartContract 验证本地上传 multipart 的 file/name/appType/businessId。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestServiceUploadMultipartContract(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "CONTRACT.PDF")
	if err := os.WriteFile(filePath, []byte("pdf-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingClient{data: `{"fileId":"12","businessId":"biz-1","fileHash":"hash"}`}
	service := NewService(client, time.Second)
	if _, err := service.UploadFile(context.Background(), filePath, "采购合同.PDF", AppTypeThirdParty, "biz-1"); err != nil {
		t.Fatal(err)
	}
	mediaType, parameters, err := mime.ParseMediaType(client.request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("content-type=%q err=%v", client.request.Header.Get("Content-Type"), err)
	}
	reader := multipart.NewReader(bytes.NewReader(client.request.Body), parameters["boundary"])
	fields := map[string]string{}
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		content, readErr := io.ReadAll(part)
		if readErr != nil {
			t.Fatal(readErr)
		}
		fields[part.FormName()] = string(content)
	}
	if fields["file"] != "pdf-content" || fields["name"] != "采购合同.PDF" || fields["appType"] != AppTypeThirdParty || fields["businessId"] != "biz-1" {
		t.Fatalf("fields=%#v", fields)
	}
}

// TestValidateUploadFileBoundary 验证 2 MiB 等于上限时允许，超过一个字节时拒绝。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestValidateUploadFileBoundary(t *testing.T) {
	directory := t.TempDir()
	allowedPath := filepath.Join(directory, "allowed.docx")
	if err := os.WriteFile(allowedPath, make([]byte, MaxUploadBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateUploadFile(allowedPath); err != nil {
		t.Fatalf("等于上限应允许: %v", err)
	}
	oversizedPath := filepath.Join(directory, "oversized.pdf")
	if err := os.WriteFile(oversizedPath, make([]byte, MaxUploadBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateUploadFile(oversizedPath); err == nil {
		t.Fatal("超过上限应拒绝")
	}
}
