package update

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
	if !result.Updated {
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
