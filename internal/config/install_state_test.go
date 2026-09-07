package config

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/filelock"
)

// TestFileInstallStateStoreLifecycle 验证首次安装门禁可持久化、完成且保持权限受控。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；状态、幂等性或权限漂移时通过 t.Fatal 报告。
func TestFileInstallStateStoreLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "install-state.json")
	store := NewFileInstallStateStore(path)
	if _, err := store.Load(); !errors.Is(err, ErrInstallStateNotFound) {
		t.Fatalf("err=%v，期望 ErrInstallStateNotFound", err)
	}
	if err := store.Save(InstallState{
		Schema: InstallStateSchema, EventID: "install-event-1", InstalledVersion: "1.2.3",
		FirstInstall: true, AuthorizationRequired: true, NextAction: "authorize",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteAuthorization(); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteAuthorization(); err != nil {
		t.Fatalf("重复完成应保持幂等: %v", err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.EventID != "install-event-1" || state.InstalledVersion != "1.2.3" || state.FirstInstall || state.AuthorizationRequired || state.NextAction != "" {
		t.Fatalf("state=%#v", state)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("安装状态文件权限=%o，期望 600", info.Mode().Perm())
	}
}

/*
TestInstallerWaitsForAuthorizationState 验证真实 Node 安装器等待 CLI 文件锁，并保留并发完成的授权。
入参：t *testing.T 为测试上下文；构建产物和安装状态均放在临时目录。
返回值：无；安装器越过锁、覆盖授权状态或遗漏版本更新时测试失败。
*/
func TestInstallerWaitsForAuthorizationState(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	binary := filepath.Join(temporary, "everyline-cli.exe")
	build := exec.Command("go", "build", "-o", binary, "./cmd/everyline-cli")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建安装夹具: %v\n%s", err, output)
	}
	if err := os.WriteFile(filepath.Join(temporary, "package.json"), []byte(`{"version":"0.0.7"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(temporary, "install-state.json")
	store := NewFileInstallStateStore(path)
	state := InstallState{Schema: InstallStateSchema, EventID: "current-install", InstalledVersion: "0.0.2", FirstInstall: true, AuthorizationRequired: true, NextAction: "authorize"}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	// 父进程模拟持锁提交授权；子进程运行真实安装器及包内 CLI，避免仅验证同进程互斥。
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, "node", "-e", `
const { ensureFirstInstallState } = require(process.argv[1]);
process.stdout.write("ready\n");
ensureFirstInstallState(process.argv[2], { EVERYLINE_CONFIG_DIR: process.argv[2] }, process.argv[2], process.platform, [{ status: "existing" }], false, process.argv[3]);
`, filepath.Join(root, "scripts", "install.js"), temporary, binary)
	var stderr bytes.Buffer
	child.Stderr = &stderr
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	err = filelock.With(path+".lock", func() error {
		if err := child.Start(); err != nil {
			return err
		}
		go func() { done <- child.Wait() }()
		if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
			return fmt.Errorf("安装器启动失败: line=%q err=%v", line, err)
		}
		select {
		case err := <-done:
			return fmt.Errorf("安装器未等待授权写锁: %v", err)
		case <-time.After(200 * time.Millisecond):
		}
		// 等价于 CompleteAuthorization 的锁内提交；安装器随后必须读到这些最新字段。
		state.FirstInstall, state.AuthorizationRequired, state.NextAction = false, false, ""
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.saveUnlocked(state)
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("安装器执行失败: %v\n%s", err, stderr.String())
		}
	case <-ctx.Done():
		t.Fatal("释放锁后安装器未完成")
	}
	stored, err := store.Load()
	if err != nil || stored.InstalledVersion != "0.0.7" || stored.EventID != state.EventID || stored.FirstInstall || stored.AuthorizationRequired || stored.NextAction != "" {
		t.Fatalf("安装更新覆盖了授权或事件: state=%+v err=%v", stored, err)
	}
}
