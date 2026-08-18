package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestFileStoreLifecycle 验证 Profile 原子持久化、默认选择、排序和 0600 权限。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestFileStoreLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	store := NewFileStore(path)
	dev := Profile{Name: "dev", BaseURL: "https://dev-open.qtech.cn", TokenURL: "https://dev-open.qtech.cn/token", AppID: "cli-dev", DefaultOutput: "json"}
	prod := Profile{Name: "prod", BaseURL: "https://blue-open.qtech.cn", TokenURL: "https://blue-open.qtech.cn/token", AppID: "cli-prod", DefaultOutput: "table"}
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
