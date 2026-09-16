package tracecontext_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"git.qtech.cn/ai/everyline-cli/internal/tracecontext"
)

var traceparentPattern = regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-01$`)

func TestRequestTraceApplyUsesStableTraceIDAndFreshSpanID(t *testing.T) {
	t.Parallel()

	requestTrace, err := tracecontext.NewRequestTrace()
	if err != nil {
		t.Fatalf("NewRequestTrace() error = %v", err)
	}

	first := make(http.Header)
	first.Set(tracecontext.HeaderTraceparent, "caller supplied")
	first.Set(tracecontext.HeaderLogID, "caller supplied")
	if err := requestTrace.Apply(first); err != nil {
		t.Fatalf("Apply(first) error = %v", err)
	}

	second := make(http.Header)
	if err := requestTrace.Apply(second); err != nil {
		t.Fatalf("Apply(second) error = %v", err)
	}

	firstParts := traceparentPattern.FindStringSubmatch(first.Get(tracecontext.HeaderTraceparent))
	secondParts := traceparentPattern.FindStringSubmatch(second.Get(tracecontext.HeaderTraceparent))
	if firstParts == nil || secondParts == nil {
		t.Fatalf("traceparent values = %q, %q", first.Get(tracecontext.HeaderTraceparent), second.Get(tracecontext.HeaderTraceparent))
	}
	if firstParts[1] != requestTrace.TraceID || secondParts[1] != requestTrace.TraceID {
		t.Fatalf("trace ids = %q, %q, want %q", firstParts[1], secondParts[1], requestTrace.TraceID)
	}
	if first.Get(tracecontext.HeaderLogID) != requestTrace.TraceID || second.Get(tracecontext.HeaderLogID) != requestTrace.TraceID {
		t.Fatalf("X-Log-Id values = %q, %q, want %q", first.Get(tracecontext.HeaderLogID), second.Get(tracecontext.HeaderLogID), requestTrace.TraceID)
	}
	if firstParts[2] == secondParts[2] {
		t.Fatalf("span id was reused across attempts: %q", firstParts[2])
	}
	if strings.Trim(firstParts[1], "0") == "" || strings.Trim(firstParts[2], "0") == "" {
		t.Fatal("W3C trace and span ids must not be all zero")
	}
}

func TestRequestTraceApplyRejectsInvalidTraceID(t *testing.T) {
	t.Parallel()

	for _, traceID := range []string{
		"",
		"00000000000000000000000000000000",
		"1111111111111111111111111111111",
		"1111111111111111111111111111111Z",
	} {
		headers := make(http.Header)
		if err := (tracecontext.RequestTrace{TraceID: traceID}).Apply(headers); err == nil {
			t.Fatalf("Apply() error = nil for trace id %q", traceID)
		}
	}
}
