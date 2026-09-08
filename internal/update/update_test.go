package update

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func manifest(version string, platform string, artifactURL string, checksum string) string {
	return `{"version":"` + version + `","platforms":{"` + platform + `":{"url":"` + artifactURL + `","sha256":"` + checksum + `"}}}`
}

func checksum(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

func testHTTPClient(handler func(*http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{Transport: roundTripFunc(handler)}
}

// TestCheckComparesVersionsWithoutSelectingArtifact 验证轻量版本检查只比较 manifest 版本，不要求当前平台制品。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；版本比较或网络边界不符合预期时通过 t.Fatal 报告。
func TestCheckComparesVersionsWithoutSelectingArtifact(t *testing.T) {
	client := testHTTPClient(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, manifest("1.1.0", "other-platform", "https://updates.example.test/everyline-cli", checksum("new"))), nil
	})
	result, err := Check(t.Context(), "1.0.0", "https://updates.example.test/manifest.json", client)
	if err != nil {
		t.Fatal(err)
	}
	if result.CurrentVersion != "1.0.0" || result.LatestVersion != "1.1.0" || result.IsLatest {
		t.Fatalf("result=%#v", result)
	}
}

func TestRunRejectsNonHTTPSManifestBeforeNetwork(t *testing.T) {
	called := false
	client := testHTTPClient(func(*http.Request) (*http.Response, error) {
		called = true
		return response(http.StatusOK, "{}"), nil
	})

	_, err := Run(t.Context(), "1.0.0", "http://updates.example.test/manifest.json", Options{HTTPClient: client})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("非 HTTPS manifest 不应发起网络请求")
	}
}

func TestRunRejectsDevelopmentVersionBeforeNetwork(t *testing.T) {
	called := false
	client := testHTTPClient(func(*http.Request) (*http.Response, error) {
		called = true
		return response(http.StatusOK, "{}"), nil
	})

	_, err := Run(t.Context(), "dev", "https://updates.example.test/manifest.json", Options{HTTPClient: client})
	if err == nil || !strings.Contains(err.Error(), "当前版本不可用于自更新") {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("开发版本不可比较时不应访问 manifest")
	}
}

func TestRunRejectsMissingPlatformArtifact(t *testing.T) {
	client := testHTTPClient(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, manifest("1.1.0", "linux-amd64", "https://updates.example.test/everyline-cli", checksum("new"))), nil
	})

	_, err := Run(t.Context(), "1.0.0", "https://updates.example.test/manifest.json", Options{HTTPClient: client, Platform: "darwin-arm64"})
	if err == nil || !strings.Contains(err.Error(), "未提供当前平台制品") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunReturnsNoUpdateWithoutDownloadingArtifact(t *testing.T) {
	artifactRequested := false
	client := testHTTPClient(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "manifest") {
			return response(http.StatusOK, manifest("1.0.0", runtime.GOOS+"-"+runtime.GOARCH, "https://updates.example.test/everyline-cli", checksum("same"))), nil
		}
		artifactRequested = true
		return response(http.StatusOK, "unexpected"), nil
	})

	result, err := Run(t.Context(), "1.0.0", "https://updates.example.test/manifest.json", Options{HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated || result.LatestVersion != "1.0.0" {
		t.Fatalf("result=%#v", result)
	}
	if artifactRequested {
		t.Fatal("当前版本已是最新时不应下载制品")
	}
}

func TestRunDryRunValidatesButDoesNotReplace(t *testing.T) {
	target := filepath.Join(t.TempDir(), "everyline-cli")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := testHTTPClient(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "manifest") {
			return response(http.StatusOK, manifest("1.1.0", runtime.GOOS+"-"+runtime.GOARCH, "https://updates.example.test/everyline-cli", checksum("new"))), nil
		}
		return response(http.StatusOK, "new"), nil
	})

	result, err := Run(t.Context(), "1.0.0", "https://updates.example.test/manifest.json", Options{HTTPClient: client, ExecutablePath: target, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || result.Updated {
		t.Fatalf("result=%#v", result)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old" {
		t.Fatalf("target=%q，dry-run 不应替换文件", content)
	}
}

// TestRunDownloadsVerifiesAndReplacesBinary 验证同步替换会写入新制品、保留权限，并返回 updated 而非 scheduled。
// 入参：t *testing.T 为测试上下文和临时目录管理器。
// 返回值：无；下载、替换、权限或结果状态不符合预期时通过 t.Fatal 报告。
func TestRunDownloadsVerifiesAndReplacesBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 由替换助手异步完成，独立集成测试另行覆盖")
	}
	target := filepath.Join(t.TempDir(), "everyline-cli")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	client := testHTTPClient(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "manifest") {
			return response(http.StatusOK, manifest("1.1.0", runtime.GOOS+"-"+runtime.GOARCH, "https://updates.example.test/everyline-cli", checksum("new"))), nil
		}
		return response(http.StatusOK, "new"), nil
	})

	result, err := Run(t.Context(), "1.0.0", "https://updates.example.test/manifest.json", Options{HTTPClient: client, ExecutablePath: target})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.Scheduled {
		t.Fatalf("result=%#v", result)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("target=%q", content)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode=%o，更新后应保留原文件权限", info.Mode().Perm())
	}
}

