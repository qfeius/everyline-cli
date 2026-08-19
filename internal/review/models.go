package review

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"git.qtech.cn/ai/everyline-cli/internal/contracts"
)

const (
	AppTypeCLM                    = "CLM"
	AppTypeCR                     = "CR"
	AppTypeThirdParty             = "THIRD_PARTY"
	VisibilityScopeContractResult = "contractResult"
)

// Document 保存平台 data 对象并保留未来新增字段，避免 CLI 因响应扩展而丢数据。
type Document map[string]any

// StartRequest 对应 operation smartAuditTaskStartReview 的冻结请求契约。
type StartRequest struct {
	BusinessID                     string         `json:"businessId" yaml:"businessId"`
	AppType                        string         `json:"appType" yaml:"appType"`
	FileID                         int64          `json:"fileId" yaml:"fileId"`
	FileHash                       string         `json:"fileHash" yaml:"fileHash"`
	Config                         map[string]any `json:"config" yaml:"config"`
	AllowInvalidSelectedChecklists bool           `json:"allowInvalidSelectedChecklists,omitempty" yaml:"allowInvalidSelectedChecklists,omitempty"`
	TriggerScene                   string         `json:"triggerScene,omitempty" yaml:"triggerScene,omitempty"`
	UsageReportContext             map[string]any `json:"usageReportContext,omitempty" yaml:"usageReportContext,omitempty"`
}

// Validate 校验普通发起审查的必填字段、应用类型和触发场景。
// 入参：无，接收者 StartRequest 为待校验请求。
// 返回值：error，请求满足后端 V3 契约时为 nil。
func (request StartRequest) Validate() error {
	if err := ValidateReviewIdentity(request); err != nil {
		return err
	}
	if request.TriggerScene != "" && request.TriggerScene != "manual" && request.TriggerScene != "auto" {
		return fmt.Errorf("triggerScene 必须是 manual 或 auto")
	}
	return nil
}

// Normalize 将后端默认的空配置显式编码为对象，保证 dry-run 和真实请求一致。
func (request StartRequest) Normalize() StartRequest {
	request.BusinessID = strings.TrimSpace(request.BusinessID)
	request.FileHash = normalizeSHA256(request.FileHash)
	if request.Config == nil {
		request.Config = map[string]any{}
	}
	return request
}

// FeishuStartRequest 对应字段捷径入口 /open-api/feishu/v1/smartAudit/init 的请求契约。
type FeishuStartRequest struct {
	FileID                 int64  `json:"fileId" yaml:"fileId"`
	ReviewStrength         *int   `json:"reviewStrength,omitempty" yaml:"reviewStrength,omitempty"`
	AuditerName            string `json:"auditerName,omitempty" yaml:"auditerName,omitempty"`
	SelectedPosition       string `json:"selectedPosition" yaml:"selectedPosition"`
	EnableChapterHierarchy bool   `json:"enableChapterHierarchy,omitempty" yaml:"enableChapterHierarchy,omitempty"`
	ReviewRules            string `json:"reviewRules,omitempty" yaml:"reviewRules,omitempty"`
	Biz1                   string `json:"biz1,omitempty" yaml:"biz1,omitempty"`
	Biz2                   string `json:"biz2,omitempty" yaml:"biz2,omitempty"`
	Biz3                   string `json:"biz3,omitempty" yaml:"biz3,omitempty"`
	Biz4                   string `json:"biz4,omitempty" yaml:"biz4,omitempty"`
	Biz5                   string `json:"biz5,omitempty" yaml:"biz5,omitempty"`
	HasQuota               bool   `json:"hasQuota" yaml:"hasQuota"`
	BaseSignature          string `json:"baseSignature" yaml:"baseSignature"`
	PackID                 string `json:"packID" yaml:"packID"`
}

type feishuReviewRule struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	RiskLevel string  `json:"riskLevel"`
	Logic     string  `json:"logic"`
	Tips      *string `json:"tips,omitempty"`
}

// Validate 校验字段捷径入口的文件、位置、额度和签名材料。
func (request FeishuStartRequest) Validate() error {
	if request.FileID <= 0 {
		return fmt.Errorf("fileId 必须大于 0")
	}
	if strings.TrimSpace(request.SelectedPosition) == "" {
		return fmt.Errorf("selectedPosition 不能为空")
	}
	if request.ReviewStrength != nil && (*request.ReviewStrength < 0 || *request.ReviewStrength > 2) {
		return fmt.Errorf("reviewStrength 必须在 0 到 2 之间")
	}
	if !request.HasQuota {
		return fmt.Errorf("hasQuota 必须为 true")
	}
	if strings.TrimSpace(request.BaseSignature) == "" {
		return fmt.Errorf("baseSignature 不能为空")
	}
	if strings.TrimSpace(request.PackID) == "" {
		return fmt.Errorf("packID 不能为空")
	}
	if signature := strings.TrimSpace(request.BaseSignature); signature != "" {
		parts := strings.Split(signature, ".")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return fmt.Errorf("baseSignature 必须是 payload.signature 格式")
		}
	}
	if rules := strings.TrimSpace(request.ReviewRules); rules != "" {
		var decoded []feishuReviewRule
		if err := json.Unmarshal([]byte(rules), &decoded); err != nil {
			return fmt.Errorf("reviewRules 必须是规则数组 JSON: %w", err)
		}
		if decoded == nil {
			return fmt.Errorf("reviewRules 必须是规则数组 JSON")
		}
		for index, rule := range decoded {
			if strings.TrimSpace(rule.Name) == "" || strings.TrimSpace(rule.RiskLevel) == "" || strings.TrimSpace(rule.Logic) == "" {
				return fmt.Errorf("reviewRules 第 %d 项必须包含 name、riskLevel 和 logic", index+1)
			}
		}
	}
	return nil
}

