package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"git.qtech.cn/ai/everyline-cli/internal/filelock"
)

var ErrAppSecretNotFound = errors.New("app secret 不存在")

const defaultKeychainService = "everyline-cli.app-secret"

// AppSecretStore 定义按 Profile 保存 app secret 的最小能力。
type AppSecretStore interface {
	LoadAppSecret(string) (string, error)
	SaveAppSecret(string, string) error
}

// securityCommandRunner 是 macOS security 命令的可测试执行边界。
type securityCommandRunner func(context.Context, []string, io.Reader) ([]byte, error)

// KeychainSecretStore 使用 macOS 默认 Keychain 保存 app secret。
type KeychainSecretStore struct {
	service string
	run     securityCommandRunner
}

// NewKeychainSecretStore 创建 macOS Keychain app secret 仓库。
func NewKeychainSecretStore(service string) *KeychainSecretStore {
	if strings.TrimSpace(service) == "" {
		service = defaultKeychainService
	}
	return &KeychainSecretStore{service: service, run: runSecurityCommand}
}

// LoadAppSecret 从 macOS Keychain 读取指定 Profile 的 app secret。
func (store *KeychainSecretStore) LoadAppSecret(profileName string) (string, error) {
	if strings.TrimSpace(profileName) == "" {
		return "", fmt.Errorf("读取 app secret: profile 不能为空")
	}
	output, err := store.run(context.Background(), []string{
		"find-generic-password",
		"-a", profileName,
		"-s", store.service,
		"-w",
	}, nil)
	if err != nil {
		return "", fmt.Errorf("读取 macOS Keychain app secret: %w", err)
	}
	secret := strings.TrimSuffix(string(output), "\n")
	secret = strings.TrimSuffix(secret, "\r")
	if secret == "" {
		return "", ErrAppSecretNotFound
	}
	return secret, nil
}

// SaveAppSecret 将 app secret 写入 macOS Keychain；secret 只通过 stdin 传给 security 命令。
func (store *KeychainSecretStore) SaveAppSecret(profileName, secret string) error {
	if strings.TrimSpace(profileName) == "" {
		return fmt.Errorf("保存 app secret: profile 不能为空")
	}
	if secret == "" {
		return fmt.Errorf("保存 app secret: secret 不能为空")
	}
	_, err := store.run(context.Background(), []string{
		"add-generic-password",
		"-a", profileName,
		"-s", store.service,
		"-U",
		"-w",
	}, strings.NewReader(secret+"\n"))
	if err != nil {
		return fmt.Errorf("写入 macOS Keychain app secret: %w", err)
	}
	return nil
}

func runSecurityCommand(ctx context.Context, args []string, stdin io.Reader) ([]byte, error) {
	command := exec.CommandContext(ctx, "security", args...)
	command.Stdin = stdin
	return command.Output()
}

// FallbackSecretStore 按主存储优先、备用存储回退的顺序管理 app secret。
type FallbackSecretStore struct {
	primary  AppSecretStore
	fallback AppSecretStore
}

// NewFallbackSecretStore 创建主存储失败时回退到备用存储的组合器。
func NewFallbackSecretStore(primary, fallback AppSecretStore) *FallbackSecretStore {
	return &FallbackSecretStore{primary: primary, fallback: fallback}
}

// LoadAppSecret 先读取主存储；主存储失败时读取备用存储。
func (store *FallbackSecretStore) LoadAppSecret(profileName string) (string, error) {
	if store.primary != nil {
		if secret, err := store.primary.LoadAppSecret(profileName); err == nil {
			return secret, nil
		}
	}
	if store.fallback != nil {
		return store.fallback.LoadAppSecret(profileName)
	}
	return "", ErrAppSecretNotFound
}

// SaveAppSecret 先写入主存储；主存储失败时写入备用存储。
func (store *FallbackSecretStore) SaveAppSecret(profileName, secret string) error {
	if store.primary != nil {
		if err := store.primary.SaveAppSecret(profileName, secret); err == nil {
			return nil
		}
	}
	if store.fallback != nil {
		return store.fallback.SaveAppSecret(profileName, secret)
	}
	return fmt.Errorf("保存 app secret: 没有可用的本地存储")
}

type appSecretFile struct {
	AppSecrets map[string]string `json:"app_secrets"`
}

// FileSecretStore 将 app secret 保存在权限受控的独立 JSON 文件中。
type FileSecretStore struct {
	path string
	mu   sync.Mutex
}

// NewFileSecretStore 创建 app secret 文件仓库。
func NewFileSecretStore(path string) *FileSecretStore {
	return &FileSecretStore{path: path}
}

// NewDefaultAppSecretStore 创建当前平台的默认 app secret 仓库。
// macOS 使用 Keychain 优先、secrets.json 回退；其他平台直接使用 secrets.json。
func NewDefaultAppSecretStore(configDir string) AppSecretStore {
	fileStore := NewFileSecretStore(filepath.Join(configDir, "secrets.json"))
	if runtime.GOOS != "darwin" {
		return fileStore
	}
	return NewFallbackSecretStore(NewKeychainSecretStore(defaultKeychainService), fileStore)
}

// LoadAppSecret 读取指定 Profile 的 app secret；未配置时返回 ErrAppSecretNotFound。
func (store *FileSecretStore) LoadAppSecret(profileName string) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	secrets, err := store.loadUnlocked()
	if err != nil {
		return "", err
	}
	secret, ok := secrets.AppSecrets[profileName]
	if !ok || secret == "" {
		return "", ErrAppSecretNotFound
	}
	return secret, nil
}

// SaveAppSecret 原子保存指定 Profile 的 app secret，并保持文件为 0600。
func (store *FileSecretStore) SaveAppSecret(profileName, secret string) error {
	if strings.TrimSpace(profileName) == "" {
		return fmt.Errorf("保存 app secret: profile 不能为空")
	}
	if secret == "" {
		return fmt.Errorf("保存 app secret: secret 不能为空")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return filelock.With(store.path+".lock", func() error {
		secrets, err := store.loadUnlocked()
		if err != nil {
			return err
		}
		secrets.AppSecrets[profileName] = secret
		return store.saveUnlocked(secrets)
	})
}

func (store *FileSecretStore) loadUnlocked() (appSecretFile, error) {
	secrets := appSecretFile{AppSecrets: map[string]string{}}
	content, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return secrets, nil
	}
	if err != nil {
		return secrets, fmt.Errorf("读取 app secret 文件: %w", err)
	}
	if err := json.Unmarshal(content, &secrets); err != nil {
		return secrets, fmt.Errorf("解析 app secret 文件: %w", err)
	}
	if secrets.AppSecrets == nil {
		secrets.AppSecrets = map[string]string{}
	}
	return secrets, nil
}

func (store *FileSecretStore) saveUnlocked(secrets appSecretFile) error {
	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("创建 app secret 目录: %w", err)
	}
	if directory != "." {
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("设置 app secret 目录权限: %w", err)
		}
	}
	content, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return fmt.Errorf("编码 app secret 文件: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".secrets-*.tmp")
	if err != nil {
		return fmt.Errorf("创建 app secret 临时文件: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("设置 app secret 文件权限: %w", err)
	}
	if _, err := temporary.Write(append(content, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("写入 app secret 文件: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭 app secret 文件: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("替换 app secret 文件: %w", err)
	}
	return nil
}
