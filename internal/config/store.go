package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"git.qtech.cn/ai/everyline-cli/internal/filelock"
)

var (
	ErrProfileNotFound = errors.New("profile 不存在")
	ErrNoActiveProfile = errors.New("尚未选择 profile")
)

// fileData 是配置文件的持久化结构，profiles 按名称索引以保证更新幂等。
type fileData struct {
	Current  string             `json:"current_profile"`
	Profiles map[string]Profile `json:"profiles"`
}

// Store 定义 Profile 的最小持久化能力，便于 CLI 测试替换为临时目录。
type Store interface {
	Add(Profile) error
	List() ([]Profile, error)
	Use(string) error
	SetDefaultIdentity(string, IdentityKind) error
	Get(string) (Profile, error)
	Current() (Profile, error)
}

// FileStore 使用权限受控的 JSON 文件保存非敏感 Profile 配置。
type FileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore 创建指定路径的文件配置仓库。
// 入参：path string 为 config.json 的绝对或相对路径。
// 返回值：*FileStore，可用于读写 Profile。
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

// DefaultDir 返回 CLI 配置目录，测试可用 EVERYLINE_CONFIG_DIR 覆盖。
// 入参：无。
// 返回值：string，配置目录路径；error，无法解析用户主目录时非 nil。
func DefaultDir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("EVERYLINE_CONFIG_DIR")); configured != "" {
		return configured, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("解析用户目录: %w", err)
	}
	return filepath.Join(homeDir, ".everyline-cli"), nil
}

// Add 新增或覆盖同名 Profile，并保持现有默认选择不变。
// 入参：profile Profile 为要持久化的非敏感连接配置。
// 返回值：error，校验或落盘失败时非 nil。
func (store *FileStore) Add(profile Profile) error {
	if profile.DefaultOutput == "" {
		profile.DefaultOutput = "json"
	}
	if profile.DefaultIdentity == "" {
		profile.DefaultIdentity = IdentityApp
	}
	if err := profile.ValidateForIdentity(profile.DefaultIdentity); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()

	return filelock.With(store.path+".lock", func() error {
		data, err := store.loadUnlocked()
		if err != nil {
			return err
		}
		data.Profiles[profile.Name] = profile
		if data.Current == "" {
			data.Current = profile.Name
		}
		return store.saveUnlocked(data)
	})
}

// List 按名称排序返回全部 Profile，保证 JSON 和表格输出稳定。
// 入参：无。
// 返回值：[]Profile 为配置列表；error 为读取失败。
func (store *FileStore) List() ([]Profile, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	data, err := store.loadUnlocked()
	if err != nil {
		return nil, err
	}
	profiles := make([]Profile, 0, len(data.Profiles))
	for _, profile := range data.Profiles {
		profiles = append(profiles, profile)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}

// Use 将已存在的 Profile 设为默认环境。
// 入参：name string 为 Profile 名称。
// 返回值：error，Profile 不存在或落盘失败时非 nil。
func (store *FileStore) Use(name string) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	return filelock.With(store.path+".lock", func() error {
		data, err := store.loadUnlocked()
		if err != nil {
			return err
		}
		if _, exists := data.Profiles[name]; !exists {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, name)
		}
		data.Current = name
		return store.saveUnlocked(data)
	})
}

// SetDefaultIdentity 更新 Profile 的默认业务身份，不触碰其他连接字段或 token 缓存。
// 入参：name string 为 Profile 名称；identity IdentityKind 为 app 或 user。
// 返回值：error，Profile 不存在、身份非法或落盘失败时非 nil。
func (store *FileStore) SetDefaultIdentity(name string, identity IdentityKind) error {
	parsed, err := ParseIdentityKind(string(identity))
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()

	return filelock.With(store.path+".lock", func() error {
		data, err := store.loadUnlocked()
		if err != nil {
			return err
		}
		profile, exists := data.Profiles[name]
		if !exists {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, name)
		}
		profile.DefaultIdentity = parsed
		data.Profiles[name] = profile
		return store.saveUnlocked(data)
	})
}

// Get 按名称读取 Profile。
// 入参：name string 为 Profile 名称。
// 返回值：Profile 为命中的配置；error 在不存在或读取失败时非 nil。
func (store *FileStore) Get(name string) (Profile, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	data, err := store.loadUnlocked()
	if err != nil {
		return Profile{}, err
	}
	profile, exists := data.Profiles[name]
	if !exists {
		return Profile{}, fmt.Errorf("%w: %s", ErrProfileNotFound, name)
	}
	return profile, nil
}

// Current 读取当前默认 Profile。
// 入参：无。
// 返回值：Profile 为当前配置；error 在未选择或读取失败时非 nil。
func (store *FileStore) Current() (Profile, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	data, err := store.loadUnlocked()
	if err != nil {
		return Profile{}, err
	}
	if data.Current == "" {
		return Profile{}, ErrNoActiveProfile
	}
	profile, exists := data.Profiles[data.Current]
	if !exists {
		return Profile{}, fmt.Errorf("%w: %s", ErrProfileNotFound, data.Current)
	}
	return profile, nil
}

// loadUnlocked 从磁盘加载配置；文件不存在时返回可直接写入的空结构。
// 入参：无；调用方必须持有 store.mu。
// 返回值：fileData 为配置快照；error 为读取或解析失败。
func (store *FileStore) loadUnlocked() (fileData, error) {
	data := fileData{Profiles: map[string]Profile{}}
	content, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return data, fmt.Errorf("读取配置文件: %w", err)
	}
	if err := json.Unmarshal(content, &data); err != nil {
		return data, fmt.Errorf("解析配置文件: %w", err)
	}
	if data.Profiles == nil {
		data.Profiles = map[string]Profile{}
	}
	return data, nil
}

// saveUnlocked 原子替换配置文件，并强制目录 0700、文件 0600 权限。
// 入参：data fileData 为要持久化的完整配置。
// 返回值：error，编码或落盘失败时非 nil。
func (store *FileStore) saveUnlocked(data fileData) error {
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return fmt.Errorf("创建配置目录: %w", err)
	}
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("编码配置文件: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(store.path), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时配置文件: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("设置配置文件权限: %w", err)
	}
	if _, err := temporary.Write(append(content, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("写入配置文件: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭配置文件: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("替换配置文件: %w", err)
	}
	return nil
}