// ValidateReviewIdentity 校验普通 V3 发起审查所需的 businessId/appType/fileId/fileHash 身份。
// 入参：request StartRequest 提供文件与业务上下文，config 不参与本校验。
// 返回值：error，身份字段完整有效时为 nil。
func ValidateReviewIdentity(request StartRequest) error {
	if strings.TrimSpace(request.BusinessID) == "" {
		return fmt.Errorf("businessId 不能为空")
	}
	if err := ValidateAppType(request.AppType); err != nil {
		return err
	}
	if request.FileID <= 0 {
		return fmt.Errorf("fileId 必须大于 0")
	}
	if err := ValidateFileHash(request.FileHash); err != nil {
		return err
	}
	return nil
}

// ValidateSubjectIdentity 校验主体提取所需的业务和文件身份；fileHash 仅作可选链路追踪字段。
func ValidateSubjectIdentity(request StartRequest) error {
	if strings.TrimSpace(request.BusinessID) == "" {
		return fmt.Errorf("businessId 不能为空")
	}
	if err := ValidateAppType(request.AppType); err != nil {
		return err
	}
	if request.FileID <= 0 {
		return fmt.Errorf("fileId 必须大于 0")
	}
	return nil
}

// ValidateUploadBusinessContext 校验上传阶段按 appType 要求的业务上下文。
// CLM 上传接口在创建业务文件快照前必须拿到 businessId；其他类型允许由服务端生成。
func ValidateUploadBusinessContext(appType string, businessID string) error {
	if err := ValidateAppType(appType); err != nil {
		return err
	}
	if appType == AppTypeCLM && strings.TrimSpace(businessID) == "" {
		return fmt.Errorf("CLM 上传必须提供 businessId")
	}
	return nil
}

// RunSource 描述一键工作流的本地文件或 URL 输入。
type RunSource struct {
	Type    string `json:"type" yaml:"type"`
	Path    string `json:"path,omitempty" yaml:"path,omitempty"`
	FileURL string `json:"fileUrl,omitempty" yaml:"fileUrl,omitempty"`
	Name    string `json:"name" yaml:"name"`
}

// RunSpec 是 review run 的稳定输入协议。
type RunSpec struct {
	Source          RunSource      `json:"source" yaml:"source"`
	BusinessID      string         `json:"businessId,omitempty" yaml:"businessId,omitempty"`
	FileHash        string         `json:"fileHash,omitempty" yaml:"fileHash,omitempty"`
	AppType         string         `json:"appType,omitempty" yaml:"appType,omitempty"`
	Config          map[string]any `json:"config" yaml:"config"`
	ExtractSubjects bool           `json:"extractSubjects,omitempty" yaml:"extractSubjects,omitempty"`
	Wait            bool           `json:"wait" yaml:"wait"`
}

// Normalize 补齐一键工作流的安全默认值，且不改变调用方提供的业务字段。
// 入参：无，接收者 RunSpec 为待归一化输入。
// 返回值：RunSpec，默认 appType=THIRD_PARTY 且 config 至少为空对象。
func (spec RunSpec) Normalize() RunSpec {
	spec.BusinessID = strings.TrimSpace(spec.BusinessID)
	spec.FileHash = normalizeSHA256(spec.FileHash)
	if spec.AppType == "" {
		spec.AppType = AppTypeThirdParty
	}
	if spec.Config == nil {
		spec.Config = map[string]any{}
	}
	return spec
}

// Validate 通过 review-run JSON Schema 校验一键工作流输入结构。
// 入参：无，接收者 RunSpec 为已归一化或待归一化的工作流输入。
// 返回值：error，输入符合 review-run.schema.json 时为 nil。
func (spec RunSpec) Validate() error {
	return contracts.ValidateSchema("review-run.schema.json", spec.Normalize())
}

// ValidateFileHash 校验 V3 发起审查所需的 SHA-256 十六进制指纹。
func ValidateFileHash(fileHash string) error {
	normalized := normalizeSHA256(fileHash)
	if len(normalized) != 64 {
		return fmt.Errorf("fileHash 必须是 64 位 SHA-256 十六进制字符串")
	}
	for _, character := range normalized {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return fmt.Errorf("fileHash 必须是 64 位 SHA-256 十六进制字符串")
		}
	}
	return nil
}

