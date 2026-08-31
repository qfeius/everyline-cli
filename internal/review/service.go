package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
)

const (
	OperationUploadFile      = "uploadContractFileV3"
	OperationUploadFileURL   = "uploadContractFileByURLV3"
	OperationExtractSubjects = "smartAuditContractSubjects"
	OperationStartReview     = "smartAuditTaskStartReview"
	OperationTaskStatus      = "smartAuditTaskStatus"
	OperationTaskInfo        = "smartAuditTaskInfo"

	pathUploadFile      = "/open-apis/contract-review/v3/file/contract/upload"
	pathUploadFileURL   = "/open-apis/contract-review/v3/file/contract/uploadByUrl"
	pathExtractSubjects = "/open-apis/contract-review/v3/smartAudit/contract/subjects"
	pathStartReview     = "/open-apis/contract-review/v3/smartAudit/task/startReview"
	pathTaskStatus      = "/open-apis/contract-review/v3/smartAudit/task/status"
	pathTaskInfo        = "/open-apis/contract-review/v3/smartAudit/task/info"

	MaxUploadBytes = 2 * 1024 * 1024
)

var allowedExtensions = map[string]struct{}{`.doc`: {}, `.docx`: {}, `.pdf`: {}}

// Client 是 review service 依赖的受控 HTTP Adapter 边界。
type Client interface {
	Do(context.Context, openplatform.Request) (openplatform.Response, error)
}

// API 描述工作流依赖的领域操作，测试可使用内存 Adapter。
type API interface {
	UploadFile(context.Context, string, string, string, string) (Document, error)
	UploadURL(context.Context, string, string) (Document, error)
	ExtractSubjects(context.Context, StartRequest) (Document, error)
	Start(context.Context, StartRequest) (Document, error)
	Status(context.Context, TaskQuery) (Document, error)
	Info(context.Context, TaskQuery) (Document, error)
}

// Service 将每个审查 operation ID 映射到唯一 method/path/body 契约。
type Service struct {
	client  Client
	timeout time.Duration
}

// NewService 创建审查领域服务。
// 入参：client Client 为 HTTP Adapter；timeout time.Duration 为普通远端请求超时。
// 返回值：*Service，可执行文件和任务操作。
func NewService(client Client, timeout time.Duration) *Service {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Service{client: client, timeout: timeout}
}

// UploadFile 校验并上传本地合同文件，multipart 字段严格使用 file/name/appType/businessId。
// 入参：ctx context.Context；filePath string 为本地路径；name string 为业务文件名；appType string 为接入类型；businessID string 为可选业务 ID。
// 返回值：Document 为上传结果；error 为校验、读取或 API 失败。
func (service *Service) UploadFile(ctx context.Context, filePath string, name string, appType string, businessID string) (Document, error) {
	if err := ValidateUploadFile(filePath); err != nil {
		return nil, err
	}
	if err := ValidateFileName(name); err != nil {
		return nil, err
	}
	businessID = strings.TrimSpace(businessID)
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取上传文件: %w", err)
	}
	if len(content) > MaxUploadBytes {
		return nil, fmt.Errorf("文件大小不能超过 %d 字节（2 MiB）", MaxUploadBytes)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	filePart, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("创建 multipart 文件字段: %w", err)
	}
	if _, err := filePart.Write(content); err != nil {
		return nil, fmt.Errorf("写入 multipart 文件字段: %w", err)
	}
	if err := writer.WriteField("name", name); err != nil {
		return nil, fmt.Errorf("写入 name 字段: %w", err)
	}
	if strings.TrimSpace(appType) != "" {
		if err := writer.WriteField("appType", appType); err != nil {
			return nil, fmt.Errorf("写入 appType 字段: %w", err)
		}
	}
	if businessID != "" {
		if err := writer.WriteField("businessId", businessID); err != nil {
			return nil, fmt.Errorf("写入 businessId 字段: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("结束 multipart 请求: %w", err)
	}
	response, err := service.client.Do(ctx, openplatform.Request{
		OperationID:   OperationUploadFile,
		Method:        http.MethodPost,
		Path:          pathUploadFile,
		ContractInput: uploadFileContractInput(filePath, name, appType, businessID),
		Header:        http.Header{"Content-Type": []string{writer.FormDataContentType()}},
		Body:          body.Bytes(),
		Timeout:       service.timeout,
		SuccessCode:   200,
	})
	if err != nil {
		return nil, err
	}
	return DecodeDocument(response.Data)
}

// UploadURL 通过远端 URL 上传合同，JSON 字段严格使用 V3 契约的 fileUrl/name。
// 入参：ctx context.Context；fileURL string 为 http/https 地址；name string 为文件名。
// 返回值：Document 为上传结果；error 为输入或 API 失败。
func (service *Service) UploadURL(ctx context.Context, fileURL string, name string) (Document, error) {
	parsed, err := url.ParseRequestURI(fileURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("file-url 必须是完整的 http/https URL")
	}
	if err := ValidateFileName(name); err != nil {
		return nil, err
	}
	input := map[string]string{"fileUrl": fileURL, "name": name}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("编码 URL 上传请求: %w", err)
	}
	return service.doJSON(ctx, OperationUploadFileURL, http.MethodPost, pathUploadFileURL, nil, body, input)
}

