package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"git.qtech.cn/ai/everyline-cli/internal/contracts"
)

const (
	AppTypeCLM                    = "CLM"
	AppTypeCR                     = "CR"
	AppTypeThirdParty             = "THIRD_PARTY"
	VisibilityScopeContractResult = "contractResult"
	// usageReportBusinessCodeEverylineCLI 是 startReview 固定上报的 CLI 业务编码，不接受调用方覆盖。
	usageReportBusinessCodeEverylineCLI = "everyLine_100_openApi_cli"
)

// ErrReviewRuleSourceRequired 表示发起审查时没有选择任何可执行的规则来源。
var ErrReviewRuleSourceRequired = errors.New("至少提供一种规则来源：非空 selectedCheckListIds，或 matchContractTypeRulePackage=true")

// Document 保存平台 data 对象并保留未来新增字段，避免 CLI 因响应扩展而丢数据。
type Document map[string]any

// StartRequest 对应 operation smartAuditTaskStartReview 的冻结请求契约。
type StartRequest struct {
	BusinessID string `json:"businessId" yaml:"businessId"`
	// AppType、TriggerScene 等字段保留在内部类型中供已有领域调用兼容；CLI start/run 输入不再暴露或透传这些字段。
	AppType                        string         `json:"appType" yaml:"appType"`
	FileID                         int64          `json:"fileId" yaml:"fileId"`
	FileHash                       string         `json:"fileHash" yaml:"fileHash"`
	Config                         map[string]any `json:"config" yaml:"config"`
	AllowInvalidSelectedChecklists bool           `json:"allowInvalidSelectedChecklists,omitempty" yaml:"allowInvalidSelectedChecklists,omitempty"`
	TriggerScene                   string         `json:"triggerScene,omitempty" yaml:"triggerScene,omitempty"`
}

// StartInput 是 review task start 的 CLI 输入契约，只保留发起审查所需的必填字段。
type StartInput struct {
	BusinessID string         `json:"businessId" yaml:"businessId"`
	FileID     int64          `json:"fileId" yaml:"fileId"`
	FileHash   string         `json:"fileHash" yaml:"fileHash"`
	Config     map[string]any `json:"config" yaml:"config"`
}

// Normalize 规范化 start CLI 输入中的文本和审查配置，不改变字段语义。
// 入参：无，接收者 StartInput 为用户输入。
// 返回值：StartInput，为可执行本地校验和转换的规范化输入。
func (input StartInput) Normalize() StartInput {
	input.BusinessID = strings.TrimSpace(input.BusinessID)
	input.FileHash = normalizeSHA256(input.FileHash)
	input.Config = normalizeReviewConfig(input.Config)
	return input
}

// Validate 校验 start CLI 输入模型，并在 HTTP 模型转换前执行规则来源组合校验。
// 入参：无，接收者 StartInput 为用户输入。
// 返回值：error，输入合法且至少选择一种规则来源时为 nil。
func (input StartInput) Validate() error {
	input = input.Normalize()
	schemaErr := contracts.ValidateSchema("review-start-input.schema.json", input)
	if ruleErr := ValidateReviewRuleSources(input.Config); ruleErr != nil && hasValidReviewInputCore(input.Config) {
		return ruleErr
	}
	return schemaErr
}

// ToRequest 将中文 CLI 输入转换为后端发起请求；fileId 继续保持当前 CLI 的 int64 输入行为。
// 入参：无，接收者 StartInput 为已校验或待转换的用户输入。
// 返回值：StartRequest 为使用数字审查强度的后端模型；error 为未知审查强度。
func (input StartInput) ToRequest() (StartRequest, error) {
	input = input.Normalize()
	config, err := reviewConfigContractPayload(input.Config)
	if err != nil {
		return StartRequest{}, err
	}
	return StartRequest{
		BusinessID: input.BusinessID,
		FileID:     input.FileID,
		FileHash:   input.FileHash,
		Config:     config,
	}, nil
}

