//go:build windows

package update

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDeferredReplacementHelperCompletesAfterTargetExit 在真实 Windows 进程上验证 helper 重试被占用目标，并在目标退出后完成替换。
// 入参：t *testing.T 为测试上下文、临时目录和进程清理管理器。
// 返回值：无；CLI 构建、目标锁定、helper 重试、目标替换或最终状态输出失败时通过 t.Fatal 报告。
func TestDeferredReplacementHelperCompletesAfterTargetExit(t *testing.T) {
	moduleRoot := filepath.Clean(filepath.Join("..", ".."))
	temporaryDirectory := t.TempDir()
	targetPath := filepath.Join(temporaryDirectory, "everyline-cli.exe")
	build := exec.Command("go", "build", "-o", targetPath, "./cmd/everyline-cli")
	build.Dir = moduleRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建 Windows helper fixture: %v\n%s", err, output)
	}
	// auth login 从保持打开的 stdin 读取 secret，可让真实目标 EXE 稳定处于运行和文件锁定状态。
	configDirectory := filepath.Join(temporaryDirectory, "config")
	fixtureEnvironment := append(os.Environ(), "EVERYLINE_CONFIG_DIR="+configDirectory)
	configure := exec.Command(targetPath, "config", "add", "locked", "--base-url", "http://127.0.0.1:1", "--token-url", "http://127.0.0.1:1/token", "--app-id", "fixture-app")
	configure.Env = fixtureEnvironment
	if output, err := configure.CombinedOutput(); err != nil {
		t.Fatalf("创建 Windows 锁定测试 Profile: %v\n%s", err, output)
	}
	helperPath, err := createReplacementHelper(targetPath, temporaryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(helperPath) })
	artifactPath := filepath.Join(temporaryDirectory, "downloaded.exe")
	if err := os.WriteFile(artifactPath, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	var targetStderr bytes.Buffer
	target := exec.Command(targetPath, "auth", "login", "--profile", "locked", "--as", "app", "--app-secret-stdin")
	target.Env = fixtureEnvironment
	target.Stderr = &targetStderr
	targetStdin, err := target.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = targetStdin.Close()
		if target.Process != nil {
			_ = target.Process.Kill()
		}
	})
	var helperStderr bytes.Buffer
	helper := exec.Command(helperPath, "__everyline-cli-replace", artifactPath, targetPath, helperPath)
	helper.Stderr = &helperStderr
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if helper.Process != nil {
			_ = helper.Process.Kill()
		}
	})
	helperDone := make(chan error, 1)
	go func() { helperDone <- helper.Wait() }()
	// helper 必须在目标运行期间持续重试，不能提前失败或声称完成。
	select {
	case err := <-helperDone:
		t.Fatalf("目标仍运行时 helper 提前结束: %v\n%s", err, helperStderr.String())
	case <-time.After(600 * time.Millisecond):
	}
	if err := targetStdin.Close(); err != nil {
		t.Fatal(err)
	}
	targetDone := make(chan error, 1)
	go func() { targetDone <- target.Wait() }()
	select {
	case <-targetDone:
	case <-time.After(5 * time.Second):
		_ = target.Process.Kill()
		t.Fatalf("等待被锁定目标退出超时: %s", targetStderr.String())
	}
	select {
	case err := <-helperDone:
		if err != nil {
			t.Fatalf("helper 失败: %v\n%s", err, helperStderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("等待 Windows helper 完成超时")
	}
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "replacement" {
		t.Fatalf("target=%q", content)
	}
	if !strings.Contains(helperStderr.String(), "已完成二进制替换") {
		t.Fatalf("helper stderr=%q", helperStderr.String())
	}
}
