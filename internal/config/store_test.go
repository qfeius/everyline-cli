package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestDefaultDirResolvesRelativeOverrideFromHome 验证相对配置覆盖不受 CLI 当前工作目录影响。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；相对路径未以用户主目录为基准时通过 t.Fatal 报告。
func TestDefaultDirResolvesRelativeOverrideFromHome(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EVERYLINE_CONFIG_DIR", filepath.Join("settings", "everyline"))

	got, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(homeDir, "settings", "everyline")
	if got != want {
		t.Fatalf("config dir=%q，期望 %q", got, want)
	}
}

/*
TestFileStoreLifecycle 验证 Profile 原子持久化、默认选择、排序和 0600 权限。
入参：t *testing.T 为测试上下文。
返回值：无；失败通过 t.Fatal 报告。
*/
func TestFileStoreLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	store := NewFileStore(path)
	dev := Profile{Name: "dev", BaseURL: "https://dev-open.qtech.cn", TokenURL: "https://dev-open.qtech.cn/token", AppID: "cli-dev", DefaultOutput: "json"}
	prod := Profile{Name: "prod", BaseURL: "https://open-b.qfei.cn", TokenURL: "https://open-b.qfei.cn/token", AppID: "cli-prod", DefaultOutput: "table"}
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

// TestFileStoreDefaultsOutputToJSON 验证新 Profile 未指定输出格式时持久化为 json。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；默认值不是 json 时通过 t.Fatal 报告。
func TestFileStoreDefaultsOutputToJSON(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Add(Profile{Name: "dev", BaseURL: "https://dev-open.qtech.cn", TokenURL: "https://dev-open.qtech.cn/token", AppID: "cli-dev"}); err != nil {
		t.Fatal(err)
	}
	profile, err := store.Get("dev")
	if err != nil {
		t.Fatal(err)
	}
	if profile.DefaultOutput != "json" {
		t.Fatalf("default_output=%q，期望 json", profile.DefaultOutput)
	}
}

// TestFileStoreConcurrentInstances 验证多个独立仓库实例并发读改写时不会丢失 Profile。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestFileStoreConcurrentInstances(t *testing.T) {
	const profileCount = 40
	path := filepath.Join(t.TempDir(), "config.json")
	start := make(chan struct{})
	errors := make(chan error, profileCount)
	var waitGroup sync.WaitGroup
	for index := 0; index < profileCount; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			<-start
			name := fmt.Sprintf("profile-%02d", index)
			errors <- NewFileStore(path).Add(Profile{
				Name: name, BaseURL: "https://open.qfei.cn", TokenURL: "https://open.qfei.cn/token", AppID: name,
			})
		}(index)
	}
	close(start)
	waitGroup.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	profiles, err := NewFileStore(path).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != profileCount {
		t.Fatalf("并发写入后 profiles=%d，期望 %d", len(profiles), profileCount)
	}
}
