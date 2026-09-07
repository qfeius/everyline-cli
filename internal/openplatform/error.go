package openplatform

import (
	"fmt"
	"time"
)

// APIError 保留远端错误定位和安全重试所需的完整上下文。
type APIError struct {
	HTTPStatus int           `json:"httpStatus" yaml:"httpStatus"`
	Code       string        `json:"code" yaml:"code"`
	Message    string        `json:"message" yaml:"message"`
	RequestID  string        `json:"requestId,omitempty" yaml:"requestId,omitempty"`
	RetryAfter time.Duration `json:"retryAfter,omitempty" yaml:"retryAfter,omitempty"`
	Retryable  bool          `json:"retryable" yaml:"retryable"`
}

// Error 将 APIError 格式化为适合 stderr 的单行信息，同时避免泄露 token。
// 入参：无，接收者包含远端错误上下文。
// 返回值：string，脱敏后的错误描述。
func (apiError *APIError) Error() string {
	if apiError.RequestID != "" {
		return fmt.Sprintf("远端 API 错误: http=%d code=%s message=%s request_id=%s", apiError.HTTPStatus, apiError.Code, apiError.Message, apiError.RequestID)
	}
	return fmt.Sprintf("远端 API 错误: http=%d code=%s message=%s", apiError.HTTPStatus, apiError.Code, apiError.Message)
}
