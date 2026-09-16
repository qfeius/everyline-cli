package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/invocation"
	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
)

var testAgents = []string{"doubao", "doubaoWork", "workbuddy", "codex", "unknown"}

var requestTracePattern = regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-01$`)

func testInvocationReport(attempt int) invocation.Result {
	report := invocation.Analyze(nil, nil)
	report.AgentSourceType = testAgents[(attempt-1)%len(testAgents)]
	report.EvidenceType = "process_name"
	report.Confidence = "low"
	report.RuleID = fmt.Sprintf("test.attempt.%d", attempt)
	return report
}

func assertInvocationHeaders(t *testing.T, headers http.Header, attempt int) {
	t.Helper()
	parts := requestTracePattern.FindStringSubmatch(headers.Get("traceparent"))
	if parts == nil || parts[1] != headers.Get("X-Log-Id") {
		t.Errorf("attempt=%d invalid Trace headers: %v", attempt, headers)
	}
	expected := http.Header{}
	invocation.ApplyHeaders(expected, testInvocationReport(attempt))
	for name, values := range expected {
		if len(headers.Values(name)) != 1 || headers.Get(name) != values[0] {
			t.Errorf("attempt=%d %s=%q want=%q", attempt, name, headers.Values(name), values)
		}
	}
	if headers.Get("X-Qfei-Request-Source-Type") != "" {
		t.Error("obsolete source header was sent")
	}
}

func addInvocationTestProfile(t *testing.T, runtime *Runtime, baseURL string) {
	t.Helper()
	if err := runtime.Profiles.Add(config.Profile{Name: "local", BaseURL: baseURL,
		TokenURL: baseURL + "/token", AppID: "test-app", DefaultOutput: "json"}); err != nil {
		t.Fatal(err)
	}
}

// 同一个 Runtime 连续从不同客户端返回探测结果，验证没有在登录/首次调用时固化来源。
func TestCLIInvocationHeadersForEveryDomainAndIdentity(t *testing.T) {
	for _, identity := range []string{"app", "user"} {
		t.Run(identity, func(t *testing.T) {
			t.Setenv("EVERYLINE_ACCESS_TOKEN", "app-test-token")

			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := int(calls.Add(1))
				assertInvocationHeaders(t, r.Header, attempt)
				if r.Header.Get("Authorization") != "Bearer "+identity+"-test-token" {
					t.Error("identity token changed")
				}
				if r.Header.Get("User-Agent") != "everyline-cli" || r.Header.Get("Accept") != "application/json" {
					t.Error("existing headers changed")
				}
				fmt.Fprint(w, `{"code":200,"data":{}}`)
			}))
			defer server.Close()
			runtime, stdout, stderr := testRuntime(t)
			runtime.HTTP = server.Client()
			addInvocationTestProfile(t, runtime, server.URL)
			if identity == "user" {
				store := runtime.Tokens.(interface {
					SaveForIdentity(string, config.IdentityKind, auth.Token) error
				})
				if err := store.SaveForIdentity("local", config.IdentityUser, auth.Token{AccessToken: "user-test-token"}); err != nil {
					t.Fatal(err)
				}
			}

			inspections := 0
			runtime.InspectEnvironment = func(ctx context.Context, depth int) invocation.Result {
				if depth != invocation.DefaultMaxDepth || ctx.Err() != nil {
					t.Fatal("wrong detection context/depth")
				}
				inspections++
				return testInvocationReport(inspections)
			}
			commands := [][]string{
				{"review", "task", "status", "--task-id", "88"},
				{"checklist", "list"},
				{"rule", "group", "list"},
				{"rule", "list", "--group-id", "12"},
				{"review", "task", "info", "--task-id", "88"},
			}
			for _, args := range commands {
				stdout.Reset()
				if err := Execute(context.Background(), runtime, append(args, "--as", identity, "--verbose", "--output", "json")); err != nil {
					t.Fatalf("%v: %v", args, err)
				}
				if !json.Valid(stdout.Bytes()) {
					t.Fatalf("diagnostics polluted stdout: %s", stdout)
				}
			}
			if inspections != len(commands) || int(calls.Load()) != len(commands) {
				t.Fatalf("inspections=%d requests=%d", inspections, calls.Load())
			}
			if strings.Count(stderr.String(), "invocation_source ") != len(commands) ||
				strings.Count(stderr.String(), "request_trace ") != len(commands) || strings.Contains(stderr.String(), "test-token") {
				t.Fatalf("missing or sensitive diagnostic output: %s", stderr)
			}
		})
	}
}

// 真实 CLI 命令到本地 HTTP 接收端，覆盖 multipart、JSON、GET 轮询及一次 GET 重试。
func TestCLIReviewWorkflowInspectsUploadExtractionStartPollingAndRetry(t *testing.T) {
	t.Setenv("EVERYLINE_ACCESS_TOKEN", "workflow-token")
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var calls atomic.Int32
	statusCalls := 0
	traces := make(chan string, 7)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertInvocationHeaders(t, r.Header, int(calls.Add(1)))
		traces <- r.Header.Get("X-Log-Id")
		if r.Header.Get("Authorization") != "Bearer workflow-token" {
			t.Error("token changed")
		}
		data := `{}`
		switch r.URL.Path {
		case "/open-apis/contract-review/v3/file/contract/upload":
			if err := r.ParseMultipartForm(4 << 20); err != nil {
				t.Errorf("upload broken: %v", err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			body, err := io.ReadAll(file)
			file.Close()
			if err != nil || string(body) != "test-contract" || r.FormValue("appType") != "" {
				t.Error("multipart content changed")
			}
			data = `{"fileId":11,"businessId":"biz-1","fileHash":"` + hash + `"}`
		case "/open-apis/contract-review/v3/smartAudit/contract/subjects", "/open-apis/contract-review/v3/smartAudit/task/startReview":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["businessId"] != "biz-1" || body["fileId"] != "11" || body["appType"] != nil {
				t.Errorf("JSON body changed: %v", body)
			}
			if body["channelType"] != nil {
				t.Error("source metadata entered business payload")
			}
			data = `{"taskId":88,"status":"running","counterparts":[]}`
		case "/open-apis/contract-review/v3/smartAudit/task/status":
			statusCalls++
			if r.URL.Query().Get("taskId") != "88" {
				t.Error("task query changed")
			}
			if statusCalls == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"code":503,"msg":"retry","data":null}`)
				return
			}
			data = `{"taskId":88,"status":"running"}`
			if statusCalls == 3 {
				data = `{"taskId":88,"status":"success"}`
			}
		case "/open-apis/contract-review/v3/smartAudit/task/info":
			data = `{"taskId":88,"status":"success","result":[]}`
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"code":200,"data":`+data+`}`)
	}))
	defer server.Close()
	runtime, stdout, stderr := testRuntime(t)
	runtime.HTTP = server.Client()
	addInvocationTestProfile(t, runtime, server.URL)
	inspections := 0
	runtime.InspectEnvironment = func(context.Context, int) invocation.Result {
		inspections++
		return testInvocationReport(inspections)
	}
	file := filepath.Join(t.TempDir(), "contract.pdf")
	if err := os.WriteFile(file, []byte("test-contract"), 0600); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"source":     map[string]string{"type": "file", "path": file, "name": "contract.pdf"},
		"businessId": "biz-1", "extractSubjects": true, "wait": true,
		"config": map[string]any{"selectedPosition": "测试公司", "selectedAuditRole": "甲方", "reviewStrength": "中立", "matchContractTypeRulePackage": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = Execute(context.Background(), runtime, []string{"review", "run", "--data", string(payload),
		"--interval", "1ms", "--deadline", "10s", "--output", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 7 || inspections != 7 || !strings.Contains(stdout.String(), `"success"`) {
		t.Fatalf("requests=%d inspections=%d output=%s", calls.Load(), inspections, stdout)
	}
	seen := map[string]bool{}
	var previous string
	for i := 0; i < 7; i++ {
		traceID := <-traces
		if i == 4 {
			if traceID != previous {
				t.Fatal("GET retry changed logical Trace ID")
			}
		} else if seen[traceID] {
			t.Fatal("independent workflow stage or polling request reused Trace ID")
		}
		seen[traceID], previous = true, traceID
	}
	if strings.Contains(stderr.String(), "invocation_source") || strings.Contains(stderr.String(), "request_trace") {
		t.Fatal("non-verbose command emitted request diagnostics")
	}
}

func TestInvocationHookDoesNotApplyToTokenRefreshOrDryRun(t *testing.T) {
	t.Setenv("EVERYLINE_ACCESS_TOKEN", "")
	t.Setenv("EVERYLINE_APP_SECRET", "test-secret")
	var tokenCalls, businessCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokenCalls.Add(1)
			if r.Header.Get("traceparent") != "" || r.Header.Get("X-Log-Id") != "" {
				t.Error("token request carried a business Trace")
			}
			for name := range r.Header {
				if strings.HasPrefix(strings.ToLower(name), "x-qfei-") {
					t.Error("source hook ran on token request")
				}
			}
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"refreshed-token","expire":7200}`)
			return
		}
		assertInvocationHeaders(t, r.Header, int(businessCalls.Add(1)))
		if r.Header.Get("Authorization") != "Bearer refreshed-token" {
			t.Error("refresh token changed")
		}
		fmt.Fprint(w, `{"code":200,"data":[]}`)
	}))
	defer server.Close()
	runtime, _, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	addInvocationTestProfile(t, runtime, server.URL)
	inspections := 0
	runtime.InspectEnvironment = func(context.Context, int) invocation.Result {
		inspections++
		return testInvocationReport(inspections)
	}
	for _, args := range [][]string{
		{"checklist", "create", "--dry-run", "--data", `{"name":"test","reviewRuleIds":["1"]}`},
		{"version"}, {"--help"}, {"auth", "status"},
	} {
		if err := Execute(context.Background(), runtime, args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if inspections != 0 || tokenCalls.Load() != 0 || businessCalls.Load() != 0 {
		t.Fatal("local command made an inspection or request")
	}
	if err := Execute(context.Background(), runtime, []string{"checklist", "list"}); err != nil {
		t.Fatal(err)
	}
	if inspections != 1 || tokenCalls.Load() != 1 || businessCalls.Load() != 1 {
		t.Fatalf("inspections=%d token=%d business=%d", inspections, tokenCalls.Load(), businessCalls.Load())
	}
}

func TestInvocationFailureAndInvalidMetadataRemainOptional(t *testing.T) {
	for _, mode := range []string{"timeout", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("EVERYLINE_ACCESS_TOKEN", "fallback-token")
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get(invocation.HeaderChannelType) != "cli" || r.Header.Get(invocation.HeaderProductCode) != "contract-review" ||
					r.Header.Get(invocation.HeaderAgentSourceType) != "unknown" || r.Header.Get(invocation.HeaderRuleID) != "" {
					t.Errorf("unsafe fallback headers: %v", r.Header)
				}
				fmt.Fprint(w, `{"code":200,"data":[]}`)
			}))
			defer server.Close()
			runtime, _, _ := testRuntime(t)
			runtime.HTTP = server.Client()
			addInvocationTestProfile(t, runtime, server.URL)
			runtime.InspectEnvironment = func(parent context.Context, _ int) invocation.Result {
				report := invocation.Analyze(nil, nil)
				if mode == "timeout" {
					child, cancel := context.WithTimeout(parent, time.Millisecond)
					defer cancel()
					<-child.Done()
					report.Reason = "environment inspection timed out"
				} else {
					report.RuleID = "bad\r\nInjected: true"
				}
				return report
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := Execute(ctx, runtime, []string{"checklist", "list"}); err != nil {
				t.Fatal(err)
			}
			if ctx.Err() != nil || calls.Load() != 1 {
				t.Fatal("optional metadata blocked request")
			}
		})
	}
}

