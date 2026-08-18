package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileTokenStore 在 Keychain 不可用时，以 0600 文件缓存短期 token。
type FileTokenStore struct {
	path string
	mu   sync.Mutex
}

// NewFileTokenStore 创建文件 token 缓存。
// 入参：path string 为 tokens.json 路径。
// 返回值：*FileTokenStore，可用于 token 读写和注销。
func NewFileTokenStore(path string) *FileTokenStore {
	return &FileTokenStore{path: path}
}

// Load 读取指定 Profile 的 token。
// 入参：profileName string 为 Profile 名称。
// 返回值：Token 为缓存值；error 在不存在或读取失败时非 nil。
func (store *FileTokenStore) Load(profileName string) (Token, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	tokens, err := store.loadUnlocked()
	if err != nil {
		return Token{}, err
	}
	token, exists := tokens[profileName]
	if !exists {
		return Token{}, os.ErrNotExist
	}
	return token, nil
}

// Save 保存指定 Profile 的 token，并保证文件权限为 0600。
// 入参：profileName string 为 Profile 名称；token Token 为缓存值。
// 返回值：error，落盘失败时非 nil。
func (store *FileTokenStore) Save(profileName string, token Token) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	tokens, err := store.loadUnlocked()
	if err != nil {
		return err
	}
	tokens[profileName] = token
	return store.saveUnlocked(tokens)
}

// Delete 删除指定 Profile 的 token；缓存文件不存在时保持幂等成功。
// 入参：profileName string 为 Profile 名称。
// 返回值：error，读取或落盘失败时非 nil。
func (store *FileTokenStore) Delete(profileName string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	tokens, err := store.loadUnlocked()
	if err != nil {
		return err
	}
	delete(tokens, profileName)
	return store.saveUnlocked(tokens)
}

// loadUnlocked 读取整个 token 映射；不存在时返回空映射。
// 入参：无；调用方必须持有 store.mu。
// 返回值：map[string]Token 为缓存快照；error 为读取或解析失败。
func (store *FileTokenStore) loadUnlocked() (map[string]Token, error) {
	tokens := map[string]Token{}
	content, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return tokens, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 token 缓存: %w", err)
	}
	if err := json.Unmarshal(content, &tokens); err != nil {
		return nil, fmt.Errorf("解析 token 缓存: %w", err)
	}
	return tokens, nil
}

// saveUnlocked 原子替换 token 缓存并收紧目录和文件权限。
// 入参：tokens map[string]Token 为完整缓存。
// 返回值：error，编码或落盘失败时非 nil。
func (store *FileTokenStore) saveUnlocked(tokens map[string]Token) error {
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return fmt.Errorf("创建 token 缓存目录: %w", err)
	}
	content, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("编码 token 缓存: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(store.path), ".tokens-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时 token 文件: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("设置 token 文件权限: %w", err)
	}
	if _, err := temporary.Write(append(content, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("写入 token 缓存: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭 token 缓存: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("替换 token 缓存: %w", err)
	}
	return nil
}