func normalizeSHA256(fileHash string) string {
	return strings.ToLower(strings.TrimSpace(fileHash))
}

// RunResult 汇总一键工作流各阶段结果，便于 Agent 追踪 fileId、taskId 和终态详情。
type RunResult struct {
	Upload   Document `json:"upload" yaml:"upload"`
	Subjects Document `json:"subjects,omitempty" yaml:"subjects,omitempty"`
	Start    Document `json:"start" yaml:"start"`
	Final    Document `json:"final,omitempty" yaml:"final,omitempty"`
}

// TaskQuery 是 status、info 和 wait 的公共查询上下文。
type TaskQuery struct {
	TaskID          int64  `json:"taskId"`
	BusinessID      string `json:"businessId,omitempty"`
	AppType         string `json:"appType,omitempty"`
	VisibilityScope string `json:"visibilityScope,omitempty"`
}

// FeishuTaskQuery 对应字段捷径接口合并状态与详情的查询参数。
// 入参：smartAuditId 是字段捷径入口返回的任务 ID；reviewPosition 为可选审查立场。
// 返回值：该结构直接映射后端 /open-api/v1/smartAudit/info 的 query。
type FeishuTaskQuery struct {
	SmartAuditID   int64  `json:"smartAuditId"`
	ReviewPosition string `json:"reviewPosition,omitempty"`
}

// FeishuTaskStatus 将字段捷径接口的数值状态转换为工作流可识别的语义状态。
// 入参：document 是 /smartAudit/info 返回的业务对象。
// 返回值：prepare、running、success、fail、skipped 或空字符串。
func FeishuTaskStatus(document Document) string {
	if status, ok := StringValue(document, "taskStatus"); ok {
		if normalized := normalizeFeishuTaskStatus(status); normalized != "" {
			return normalized
		}
	}
	if status, ok := StringValue(document, "taskStatusName"); ok {
		return normalizeFeishuTaskStatus(status)
	}
	statusCode, ok := Int64Value(document, "taskStatus")
	if !ok {
		return ""
	}
	switch statusCode {
	case 4:
		return "prepare"
	case 0:
		return "running"
	case 1:
		return "success"
	case 2:
		return "fail"
	case 3:
		return "skipped"
	default:
		return ""
	}
}

// normalizeFeishuTaskStatus 统一字段捷径状态名称，兼容后端返回的枚举名和工作流语义值。
// 入参：status string 为 taskStatus 或 taskStatusName 文本。
// 返回值：prepare、running、success、fail、skipped 或空字符串。
func normalizeFeishuTaskStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PREPARE":
		return "prepare"
	case "RUNNING":
		return "running"
	case "COMPLETE", "SUCCESS":
		return "success"
	case "FAIL", "FAILED":
		return "fail"
	case "SKIPPED":
		return "skipped"
	case "0":
		return "running"
	case "1":
		return "success"
	case "2":
		return "fail"
	case "3":
		return "skipped"
	case "4":
		return "prepare"
	}
	return ""
}

// ValidateAppType 校验后端冻结的三种接入应用类型。
// 入参：appType string 为待校验值。
// 返回值：error，值为 CLM、CR 或 THIRD_PARTY 时为 nil。
func ValidateAppType(appType string) error {
	switch appType {
	case AppTypeCLM, AppTypeCR, AppTypeThirdParty:
		return nil
	default:
		return fmt.Errorf("appType 必须是 CLM、CR 或 THIRD_PARTY")
	}
}

// DecodeDocument 用 UseNumber 解码平台 data，避免 int64 标识被 float64 破坏精度。
// 入参：content []byte 为 envelope.data 的 JSON。
// 返回值：Document 为业务对象；error 在 data 不是对象时非 nil。
func DecodeDocument(content []byte) (Document, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("解析业务响应: %w", err)
	}
	if document == nil {
		return nil, fmt.Errorf("业务响应 data 不能为空")
	}
	return document, nil
}

// StringValue 从 Document 中读取字符串，并兼容 JSON number。
// 入参：document Document 为业务对象；key string 为字段名。
// 返回值：string 为字段文本；bool 表示字段是否存在且非空。
func StringValue(document Document, key string) (string, bool) {
	value, exists := document[key]
	if !exists || value == nil {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		return typed, typed != ""
	case json.Number:
		return typed.String(), typed.String() != ""
	default:
		text := fmt.Sprint(typed)
		return text, text != ""
	}
}

// Int64Value 从 Document 中读取可能由字符串或 JSON number 表示的 int64 标识。
// 入参：document Document 为业务对象；key string 为字段名。
// 返回值：int64 为标识值；bool 表示转换是否成功。
func Int64Value(document Document, key string) (int64, bool) {
	value, exists := document[key]
	if !exists || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), typed == float64(int64(typed))
	case string:
		var parsed int64
		_, err := fmt.Sscan(typed, &parsed)
		return parsed, err == nil
	default:
		return 0, false
	}
}