// ExtractSubjects 无副作用提取合同主体，复用 start 请求中的文件身份字段。
// 入参：ctx context.Context；request StartRequest 提供 businessId/appType/fileId，fileHash 可选。
// 返回值：Document 为主体候选；error 为输入或 API 失败。
// 关键约束：主体提取接口要求 fileId 以 JSON 字符串传输，因此在请求边界完成类型转换。
func (service *Service) ExtractSubjects(ctx context.Context, request StartRequest) (Document, error) {
	request = request.Normalize()
	if err := ValidateSubjectIdentity(request); err != nil {
		return nil, err
	}
	input := map[string]any{
		"businessId": request.BusinessID,
		// 主体提取接口的 fileId 契约是字符串；StartRequest 内部仍保留 int64 便于复用校验和响应解析。
		"fileId": strconv.FormatInt(request.FileID, 10),
	}
	if strings.TrimSpace(request.AppType) != "" {
		input["appType"] = request.AppType
	}
	if strings.TrimSpace(request.FileHash) != "" {
		input["fileHash"] = request.FileHash
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("编码主体提取请求: %w", err)
	}
	return service.doJSON(ctx, OperationExtractSubjects, http.MethodPost, pathExtractSubjects, nil, body, input)
}

// Start 发起普通 V3 智审任务，完整透传冻结的 typed request。
// 入参：ctx context.Context；request StartRequest 为已定义字段的发起请求。
// 返回值：Document 为任务快照；error 为校验或 API 失败。
func (service *Service) Start(ctx context.Context, request StartRequest) (Document, error) {
	request = request.Normalize()
	if err := request.Validate(); err != nil {
		return nil, err
	}
	// 在 HTTP 边界使用与后端一致的字符串 fileId，避免内部 int64 类型直接泄漏到 JSON 请求体。
	payload := request.ContractPayload()
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("编码发起审查请求: %w", err)
	}
	return service.doJSON(ctx, OperationStartReview, http.MethodPost, pathStartReview, nil, body, payload)
}

// Status 查询轻量任务快照，供轮询器使用。
// 入参：ctx context.Context；query TaskQuery 为 taskId 和可选业务上下文。
// 返回值：Document 为状态快照；error 为输入或 API 失败。
func (service *Service) Status(ctx context.Context, query TaskQuery) (Document, error) {
	query = normalizeTaskQuery(query)
	values, err := taskQueryValues(query)
	if err != nil {
		return nil, err
	}
	return service.doJSON(ctx, OperationTaskStatus, http.MethodGet, pathTaskStatus, values, nil, query)
}

// Info 查询终态或调试用任务详情，并保留所有扩展展示字段。
// 入参：ctx context.Context；query TaskQuery 为 taskId 和可选业务上下文。
// 返回值：Document 为完整详情；error 为输入或 API 失败。
func (service *Service) Info(ctx context.Context, query TaskQuery) (Document, error) {
	query = normalizeTaskQuery(query)
	values, err := taskQueryValues(query)
	if err != nil {
		return nil, err
	}
	return service.doJSON(ctx, OperationTaskInfo, http.MethodGet, pathTaskInfo, values, nil, query)
}

