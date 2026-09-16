package invocation

import (
	"net/http"
	"strings"
)

const (
	HeaderChannelType     = "X-Qfei-Channel-Type"
	HeaderAgentSourceType = "X-Qfei-Agent-Source-Type"
	HeaderProductCode     = "X-Qfei-Product-Code"
	HeaderEvidenceType    = "X-Qfei-Evidence-Type"
	HeaderConfidence      = "X-Qfei-Channel-Confidence"
	HeaderDetectorVersion = "X-Qfei-Detector-Version"
	HeaderRuleID          = "X-Qfei-Rule-Id"

	ProductCodeEveryline = "contract-review"
	legacyRequestSource  = "X-Qfei-Request-Source-Type"
	maxHeaderValueBytes  = 256
)

func ApplyHeaders(headers http.Header, result Result) {
	if headers == nil {
		return
	}
	// Remove the superseded field even when a caller supplied it explicitly.
	headers.Del(legacyRequestSource)
	setOrDeleteHeader(headers, HeaderChannelType, result.ChannelType)
	setOrDeleteHeader(headers, HeaderAgentSourceType, result.AgentSourceType)
	setOrDeleteHeader(headers, HeaderProductCode, result.ProductCode)
	setOrDeleteHeader(headers, HeaderEvidenceType, result.EvidenceType)
	setOrDeleteHeader(headers, HeaderConfidence, result.Confidence)
	setOrDeleteHeader(headers, HeaderDetectorVersion, result.DetectorVersion)
	setOrDeleteHeader(headers, HeaderRuleID, result.RuleID)
}

func setOrDeleteHeader(headers http.Header, name, value string) {
	value = strings.TrimSpace(value)
	if !validHeaderValue(value) {
		headers.Del(name)
		return
	}
	headers.Set(name, value)
}

// 来源是可选元数据；非法或过长的值只丢弃该字段，不能拖累 HTTP 发送。
func validHeaderValue(value string) bool {
	if value == "" || len(value) > maxHeaderValueBytes {
		return false
	}
	for index := range value {
		if value[index] < 0x20 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
