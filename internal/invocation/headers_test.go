package invocation

import (
	"net/http"
	"strings"
	"testing"
)

func TestApplyHeadersOverwritesCallerValuesAndRemovesEmptyRule(t *testing.T) {
	headers := http.Header{
		HeaderChannelType:     {"spoofed-channel"},
		HeaderAgentSourceType: {"spoofed-agent"},
		HeaderProductCode:     {"spoofed-product"},
		legacyRequestSource:   {"legacy"},
		HeaderRuleID:          {"stale-rule"},
	}
	ApplyHeaders(headers, Result{
		ChannelType:     "cli",
		AgentSourceType: "workbuddy",
		ProductCode:     ProductCodeEveryline,
		EvidenceType:    "process_executable_path",
		Confidence:      "medium",
		DetectorVersion: DetectorVersion,
	})

	if got := headers.Get(HeaderChannelType); got != "cli" {
		t.Fatalf("channel header = %q", got)
	}
	if got := headers.Get(HeaderAgentSourceType); got != "workbuddy" {
		t.Fatalf("agent source header = %q", got)
	}
	if got := headers.Get(HeaderProductCode); got != ProductCodeEveryline {
		t.Fatalf("product code header = %q", got)
	}
	if got := headers.Get(legacyRequestSource); got != "" {
		t.Fatalf("legacy request source header = %q", got)
	}
	if got := headers.Get(HeaderRuleID); got != "" {
		t.Fatalf("rule header = %q", got)
	}
}

func TestApplyHeadersDropsOnlyInvalidOptionalValues(t *testing.T) {
	for _, value := range []string{"bad\r\nInjected: true", "bad\x00", "bad\x7f", "豆包", strings.Repeat("x", 257)} {
		t.Run(fmtHeaderTestName(value), func(t *testing.T) {
			headers := http.Header{"Authorization": {"Bearer unchanged"}, "Content-Type": {"application/json"}}
			report := Analyze(nil, nil)
			report.RuleID = value
			ApplyHeaders(headers, report)
			if headers.Get(HeaderRuleID) != "" || headers.Get(HeaderChannelType) != "cli" ||
				headers.Get(HeaderProductCode) != "contract-review" || headers.Get(HeaderAgentSourceType) != "unknown" ||
				headers.Get("Authorization") != "Bearer unchanged" || headers.Get("Content-Type") != "application/json" {
				t.Fatalf("invalid metadata damaged other headers: %v", headers)
			}
		})
	}
	ApplyHeaders(nil, Result{})
	headers := http.Header{}
	report := Analyze(nil, nil)
	report.RuleID = "  " + strings.Repeat("x", 256) + "  "
	ApplyHeaders(headers, report)
	if len(headers.Get(HeaderRuleID)) != 256 {
		t.Fatal("valid boundary value discarded")
	}
}

func fmtHeaderTestName(value string) string {
	if len(value) > 256 {
		return "oversized"
	}
	return value
}
