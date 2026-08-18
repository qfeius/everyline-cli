package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestFileStoreLifecycle 验证 Profile 原子持久化、默认选择、排序和 0600 权限。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestFileStoreLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	store := NewFileStore(path)
	dev := Profile{Name: "dev", BaseURL: "https://dev.example.com", TokenURL: "https://dev.example.com/token", AppID: "cli-dev", DefaultOutput: "json"}
	prod := Profile{Name: "prod", BaseURL: "https://example.com", TokenURL: "https://example.com/token", AppID: "cli-prod", DefaultOutput: "table"}
	if err := store.Add(prod); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(dev); err != nil {
		t.Fatal(err)
	}
	profiles, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || profiles[0].Name != "dev" || profiles[1].Name != "prod" {
		t.Fatalf("profiles 未稳定排序: %#v", profiles)
	}
	if err := store.Use("dev"); err != nil {
		t.Fatal(err)
	}
	current, err := store.Current()
	if err != nil || current.Name != "dev" {
		t.Fatalf("current=%#v err=%v", current, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("配置文件权限=%o，期望 600", info.Mode().Perm())
	}
}

// TestFileStoreMissingProfile 验证切换不存在 Profile 返回可识别错误。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestFileStoreMissingProfile(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Use("missing"); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("err=%v，期望 ErrProfileNotFound", err)
	}
}