// doJSON 执行 JSON 或 GET 操作，并解开已验证的业务 data。
// 入参：ctx context.Context；operationID/method/path string 定义契约；query url.Values 为查询；body []byte 为 JSON 请求体；contractInput any 为 Schema 输入。
// 返回值：Document 为业务 data；error 为 HTTP Adapter 或解码失败。
func (service *Service) doJSON(ctx context.Context, operationID string, method string, path string, query url.Values, body []byte, contractInput any) (Document, error) {
	header := http.Header{}
	if body != nil {
		header.Set("Content-Type", "application/json")
	}
	response, err := service.client.Do(ctx, openplatform.Request{
		OperationID:   operationID,
		Method:        method,
		Path:          path,
		ContractInput: contractInput,
		Query:         query,
		Header:        header,
		Body:          body,
		Timeout:       service.timeout,
		SuccessCode:   200,
	})
	if err != nil {
		return nil, err
	}
	return DecodeDocument(response.Data)
}

// ValidateUploadFile 校验本地文件路径、存在性、普通文件类型和 2 MiB 上限。
// 入参：filePath string 为本地文件路径。
// 返回值：error，文件可上传时为 nil。
func ValidateUploadFile(filePath string) error {
	if strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("file 不能为空")
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("读取文件信息: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("file 必须是普通文件")
	}
	if info.Size() > MaxUploadBytes {
		return fmt.Errorf("文件大小不能超过 %d 字节（2 MiB）", MaxUploadBytes)
	}
	return nil
}

// ValidateFileName 将扩展名转换为小写后，只开放完整审查链路稳定支持的三种格式。
// 入参：name string 为业务文件名。
// 返回值：error，扩展名为 .doc、.docx 或 .pdf 时为 nil。
func ValidateFileName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name 不能为空")
	}
	extension := strings.ToLower(filepath.Ext(name))
	if _, allowed := allowedExtensions[extension]; !allowed {
		return fmt.Errorf("仅支持 .doc、.docx 和 .pdf 文件")
	}
	return nil
}

// taskQueryValues 校验任务查询并构造稳定 query 参数。
// 入参：query TaskQuery 为任务和业务上下文。
// 返回值：url.Values 为请求参数；error 为校验失败。
func taskQueryValues(query TaskQuery) (url.Values, error) {
	query = normalizeTaskQuery(query)
	if query.TaskID <= 0 {
		return nil, fmt.Errorf("task-id 必须大于 0")
	}
	query.BusinessID = strings.TrimSpace(query.BusinessID)
	values := url.Values{"taskId": []string{strconv.FormatInt(query.TaskID, 10)}}
	if query.BusinessID != "" {
		values.Set("businessId", query.BusinessID)
	}
	if query.AppType != "" {
		values.Set("appType", query.AppType)
	}
	if query.VisibilityScope != "" {
		if query.VisibilityScope != VisibilityScopeContractResult {
			return nil, fmt.Errorf("visibility-scope 必须是 %s", VisibilityScopeContractResult)
		}
		if query.BusinessID == "" {
			return nil, fmt.Errorf("visibility-scope=%s 必须提供 business-id", VisibilityScopeContractResult)
		}
		values.Set("visibilityScope", query.VisibilityScope)
	}
	return values, nil
}

// normalizeTaskQuery 统一清理可选查询上下文，保证 URL query 与契约输入使用同一组值。
func normalizeTaskQuery(query TaskQuery) TaskQuery {
	query.BusinessID = strings.TrimSpace(query.BusinessID)
	query.AppType = strings.TrimSpace(query.AppType)
	query.VisibilityScope = strings.TrimSpace(query.VisibilityScope)
	return query
}

// uploadFileContractInput 构造本地上传对应的逻辑 Schema 输入，不暴露 multipart 二进制内容。
// 入参：filePath/name/appType/businessID string 分别为本地文件、业务文件名、应用类型和可选业务 ID。
// 返回值：map[string]any，为 review-upload.schema.json 对应的字段对象。
func uploadFileContractInput(filePath string, name string, appType string, businessID string) map[string]any {
	input := map[string]any{"file": filePath, "name": name}
	if strings.TrimSpace(appType) != "" {
		input["appType"] = appType
	}
	if businessID != "" {
		input["businessId"] = businessID
	}
	return input
}
