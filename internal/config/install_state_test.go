package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
