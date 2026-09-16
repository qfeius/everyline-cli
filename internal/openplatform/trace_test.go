package openplatform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/tracecontext"
)

var tracePattern = regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-01$`)

func TestClientTraceRetriesAndIndependentRequests(t *testing.T) {
	captured := make(chan http.Header, 3)
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Header.Clone()
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"code":503,"msg":"retry"}`)
			return
		}
		w.Header().Set("X-Request-Id", "server-request")
		w.Header().Set("X-Log-Id", "server-overridden-log-id")
		fmt.Fprint(w, `{"code":200,"data":[{"id":"1"}]}`)
	}))
	defer server.Close()
	var diagnostics bytes.Buffer
	client := NewClient(testProfile(server.URL), staticTokenProvider{}, server.Client(),
		WithTraceOutput(&diagnostics), WithBeforeRequestHooks(func(_ context.Context, r *http.Request) error {
			r.Header.Set("traceparent", "old-hook-trace")
			r.Header.Set("X-Log-Id", "old-hook-log-id")
			return nil
		}))
	client.sleep = func(context.Context, time.Duration) error { return nil }
	request := hookTestRequest()
	request.Header = http.Header{"traceparent": {"old-caller-trace"}, "x-log-id": {"old-caller-log-id"}}
	originalHeaders := request.Header.Clone()
	first, err := client.Do(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Do(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.TraceID == "" || first.TraceID == second.TraceID || first.RequestID != "server-request" ||
		string(first.Data) != `[{"id":"1"}]` {
		t.Fatalf("responses: first=%+v second=%+v", first, second)
	}
	spans := map[string]bool{}
	for i, expected := range []string{first.TraceID, first.TraceID, second.TraceID} {
		headers := <-captured
		parts := tracePattern.FindStringSubmatch(headers.Get(tracecontext.HeaderTraceparent))
		if parts == nil || parts[1] != expected || headers.Get(tracecontext.HeaderLogID) != expected {
			t.Fatalf("attempt %d: headers=%v expected trace=%s", i, headers, expected)
		}
		if spans[parts[2]] || strings.Trim(parts[2], "0") == "" || len(headers.Values("X-Log-Id")) != 1 ||
			len(headers.Values("traceparent")) != 1 || headers.Get("Authorization") != "Bearer token-test" {
			t.Fatalf("attempt %d: reused span or changed headers=%v", i, headers)
		}
		spans[parts[2]] = true
	}
	if !reflect.DeepEqual(request.Header, originalHeaders) {
		t.Fatal("caller headers were mutated")
	}
	if strings.Count(diagnostics.String(), "request_trace ") != 3 ||
		!strings.Contains(diagnostics.String(), first.TraceID) || strings.Contains(diagnostics.String(), "token-test") {
		t.Fatalf("invalid diagnostics: %s", &diagnostics)
	}
}

func TestClientTracePreservesAPIErrorAndDoesNotRetryWrites(t *testing.T) {
	captured := make(chan http.Header, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Header.Clone()
		w.Header().Set("X-Request-Id", "server-error")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"code":503,"msg":"unavailable"}`)
	}))
	defer server.Close()
	client := NewClient(testProfile(server.URL), staticTokenProvider{}, server.Client())
	response, err := client.Do(context.Background(), Request{OperationID: "createReviewChecklist", Method: http.MethodPost,
		Path: "/open-apis/review-rules/review-checklists", Header: http.Header{"Content-Type": {"application/json"}},
		Body: []byte(`{"name":"test","reviewRuleIds":["1"]}`)})
	var traceErr *TraceError
	var apiErr *APIError
	if !errors.As(err, &traceErr) || !errors.As(err, &apiErr) || apiErr.RequestID != "server-error" ||
		apiErr.HTTPStatus != 503 || traceErr.TraceID != response.TraceID || len(captured) != 1 {
		t.Fatalf("response=%+v err=%v requests=%d", response, err, len(captured))
	}
	if traceErr.TraceID != (<-captured).Get("X-Log-Id") || !strings.Contains(err.Error(), "trace_id="+traceErr.TraceID) {
		t.Fatalf("trace missing from error: %v", err)
	}
}

type traceTransport func(*http.Request) (*http.Response, error)

func (transport traceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return transport(r)
}

func TestClientTracePreservesNetworkAndBackoffCancellation(t *testing.T) {
	for _, mode := range []string{"network", "backoff-cancel", "hook-cancel"} {
		t.Run(mode, func(t *testing.T) {
			cause := errors.New("transport failed")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var sentTrace string
			httpClient := &http.Client{Transport: traceTransport(func(r *http.Request) (*http.Response, error) {
				sentTrace = r.Header.Get("X-Log-Id")
				if mode == "backoff-cancel" {
					return nil, context.DeadlineExceeded
				}
				return nil, cause
			})}
			client := NewClient(testProfile("https://example.test"), staticTokenProvider{}, httpClient)
			if mode == "backoff-cancel" {
				client.sleep = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
				cause = context.Canceled
			} else if mode == "hook-cancel" {
				client.beforeRequestHooks = []BeforeRequestHook{func(context.Context, *http.Request) error { cancel(); return nil }}
				cause = context.Canceled
			}
			response, err := client.Do(ctx, hookTestRequest())
			var traceErr *TraceError
			if !errors.Is(err, cause) || !errors.As(err, &traceErr) || response.TraceID == "" || response.TraceID != traceErr.TraceID {
				t.Fatalf("response=%+v error=%v", response, err)
			}
			if mode == "hook-cancel" {
				if sentTrace != "" {
					t.Fatal("cancelled request was sent")
				}
			} else if sentTrace != response.TraceID {
				t.Fatalf("sent trace=%s response trace=%s", sentTrace, response.TraceID)
			}
		})
	}
}

func TestClientLocalValidationDoesNotCreateBusinessTrace(t *testing.T) {
	client := NewClient(testProfile("https://example.test"), staticTokenProvider{}, nil)
	request := hookTestRequest()
	request.Path = "/not-open-api"
	response, err := client.Do(context.Background(), request)
	var traceErr *TraceError
	if err == nil || errors.As(err, &traceErr) || response.TraceID != "" {
		t.Fatalf("local error unexpectedly has a business trace: response=%+v err=%v", response, err)
	}
}
