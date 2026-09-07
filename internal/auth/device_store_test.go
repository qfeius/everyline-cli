package auth

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/config"
)

// fakeDeviceKeyring 为 WorkBuddy 存储测试提供内存系统凭证库。
type fakeDeviceKeyring struct {
	mu     sync.Mutex
	values map[string]string
}

// Get 读取 service/user 组合键。
// 入参：service/user string 为系统凭证定位信息。
// 返回值：string 为 JSON；error 为不存在。
func (keyring *fakeDeviceKeyring) Get(service, user string) (string, error) {
	keyring.mu.Lock()
	defer keyring.mu.Unlock()
	value, ok := keyring.values[service+":"+user]
	if !ok {
		return "", ErrDeviceCredentialNotFound
	}
	return value, nil
}

// Set 保存 service/user 组合键。
// 入参：service/user/value string 为定位信息和 JSON。
// 返回值：error，始终为 nil。
func (keyring *fakeDeviceKeyring) Set(service, user, value string) error {
	keyring.mu.Lock()
	defer keyring.mu.Unlock()
	keyring.values[service+":"+user] = value
	return nil
}

// Delete 删除 service/user 组合键。
// 入参：service/user string 为定位信息。
// 返回值：error，不存在时返回 ErrDeviceCredentialNotFound。
func (keyring *fakeDeviceKeyring) Delete(service, user string) error {
	keyring.mu.Lock()
	defer keyring.mu.Unlock()
	key := service + ":" + user
	if _, ok := keyring.values[key]; !ok {
		return ErrDeviceCredentialNotFound
	}
	delete(keyring.values, key)
	return nil
}

// TestDoubaoDeviceCredentialStoreEncryptsContent 验证 AgentKit 凭证落盘不出现 token 明文且可恢复 Profile。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；加密、权限或往返失败时通过 t.Fatal 报告。
func TestDoubaoDeviceCredentialStoreEncryptsContent(t *testing.T) {
	workspace := t.TempDir()
	encodedKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	store, err := NewDeviceCredentialStore(DeviceCredentialOptions{LookupEnv: func(name string) (string, bool) {
		switch name {
		case "SKILL_SESSION_WORKSPACE":
			return workspace, true
		case deviceCredentialKeyEnvironment:
			return encodedKey, true
		default:
			return "", false
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{Name: "test-user", BaseURL: "https://test-open.qtech.cn", TokenURL: "https://test-open.qtech.cn/token", DefaultIdentity: config.IdentityUser, DefaultOutput: "json"}
	want := DeviceCredential{Profile: &profile, Token: &Token{AccessToken: "plain-access-secret", RefreshToken: "plain-refresh-secret", ExpiresAt: time.Now().Add(time.Hour)}}
	if err := store.Save("test-user", want); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(workspace, ".everyline-cli", "credentials", "*.json.enc"))
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "plain-access-secret") || strings.Contains(string(content), "plain-refresh-secret") {
		t.Fatalf("加密凭证包含 token 明文: %q", content)
	}
	got, err := store.Load("test-user")
	if err != nil || got.Token == nil || got.Token.RefreshToken != want.Token.RefreshToken || got.Profile == nil || got.Profile.Name != "test-user" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

// TestWorkBuddyDeviceCredentialStoreUsesSessionNamespace 验证不同 WorkBuddy 会话不会读取彼此凭证。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；会话隔离失效时通过 t.Fatal 报告。
func TestWorkBuddyDeviceCredentialStoreUsesSessionNamespace(t *testing.T) {
	backend := &fakeDeviceKeyring{values: map[string]string{}}
	newStore := func(sessionID string) DeviceCredentialStore {
		store, err := NewDeviceCredentialStore(DeviceCredentialOptions{
			LookupEnv: func(name string) (string, bool) {
				if name == "CODEBUDDY_SESSION_ID" {
					return sessionID, true
				}
				return "", false
			},
			Keyring: backend,
		})
		if err != nil {
			t.Fatal(err)
		}
		return store
	}
	first := newStore("session-a")
	second := newStore("session-b")
	if err := first.Save("test-user", DeviceCredential{Token: &Token{AccessToken: "session-a-token"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Load("test-user"); !errors.Is(err, ErrDeviceCredentialNotFound) {
		t.Fatalf("second session err=%v", err)
	}
	got, err := first.Load("test-user")
	if err != nil || got.Token == nil || got.Token.AccessToken != "session-a-token" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

// TestWorkBuddyDeviceCredentialStoreIgnoresGenericSessionDrift 验证固定 WorkBuddy 标识优先于每次调用可能变化的通用 SESSION_ID。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；相同 WorkBuddy 授权事务因通用会话变化而丢失时通过 t.Fatal 报告。
func TestWorkBuddyDeviceCredentialStoreIgnoresGenericSessionDrift(t *testing.T) {
	backend := &fakeDeviceKeyring{values: map[string]string{}}
	newStore := func(genericSessionID string) DeviceCredentialStore {
		store, err := NewDeviceCredentialStore(DeviceCredentialOptions{
			LookupEnv: func(name string) (string, bool) {
				switch name {
				case "CODEBUDDY_SESSION_ID":
					return "fixed-workbuddy-session", true
				case "SESSION_ID":
					return genericSessionID, true
				default:
					return "", false
				}
			},
			Keyring: backend,
		})
		if err != nil {
			t.Fatal(err)
		}
		return store
	}

	initStore := newStore("tool-call-a")
	completeStore := newStore("tool-call-b")
	want := DeviceCredential{Pending: &DevicePendingTransaction{DeviceCode: "pending-device-code"}}
	if err := initStore.Save("test-user", want); err != nil {
		t.Fatal(err)
	}
	got, err := completeStore.Load("test-user")
	if err != nil || got.Pending == nil || got.Pending.DeviceCode != want.Pending.DeviceCode {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}
