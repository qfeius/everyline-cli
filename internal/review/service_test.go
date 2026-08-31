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
	"strings"
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
	if err := contracts.ValidateRequest(request.OperationID, request.Method, request.Path, request.ContractInput); err != nil {
		return openplatform.Response{}, err
	}
	return openplatform.Response{Data: json.RawMessage(client.data)}, nil
}

// TestServiceStartContract 验证 startReview 的 operation、method、path 和必填 JSON 字段。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestServiceStartContract(t *testing.T) {
	client := &recordingClient{data: `{"taskId":88,"status":"running"}`}
	service := NewService(client, 5*time.Second)
	request := StartRequest{
		BusinessID: "biz-1",
		FileID:     11,
		FileHash:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Config: map[string]any{
			"selectedPosition":             "xxx公司",
			"selectedAuditRole":            "甲方",
			"reviewStrength":               1,
			"matchContractTypeRulePackage": true,
		},
	}
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
	if body["businessId"] != "biz-1" || body["fileId"] != "11" || body["fileHash"] != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("body=%#v", body)
	}
	usageReportContext, ok := body["usageReportContext"].(map[string]any)
	if !ok || usageReportContext["reportBusinessCode"] != usageReportBusinessCodeEverylineCLI {
		t.Fatalf("body=%#v，usageReportContext.reportBusinessCode 应为固定 CLI 业务编码", body)
	}
	if _, exists := body["appType"]; exists {
		t.Fatalf("body=%#v，不应发送 appType", body)
	}
}

