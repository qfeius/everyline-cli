package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/cli"
	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/invocation"
)

// 编译实际入口，验证 os.Executable 辅助进程协议，而非仅验证 mock 探测器。
// HTTP 仅访问本机测试服务器，配置与凭证全部隔离在临时目录。
func TestProductionBinaryInspectionAndRequestHeaders(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "everyline-cli")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/everyline-cli")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	configDir := filepath.Join(directory, "isolated-profile")
	t.Setenv("EVERYLINE_CONFIG_DIR", configDir)
	t.Setenv("EVERYLINE_ACCESS_TOKEN", "binary-test-token")
	// 私有辅助入口不允许读取/初始化用户配置，更不能触发认证或业务请求。
	helper := exec.CommandContext(ctx, binary, "--internal-invocation-inspect", "1")
	output, err := helper.Output()
	if err != nil {
		t.Fatalf("helper: %v", err)
	}
	var report invocation.Result
	if err := json.NewDecoder(bytes.NewReader(output)).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if report.ProductCode != "contract-review" || report.ChannelType != "cli" || len(report.Processes) != 1 ||
		report.Processes[0].PID != int32(os.Getpid()) {
		t.Fatalf("wrong helper dispatch: %+v", report)
	}
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("helper touched config directory: %v", err)
	}

	captured := make(chan http.Header, 1)
	var failRequest atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Header.Clone()
		if failRequest.Load() {
			w.Header().Set("X-Request-Id", "server-error-id")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"code":400,"msg":"invalid query"}`)
			return
		}
		fmt.Fprint(w, `{"code":200,"data":[]}`)
	}))
	defer server.Close()
	store := config.NewFileStore(filepath.Join(configDir, "config.json"))
	if err := store.Add(config.Profile{Name: "local", BaseURL: server.URL, TokenURL: server.URL + "/token",
		AppID: "test-app", DefaultOutput: "json"}); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, binary, "checklist", "list", "--profile", "local", "--verbose", "--output", "json")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("request: %v %s", err, &stderr)
	}
	if !json.Valid(stdout.Bytes()) {
		t.Fatalf("stdout polluted: %s", &stdout)
	}
	tracePattern := regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-01$`)
	var successfulTrace string
	select {
	case headers := <-captured:
		parts := tracePattern.FindStringSubmatch(headers.Get("traceparent"))
		if parts == nil || parts[1] != headers.Get("X-Log-Id") {
			t.Fatalf("binary did not send matching Trace headers: %v", headers)
		}
		successfulTrace = parts[1]
		if headers.Get(invocation.HeaderChannelType) != "cli" || headers.Get(invocation.HeaderProductCode) != "contract-review" ||
			headers.Get(invocation.HeaderDetectorVersion) != invocation.DetectorVersion ||
			headers.Get(invocation.HeaderAgentSourceType) == "" || headers.Get(invocation.HeaderConfidence) == "" ||
			headers.Get(invocation.HeaderEvidenceType) == "" || headers.Get("Authorization") != "Bearer binary-test-token" {
			t.Fatalf("binary did not send expected metadata: %v", headers)
		}
	case <-time.After(time.Second):
		t.Fatal("no business request received")
	}
	if !strings.Contains(stderr.String(), "invocation_source ") || !strings.Contains(stderr.String(), "request_trace ") ||
		!strings.Contains(stderr.String(), successfulTrace) || strings.Contains(stderr.String(), "binary-test-token") {
		t.Fatalf("invalid diagnostics: %s", &stderr)
	}
	// 真实入口在未开 verbose 时也必须返回可定位的错误，且保留 API 退出码。
	failRequest.Store(true)
	stdout.Reset()
	stderr.Reset()
	failedCommand := exec.CommandContext(ctx, binary, "checklist", "list", "--profile", "local", "--output", "json")
	failedCommand.Stdout, failedCommand.Stderr = &stdout, &stderr
	var exitError *exec.ExitError
	if err := failedCommand.Run(); !errors.As(err, &exitError) || exitError.ExitCode() != cli.ExitAPI {
		t.Fatalf("wrong failure exit: %v stderr=%s", err, &stderr)
	}
	select {
	case headers := <-captured:
		failedTrace := headers.Get("X-Log-Id")
		if failedTrace == "" || failedTrace == successfulTrace || !strings.Contains(stderr.String(), "trace_id="+failedTrace) ||
			!strings.Contains(stderr.String(), "request_id=server-error-id") || strings.Contains(stderr.String(), "binary-test-token") ||
			strings.Contains(stderr.String(), "request_trace ") || stdout.Len() != 0 {
			t.Fatalf("invalid error output: stdout=%s stderr=%s", &stdout, &stderr)
		}
	case <-time.After(time.Second):
		t.Fatal("no failed business request received")
	}
	if _, err := os.Stat(filepath.Join(configDir, "tokens.json")); !os.IsNotExist(err) {
		t.Fatalf("environment token caused unexpected token-store mutation: %v", err)
	}
}