func TestInvocationHookRespectsBusinessCancellation(t *testing.T) {
	t.Setenv("EVERYLINE_ACCESS_TOKEN", "test-token")
	runtime, _, _ := testRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.InspectEnvironment = func(context.Context, int) invocation.Result {
		cancel()
		return testInvocationReport(1)
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid", nil)
	if err := runtime.environmentHook(false)(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if request.Header.Get(invocation.HeaderChannelType) != "" {
		t.Fatal("cancelled request was decorated")
	}
}

func TestCLIInvocationDoesNotChangeBusinessErrorsOrRetryWrites(t *testing.T) {
	t.Setenv("EVERYLINE_ACCESS_TOKEN", "test-token")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertInvocationHeaders(t, r.Header, int(calls.Add(1)))
		w.Header().Set("X-Request-Id", "business-error-id")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"code":500123,"msg":"existing failure","data":null}`)
	}))
	defer server.Close()
	runtime, _, _ := testRuntime(t)
	runtime.HTTP = server.Client()
	addInvocationTestProfile(t, runtime, server.URL)
	runtime.InspectEnvironment = func(context.Context, int) invocation.Result { return testInvocationReport(1) }
	err := Execute(context.Background(), runtime, []string{"checklist", "create", "--data", `{"name":"test","reviewRuleIds":["1"]}`})
	var apiError *openplatform.APIError
	if !errors.As(err, &apiError) || apiError.Code != "500123" || apiError.Message != "existing failure" || apiError.RequestID != "business-error-id" || calls.Load() != 1 {
		t.Fatalf("business behavior changed: err=%v requests=%d", err, calls.Load())
	}
}
