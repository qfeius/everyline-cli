package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

/*
TestFileStoreUnchangedUpdatesKeepFile 验证重复设置保留配置文件本体、完整内容及默认选择，避免无意义的原子替换。
入参：t *testing.T 为测试上下文。
返回值：无；无变化的操作替换文件或改变内容时通过 t.Fatal 报告。
*/
func TestFileStoreUnchangedUpdatesKeepFile(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		update func(*FileStore, Profile) error
	}{
		{name: "add with default fields", update: func(store *FileStore, profile Profile) error {
			profile.DefaultOutput = ""
			profile.DefaultIdentity = ""
			return store.Add(profile)
		}},
		{name: "add with equal scopes", scopes: []string{"read", "write"}, update: func(store *FileStore, profile Profile) error {
			profile.OAuthScopes = append([]string(nil), profile.OAuthScopes...)
			return store.Add(profile)
		}},
		{name: "add with empty scopes", update: func(store *FileStore, profile Profile) error {
			profile.OAuthScopes = []string{}
			return store.Add(profile)
		}},
		{name: "use current", update: func(store *FileStore, _ Profile) error {
			return store.Use("current")
		}},
		{name: "set normalized identity", update: func(store *FileStore, profile Profile) error {
			return store.SetDefaultIdentity(profile.Name, " APP ")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			store := NewFileStore(path)
			profile := Profile{Name: "target", BaseURL: "https://open.qfei.cn", TokenURL: "https://open.qfei.cn/token", AppID: "cli", OAuthScopes: test.scopes}
			current := profile
			current.Name = "current"
			// 先建立另一默认环境，确保重复更新 target 不会改变 current。
			if err := store.Add(current); err != nil {
				t.Fatal(err)
			}
			if err := store.Add(profile); err != nil {
				t.Fatal(err)
			}
			profile, err := store.Get(profile.Name)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			contentBefore, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			if err := test.update(store, profile); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) {
				t.Error("无变化的更新替换了配置文件")
			}
			contentAfter, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(contentBefore, contentAfter) {
				t.Error("无变化的更新改变了配置内容")
			}
		})
	}
}

/*
TestFileStoreAddRestoresMissingCurrent 验证同名配置内容一致但未选择默认环境时仍补齐 current。
入参：t *testing.T 为测试上下文。
返回值：无；默认环境未持久化时通过 t.Fatal 报告。
*/
func TestFileStoreAddRestoresMissingCurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	profile := Profile{Name: "target", BaseURL: "https://open.qfei.cn", TokenURL: "https://open.qfei.cn/token", AppID: "cli", DefaultOutput: "json", DefaultIdentity: IdentityApp}
	// 直接构造未选择 current 的已有配置，区分完整无变化与仍需补齐默认选择。
	content, err := json.Marshal(fileData{Profiles: map[string]Profile{profile.Name: profile}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewFileStore(path).Add(profile); err != nil {
		t.Fatal(err)
	}
	current, err := NewFileStore(path).Current()
	if err != nil || current.Name != profile.Name {
		t.Fatalf("current=%#v err=%v", current, err)
	}
}

/*
TestFileStoreChangedUpdatesPersist 验证跳过无变化写入后，同名 Profile 及身份的实际变更仍持久化并保留其他字段。
入参：t *testing.T 为测试上下文。
返回值：无；变更丢失或无关字段被修改时通过 t.Fatal 报告。
*/
func TestFileStoreChangedUpdatesPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := NewFileStore(path)
	profile := Profile{Name: "target", BaseURL: "https://open.qfei.cn", TokenURL: "https://open.qfei.cn/token", AppID: "cli", DefaultOutput: "json", DefaultIdentity: IdentityApp, OAuthScopes: []string{"read"}}
	if err := store.Add(profile); err != nil {
		t.Fatal(err)
	}
	profile.OAuthScopes = []string{"read", "write"}
	if err := store.Add(profile); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDefaultIdentity(profile.Name, IdentityUser); err != nil {
		t.Fatal(err)
	}
	want := profile
	want.DefaultIdentity = IdentityUser
	got, err := NewFileStore(path).Get(profile.Name)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("profile=%s，期望 %s", gotJSON, wantJSON)
	}
}

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