// TestRunDeferredReplacementReturnsScheduled 验证异步替换只标记 scheduled，不会在 helper 完成前宣称 updated。
// 入参：t *testing.T 为测试上下文和临时目录管理器。
// 返回值：无；状态语义、原文件或临时制品不符合预期时通过 t.Fatal 报告。
func TestRunDeferredReplacementReturnsScheduled(t *testing.T) {
	target := filepath.Join(t.TempDir(), "everyline-cli")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	client := testHTTPClient(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "manifest") {
			return response(http.StatusOK, manifest("1.1.0", runtime.GOOS+"-"+runtime.GOARCH, "https://updates.example.test/everyline-cli", checksum("new"))), nil
		}
		return response(http.StatusOK, "new"), nil
	})
	var scheduledArtifact string
	result, err := Run(t.Context(), "1.0.0", "https://updates.example.test/manifest.json", Options{
		HTTPClient:     client,
		ExecutablePath: target,
		replaceBinary: func(temporaryPath string, targetPath string) (bool, error) {
			if targetPath != resolvedTarget {
				t.Fatalf("targetPath=%q", targetPath)
			}
			scheduledArtifact = temporaryPath
			return true, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated || !result.Scheduled {
		t.Fatalf("result=%#v", result)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old" {
		t.Fatalf("target=%q，scheduled 状态下原文件尚未替换", content)
	}
	if scheduledArtifact == "" {
		t.Fatal("未捕获交给 helper 的临时制品")
	}
	t.Cleanup(func() { _ = os.Remove(scheduledArtifact) })
	if content, err := os.ReadFile(scheduledArtifact); err != nil || string(content) != "new" {
		t.Fatalf("scheduled artifact=%q err=%v", content, err)
	}
}

// TestReplaceWithRetryCoversSuccessAndFailure 验证 deferred helper 会重试瞬时失败，并保留耗尽重试后的最终错误。
// 入参：t *testing.T 为断言上下文。
// 返回值：无；重试次数、等待次数或错误链不符合预期时通过 t.Fatal 报告。
func TestReplaceWithRetryCoversSuccessAndFailure(t *testing.T) {
	attempts := 0
	waits := 0
	err := replaceWithRetry("temporary", "target", 3, 0, func(string, string) error {
		attempts++
		if attempts < 3 {
			return errors.New("target locked")
		}
		return nil
	}, func(time.Duration) {
		waits++
	})
	if err != nil || attempts != 3 || waits != 2 {
		t.Fatalf("err=%v attempts=%d waits=%d", err, attempts, waits)
	}

	lastErr := errors.New("rename failed")
	err = replaceWithRetry("temporary", "target", 2, 0, func(string, string) error {
		return lastErr
	}, func(time.Duration) {})
	if !errors.Is(err, lastErr) {
		t.Fatalf("err=%v，期望保留最后一次 rename 错误", err)
	}
}

func TestRunChecksumFailurePreservesBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 由替换助手异步完成，独立集成测试另行覆盖")
	}
	target := filepath.Join(t.TempDir(), "everyline-cli")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := testHTTPClient(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "manifest") {
			return response(http.StatusOK, manifest("1.1.0", runtime.GOOS+"-"+runtime.GOARCH, "https://updates.example.test/everyline-cli", strings.Repeat("0", 64))), nil
		}
		return response(http.StatusOK, "tampered"), nil
	})

	_, err := Run(t.Context(), "1.0.0", "https://updates.example.test/manifest.json", Options{HTTPClient: client, ExecutablePath: target})
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("err=%v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old" {
		t.Fatalf("target=%q，校验失败时原文件必须保留", content)
	}
}

func TestRunRejectsNPMWrapperWithoutNetwork(t *testing.T) {
	called := false
	client := testHTTPClient(func(*http.Request) (*http.Response, error) {
		called = true
		return response(http.StatusOK, "{}"), nil
	})

	_, err := Run(t.Context(), "1.0.0", "https://updates.example.test/manifest.json", Options{HTTPClient: client, Wrapper: true})
	if err == nil || !strings.Contains(err.Error(), "npm") {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("npm wrapper 模式不应访问 manifest")
	}
}

/*
TestCheckNPM 验证 npm 版本比较及失败边界。
入参：t *testing.T 为测试上下文。
返回值：无，响应解析或版本判断错误时报告失败。
*/
func TestCheckNPM(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		latest bool
		fail   bool
	}{
		{`{"version":"1.1.0"}`, 200, false, false},
		{`{"version":"1.0.0"}`, 200, true, false},
		{`{"version":"0.9.0"}`, 200, true, false},
		{`{}`, 200, false, true},
		{`invalid`, 200, false, true},
		{`{}`, 404, false, true},
	} {
		t.Run(tc.body+http.StatusText(tc.status), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://registry.npmjs.org/@qfeius%2feveryline-cli/latest" {
					t.Fatalf("unexpected URL: %s", r.URL)
				}
				return response(tc.status, tc.body), nil
			})}
			got, err := CheckNPM(t.Context(), "1.0.0", client)
			if (err != nil) != tc.fail {
				t.Fatalf("err=%v", err)
			}
			if !tc.fail && got.IsLatest != tc.latest {
				t.Fatalf("result=%+v", got)
			}
		})
	}
}