// TestServiceStartRequiresReviewConfig 验证 startReview 缺少 API 必填 config 时拒绝发送请求。
func TestServiceStartRequiresReviewConfig(t *testing.T) {
	client := &recordingClient{data: `{"taskId":88,"status":"running"}`}
	service := NewService(client, 5*time.Second)
	request := StartRequest{BusinessID: "biz-1", FileID: 11, FileHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if _, err := service.Start(context.Background(), request); err == nil || !strings.Contains(err.Error(), "selectedPosition") {
		t.Fatalf("err=%v", err)
	}
	if client.request.OperationID != "" {
		t.Fatalf("缺少 config 时不应发送请求: %#v", client.request)
	}
}

// TestServiceExtractSubjectsUsesStringFileID 验证主体提取请求按后端契约发送字符串 fileId。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；请求字段类型不匹配时通过 t.Fatal 报告。
func TestServiceExtractSubjectsUsesStringFileID(t *testing.T) {
	client := &recordingClient{data: `{"subjects":[]}`}
	service := NewService(client, time.Second)
	if _, err := service.ExtractSubjects(context.Background(), StartRequest{BusinessID: "biz-1", AppType: AppTypeThirdParty, FileID: 12}); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(client.request.Body, &body); err != nil {
		t.Fatal(err)
	}
	if fileID, ok := body["fileId"].(string); !ok || fileID != "12" {
		t.Fatalf("body=%#v，fileId 应为字符串 12", body)
	}
}

// TestServiceUploadURLUsesV3Contract 验证 URL 上传调用 V3 地址并发送 fileUrl/name。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；路径或 JSON 字段回退到旧 V1 契约时通过 t.Fatal 报告。
func TestServiceUploadURLUsesV3Contract(t *testing.T) {
	client := &recordingClient{data: `{"fileId":"12","businessId":"biz-url","fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`}
	service := NewService(client, time.Second)
	if _, err := service.UploadURL(context.Background(), "https://files.example.com/contract.pdf", "合同.pdf"); err != nil {
		t.Fatal(err)
	}
	if client.request.Path != "/open-apis/contract-review/v3/file/contract/uploadByUrl" {
		t.Fatalf("path=%q", client.request.Path)
	}
	var body map[string]any
	if err := json.Unmarshal(client.request.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["fileUrl"] != "https://files.example.com/contract.pdf" || body["name"] != "合同.pdf" {
		t.Fatalf("body=%#v", body)
	}
	if _, exists := body["fileName"]; exists {
		t.Fatalf("body=%#v，不应发送旧 V1 字段 fileName", body)
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
			name: "subjects", operationID: OperationExtractSubjects, method: "POST", path: pathExtractSubjects,
			invoke: func(service *Service) error {
				_, err := service.ExtractSubjects(context.Background(), StartRequest{BusinessID: "biz-1", AppType: AppTypeThirdParty, FileID: 12})
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
				_, err := service.Info(context.Background(), TaskQuery{TaskID: 88, BusinessID: "biz-1"})
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

// TestServiceTaskQueryExtensions 验证 status 查询会透传后端新增的 visibilityScope。
func TestServiceTaskQueryExtensions(t *testing.T) {
	client := &recordingClient{data: `{"status":"running"}`}
	service := NewService(client, time.Second)
	if _, err := service.Status(context.Background(), TaskQuery{TaskID: 88, BusinessID: "  biz-1  ", AppType: " CLM ", VisibilityScope: " contractResult "}); err != nil {
		t.Fatal(err)
	}
	if got := client.request.Query.Get("businessId"); got != "biz-1" {
		t.Fatalf("businessId=%q", got)
	}
	if got := client.request.Query.Get("visibilityScope"); got != "contractResult" {
		t.Fatalf("visibilityScope=%q", got)
	}
	if got := client.request.Query.Get("appType"); got != "CLM" {
		t.Fatalf("appType=%q", got)
	}
}

// TestServiceTaskQueryAllowsContractResultWithoutAppType 验证 contractResult 查询只依赖业务对象，不再要求 appType。
func TestServiceTaskQueryAllowsContractResultWithoutAppType(t *testing.T) {
	client := &recordingClient{data: `{"status":"running"}`}
	service := NewService(client, time.Second)
	if _, err := service.Status(context.Background(), TaskQuery{TaskID: 88, BusinessID: "biz-1", VisibilityScope: VisibilityScopeContractResult}); err != nil {
		t.Fatal(err)
	}
	if got := client.request.Query.Get("appType"); got != "" {
		t.Fatalf("appType=%q，不应发送 appType", got)
	}
}

// TestTaskQueriesAllowMissingBusinessID 验证后端允许仅用 taskId 查询状态和详情。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestTaskQueriesAllowMissingBusinessID(t *testing.T) {
	for _, invoke := range []func(*Service) error{
		func(service *Service) error {
			_, err := service.Status(context.Background(), TaskQuery{TaskID: 88})
			return err
		},
		func(service *Service) error {
			_, err := service.Info(context.Background(), TaskQuery{TaskID: 88})
			return err
		},
	} {
		client := &recordingClient{data: `{"ok":true}`}
		err := invoke(NewService(client, time.Second))
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		if client.request.Query.Get("taskId") != "88" || client.request.Query.Get("businessId") != "" {
			t.Fatalf("query=%#v", client.request.Query)
		}
	}
}

// TestTaskQueryRejectsUnknownVisibilityScope 验证可见性范围只接受后端当前枚举值。
func TestTaskQueryRejectsUnknownVisibilityScope(t *testing.T) {
	client := &recordingClient{data: `{"ok":true}`}
	_, err := NewService(client, time.Second).Status(context.Background(), TaskQuery{TaskID: 88, VisibilityScope: "all"})
	if err == nil || !strings.Contains(err.Error(), "visibility-scope") {
		t.Fatalf("err=%v", err)
	}
	if client.request.OperationID != "" {
		t.Fatalf("非法 visibilityScope 不应调用 HTTP client: %#v", client.request)
	}
}

// TestTaskQueryRequiresContextForContractResult 验证业务对象可见性范围不会在缺少上下文时发出无效查询。
func TestTaskQueryRequiresContextForContractResult(t *testing.T) {
	client := &recordingClient{data: `{"ok":true}`}
	_, err := NewService(client, time.Second).Status(context.Background(), TaskQuery{TaskID: 88, VisibilityScope: VisibilityScopeContractResult})
	if err == nil || !strings.Contains(err.Error(), "必须提供 business-id") {
		t.Fatalf("err=%v", err)
	}
	if client.request.OperationID != "" {
		t.Fatalf("上下文不完整时不应调用 HTTP client: %#v", client.request)
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
	if client.request.Timeout != time.Second {
		t.Fatalf("upload timeout=%s，期望显式配置的 1s", client.request.Timeout)
	}
}

// TestServiceUploadCLMAllowsMissingBusinessID 验证 appType 非必填时 CLM 上传不再被本地业务上下文校验拦截。
func TestServiceUploadCLMAllowsMissingBusinessID(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "contract.pdf")
	if err := os.WriteFile(filePath, []byte("pdf-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingClient{data: `{"fileId":"12","businessId":"biz-1","fileHash":"hash"}`}
	service := NewService(client, time.Second)
	if _, err := service.UploadFile(context.Background(), filePath, "合同.pdf", AppTypeCLM, " "); err != nil {
		t.Fatal(err)
	}
	if client.request.OperationID != OperationUploadFile {
		t.Fatalf("应发送上传请求: %#v", client.request)
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

// TestValidateUploadFileAllowsExtensionlessPath 验证文件路径扩展名不参与校验，业务文件名仍由 UploadFile 单独验证。
func TestValidateUploadFileAllowsExtensionlessPath(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "upload")
	if err := os.WriteFile(filePath, []byte("pdf-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateUploadFile(filePath); err != nil {
		t.Fatalf("无扩展名临时路径应允许上传: %v", err)
	}
}
