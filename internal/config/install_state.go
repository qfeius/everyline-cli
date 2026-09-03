package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"git.qtech.cn/ai/everyline-cli/internal/filelock"
)

const InstallStateSchema = "everyline.install-state.v1"

var ErrInstallStateNotFound = errors.New("安装状态不存在")

// InstallState 保存安装器与 CLI 共享的首次安装授权门禁，不包含任何凭证。
type InstallState struct {
	Schema                string `json:"schema"`
	EventID               string `json:"eventId"`
	InstalledVersion      string `json:"installedVersion,omitempty"`
	FirstInstall          bool   `json:"firstInstall"`
	AuthorizationRequired bool   `json:"authorizationRequired"`
	NextAction            string `json:"nextAction,omitempty"`
}

// InstallStateStore 定义首次安装授权状态的最小持久化能力。
type InstallStateStore interface {
	Load() (InstallState, error)
	Save(InstallState) error
	CompleteAuthorization() error
}

// FileInstallStateStore 使用权限受控的 JSON 文件保存首次安装状态。
type FileInstallStateStore struct {
	path string
	mu   sync.Mutex
}

// NewFileInstallStateStore 创建指定路径的首次安装状态仓库。
// 入参：path string 为 install-state.json 的绝对或相对路径。
// 返回值：*FileInstallStateStore，可读取和完成首次安装授权门禁。
func NewFileInstallStateStore(path string) *FileInstallStateStore {
	return &FileInstallStateStore{path: path}
}

// Load 读取安装器生成的首次安装状态，并校验协议版本和必需字段。
// 入参：无。
// 返回值：InstallState 为当前状态；error 为不存在、读取、解析或校验失败。
func (store *FileInstallStateStore) Load() (InstallState, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.loadUnlocked()
}

// Save 原子保存完整首次安装状态，供安装器兼容测试和状态迁移使用。
// 入参：state InstallState 为完整状态。
// 返回值：error，校验、编码或落盘失败时非 nil。
func (store *FileInstallStateStore) Save(state InstallState) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return filelock.With(store.path+".lock", func() error {
		return store.saveUnlocked(state)
	})
}

// CompleteAuthorization 在新授权成功后解除首次安装门禁，并保留事件和安装版本用于审计。
// 入参：无。
// 返回值：error，状态不存在时幂等成功，读取或落盘失败时非 nil。
func (store *FileInstallStateStore) CompleteAuthorization() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return filelock.With(store.path+".lock", func() error {
		state, err := store.loadUnlocked()
		if errors.Is(err, ErrInstallStateNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !state.AuthorizationRequired && !state.FirstInstall {
			return nil
		}
		state.FirstInstall = false
		state.AuthorizationRequired = false
		state.NextAction = ""
		return store.saveUnlocked(state)
	})
}

// loadUnlocked 从磁盘读取并校验状态；调用方必须持有 store.mu。
// 入参：无。
// 返回值：InstallState 为已校验状态；error 为不存在、读取、解析或协议错误。
func (store *FileInstallStateStore) loadUnlocked() (InstallState, error) {
	content, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return InstallState{}, ErrInstallStateNotFound
	}
	if err != nil {
		return InstallState{}, fmt.Errorf("读取安装状态: %w", err)
	}
	var state InstallState
	if err := json.Unmarshal(content, &state); err != nil {
		return InstallState{}, fmt.Errorf("解析安装状态: %w", err)
	}
	if state.Schema != InstallStateSchema {
		return InstallState{}, fmt.Errorf("安装状态协议不受支持: %q", state.Schema)
	}
	if state.EventID == "" {
		return InstallState{}, fmt.Errorf("安装状态缺少 eventId")
	}
	return state, nil
}

// saveUnlocked 校验并原子替换状态文件，固定目录 0700、文件 0600 权限。
// 入参：state InstallState 为待持久化状态；调用方必须持有 store.mu。
// 返回值：error，校验、编码或文件操作失败时非 nil。
func (store *FileInstallStateStore) saveUnlocked(state InstallState) error {
	if state.Schema == "" {
		state.Schema = InstallStateSchema
	}
	if state.Schema != InstallStateSchema {
		return fmt.Errorf("安装状态协议不受支持: %q", state.Schema)
	}
	if state.EventID == "" {
		return fmt.Errorf("安装状态缺少 eventId")
	}
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return fmt.Errorf("创建安装状态目录: %w", err)
	}
	if err := os.Chmod(filepath.Dir(store.path), 0o700); err != nil {
		return fmt.Errorf("设置安装状态目录权限: %w", err)
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("编码安装状态: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(store.path), ".install-state-*.tmp")
	if err != nil {
		return fmt.Errorf("创建安装状态临时文件: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("设置安装状态文件权限: %w", err)
	}
	if _, err := temporary.Write(append(content, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("写入安装状态: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭安装状态文件: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("替换安装状态文件: %w", err)
	}
	return nil
}