// ContractPayload 将内部发起请求转换为开放接口边界格式。
// 入参：无，接收者 StartRequest 为内部使用 int64 表示文件 ID 的请求。
// 返回值：map[string]any，为 startReview 接口使用字符串 fileId 并固定携带用量上报业务编码的规范化请求体。
func (request StartRequest) ContractPayload() map[string]any {
	request = request.Normalize()
	payload := map[string]any{
		"businessId": request.BusinessID,
		// startReview 接口要求 fileId 以 JSON 字符串传输；内部保留 int64 便于解析上传响应和任务数据。
		"fileId":   strconv.FormatInt(request.FileID, 10),
		"fileHash": request.FileHash,
		"config":   request.Config,
		// 用量归因字段只在 HTTP 边界生成，避免 CLI 输入或 Profile 改写固定业务编码。
		"usageReportContext": map[string]string{
			"reportBusinessCode": usageReportBusinessCodeEverylineCLI,
		},
	}
	return payload
}

// Validate 校验普通发起审查的 API 必填字段和 config 子字段。
// 入参：无，接收者 StartRequest 为待校验请求。
// 返回值：error，请求满足后端 V3 契约时为 nil。
func (request StartRequest) Validate() error {
	if err := ValidateReviewIdentity(request); err != nil {
		return err
	}
	if err := contracts.ValidateSchema("review-start.schema.json", request.ContractPayload()); err != nil {
		return err
	}
	return nil
}

// Normalize 将缺失配置规范化为空对象，以便 Schema 返回明确的必填字段错误。
func (request StartRequest) Normalize() StartRequest {
	request.BusinessID = strings.TrimSpace(request.BusinessID)
	request.FileHash = normalizeSHA256(request.FileHash)
	if request.Config == nil {
		request.Config = map[string]any{}
	}
	return request
}

