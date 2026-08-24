//go:build windows

package update

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDeferredReplacementHelperCompletesAfterParentExit 在真实 Windows 进程上验证独立 helper 等待父进程并替换目标文件。
// 入参：t *testing.T 为测试上下文、临时目录和进程清理管理器。
// 返回值：无；CLI 构建、helper 启动、父进程等待、目标替换或最终状态输出失败时通过 t.Fatal 报告。
func TestDeferredReplacementHelperCompletesAfterParentExit(t *testing.T) {
	moduleRoot := filepath.Clean(filepath.Join("..", ".."))
	temporaryDirectory := t.TempDir()
	targetPath := filepath.Join(temporaryDirectory, "everyline-cli.exe")
	build := exec.Command("go", "build", "-o", targetPath, "./cmd/everyline-cli")
	build.Dir = moduleRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建 Windows helper fixture: %v\n%s", err, output)
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

	// 短生命周期父进程为 helper 提供真实可等待 PID，验证替换不会在父进程退出前执行。
	parent := exec.Command("cmd.exe", "/C", "ping 127.0.0.1 -n 2 >NUL")
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if parent.Process != nil {
			_ = parent.Process.Kill()
		}
	})
	var helperStderr bytes.Buffer
	helper := exec.Command(helperPath, "__everyline-cli-replace", artifactPath, targetPath, strconv.Itoa(parent.Process.Pid), helperPath)
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
	select {
	case err := <-helperDone:
		if err != nil {
			t.Fatalf("helper 失败: %v\n%s", err, helperStderr.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("等待 Windows helper 完成超时")
	}
	_ = parent.Wait()
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