// ValidateReviewIdentity 校验普通 V3 发起审查所需的 businessId/fileId/fileHash 身份。
// 入参：request StartRequest 提供文件与业务上下文，config 不参与本校验。
// 返回值：error，身份字段完整有效时为 nil。
func ValidateReviewIdentity(request StartRequest) error {
	if strings.TrimSpace(request.BusinessID) == "" {
		return fmt.Errorf("businessId 不能为空")
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
	if request.FileID <= 0 {
		return fmt.Errorf("fileId 必须大于 0")
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
	Config          map[string]any `json:"config" yaml:"config"`
	ExtractSubjects bool           `json:"extractSubjects,omitempty" yaml:"extractSubjects,omitempty"`
	Wait            bool           `json:"wait" yaml:"wait"`
}

// Normalize 规范化一键工作流输入，且不改变调用方提供的业务字段。
// 入参：无，接收者 RunSpec 为待归一化输入。
// 返回值：RunSpec，保证 config 至少为空对象；空对象仍不满足发起审查契约。
func (spec RunSpec) Normalize() RunSpec {
	spec.BusinessID = strings.TrimSpace(spec.BusinessID)
	spec.FileHash = normalizeSHA256(spec.FileHash)
	spec.Config = normalizeReviewConfig(spec.Config)
	return spec
}

// Validate 通过 review-run JSON Schema 校验一键工作流输入结构。
// 入参：无，接收者 RunSpec 为已归一化或待归一化的工作流输入。
// 返回值：error，输入符合 review-run.schema.json 时为 nil。
func (spec RunSpec) Validate() error {
	spec = spec.Normalize()
	schemaErr := contracts.ValidateSchema("review-run.schema.json", spec)
	if ruleErr := ValidateReviewRuleSources(spec.Config); ruleErr != nil && hasValidReviewInputCore(spec.Config) {
		return ruleErr
	}
	return schemaErr
}

// ValidateReviewRuleSources 校验自定义清单和合同类型规则包至少启用一项；两项可同时生效。
// 入参：config map[string]any 为 CLI 或后端审查配置。
// 返回值：error，存在非空 selectedCheckListIds 或 matchContractTypeRulePackage=true 时为 nil。
func ValidateReviewRuleSources(config map[string]any) error {
	if hasSelectedChecklist(config["selectedCheckListIds"]) {
		return nil
	}
	if enabled, ok := config["matchContractTypeRulePackage"].(bool); ok && enabled {
		return nil
	}
	return ErrReviewRuleSourceRequired
}

// reviewConfigContractPayload 将 CLI 中文或兼容数字审查强度转换为后端 0/1/2 枚举，并复制其余配置。
// 入参：config map[string]any 为已校验的 CLI 审查配置。
// 返回值：map[string]any 为后端 config；error 为 reviewStrength 缺失或取值未知。
func reviewConfigContractPayload(config map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(config))
	for key, value := range config {
		result[key] = value
	}
	strength, err := reviewStrengthContractValue(result["reviewStrength"])
	if err != nil {
		return nil, err
	}
	result["reviewStrength"] = strength
	return result, nil
}

// reviewStrengthContractValue 将 CLI 新旧两种强度输入统一为后端整数枚举。
// 入参：value any 为中文标签，或 JSON/Go 数字 0、1、2。
// 返回值：int 为后端枚举；error 为类型或取值不受支持。
func reviewStrengthContractValue(value any) (int, error) {
	switch strength := value.(type) {
	case string:
		switch strings.TrimSpace(strength) {
		case "弱势":
			return 0, nil
		case "中立":
			return 1, nil
		case "强势":
			return 2, nil
		}
	case json.Number:
		if number, err := strength.Int64(); err == nil && number >= 0 && number <= 2 {
			return int(number), nil
		}
	case int:
		if strength >= 0 && strength <= 2 {
			return strength, nil
		}
	case int64:
		if strength >= 0 && strength <= 2 {
			return int(strength), nil
		}
	case float64:
		if strength == 0 || strength == 1 || strength == 2 {
			return int(strength), nil
		}
	}
	return 0, fmt.Errorf("config.reviewStrength 必须是弱势、中立、强势或 0、1、2")
}

// normalizeReviewConfig 复制并清理 CLI 审查配置中的文本，避免直接修改调用方 map。
// 入参：config map[string]any 为原始配置。
// 返回值：map[string]any，为至少非 nil 的配置副本。
func normalizeReviewConfig(config map[string]any) map[string]any {
	result := make(map[string]any, len(config))
	for key, value := range config {
		switch key {
		case "selectedPosition", "selectedAuditRole", "reviewStrength":
			if text, ok := value.(string); ok {
				value = strings.TrimSpace(text)
			}
		case "selectedCheckListIds":
			if values, ok := value.([]any); ok {
				normalized := make([]any, len(values))
				for index, item := range values {
					if text, isText := item.(string); isText {
						item = strings.TrimSpace(text)
					}
					normalized[index] = item
				}
				value = normalized
			}
		}
		result[key] = value
	}
	return result
}

// hasSelectedChecklist 判断配置值是否包含至少一个非空清单 ID，并兼容解码和领域测试的切片类型。
// 入参：value any 为 selectedCheckListIds 字段值。
// 返回值：bool，至少包含一个非空字符串时为 true。
func hasSelectedChecklist(value any) bool {
	switch values := value.(type) {
	case []string:
		for _, item := range values {
			if strings.TrimSpace(item) != "" {
				return true
			}
		}
	case []any:
		for _, item := range values {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				return true
			}
		}
	}
	return false
}

// hasValidReviewInputCore 判断规则来源之外的三个 CLI 审查字段是否已具备有效类型和值。
// 入参：config map[string]any 为 CLI 审查配置。
// 返回值：bool，立场、角色和中文或兼容数字强度均有效时为 true。
func hasValidReviewInputCore(config map[string]any) bool {
	position, positionOK := config["selectedPosition"].(string)
	role, roleOK := config["selectedAuditRole"].(string)
	if !positionOK || !roleOK || strings.TrimSpace(position) == "" || strings.TrimSpace(role) == "" {
		return false
	}
	_, err := reviewStrengthContractValue(config["reviewStrength"])
	return err == nil
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

// TaskQuery 是 status、info 和 result 的公共查询上下文。
type TaskQuery struct {
	TaskID          int64  `json:"taskId"`
	BusinessID      string `json:"businessId,omitempty"`
	AppType         string `json:"appType,omitempty"`
	VisibilityScope string `json:"visibilityScope,omitempty"`
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
