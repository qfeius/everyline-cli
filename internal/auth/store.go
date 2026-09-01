package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/filelock"
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
	return filelock.With(store.path+".lock", func() error {
		tokens, err := store.loadUnlocked()
		if err != nil {
			return err
		}
		tokens[profileName] = token
		return store.saveUnlocked(tokens)
	})
}

// Delete 删除指定 Profile 的 token；缓存文件不存在时保持幂等成功。
// 入参：profileName string 为 Profile 名称。
// 返回值：error，读取或落盘失败时非 nil。
func (store *FileTokenStore) Delete(profileName string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return filelock.With(store.path+".lock", func() error {
		tokens, err := store.loadUnlocked()
		if err != nil {
			return err
		}
		delete(tokens, profileName)
		return store.saveUnlocked(tokens)
	})
}

// LoadForIdentity 读取指定 Profile 和身份的 token；app 身份复用旧版 key 以保持兼容。
// 入参：profileName string 为 Profile 名称；identity config.IdentityKind 为业务身份。
// 返回值：Token 为缓存凭证；error 在不存在或读取失败时非 nil。
func (store *FileTokenStore) LoadForIdentity(profileName string, identity config.IdentityKind) (Token, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	tokens, err := store.loadUnlocked()
	if err != nil {
		return Token{}, err
	}
	token, exists := tokens[tokenStorageKey(profileName, identity)]
	if !exists {
		return Token{}, os.ErrNotExist
	}
	return token, nil
}

// SaveForIdentity 保存指定 Profile 和身份的 token，不覆盖另一身份的缓存。
// 入参：profileName string 为 Profile 名称；identity config.IdentityKind 为业务身份；token Token 为凭证。
// 返回值：error，落盘失败时非 nil。
func (store *FileTokenStore) SaveForIdentity(profileName string, identity config.IdentityKind, token Token) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return filelock.With(store.path+".lock", func() error {
		tokens, err := store.loadUnlocked()
		if err != nil {
			return err
		}
		tokens[tokenStorageKey(profileName, identity)] = token
		return store.saveUnlocked(tokens)
	})
}

// DeleteForIdentity 清理指定 Profile 和身份的 token，其他身份保持不变。
// 入参：profileName string 为 Profile 名称；identity config.IdentityKind 为业务身份。
// 返回值：error，读取或落盘失败时非 nil。
func (store *FileTokenStore) DeleteForIdentity(profileName string, identity config.IdentityKind) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return filelock.With(store.path+".lock", func() error {
		tokens, err := store.loadUnlocked()
		if err != nil {
			return err
		}
		delete(tokens, tokenStorageKey(profileName, identity))
		return store.saveUnlocked(tokens)
	})
}

// DeleteForIdentityIfAccessTokenMatches 仅在缓存仍是服务端拒绝的 token 时删除指定身份凭证，避免旧请求误删并发登录写入的新 token。
// 入参：profileName string 为 Profile 名称；identity config.IdentityKind 为业务身份；rejectedAccessToken string 为本次被服务端拒绝的 access token。
// 返回值：error，读取或落盘失败时非 nil；缓存不存在或已被更新时保持幂等成功。
func (store *FileTokenStore) DeleteForIdentityIfAccessTokenMatches(profileName string, identity config.IdentityKind, rejectedAccessToken string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return filelock.With(store.path+".lock", func() error {
		tokens, err := store.loadUnlocked()
		if err != nil {
			return err
		}
		key := tokenStorageKey(profileName, identity)
		cached, exists := tokens[key]
		// 关键约束：只删除本次请求实际使用的旧 token，保留期间重新授权写入的新 token。
		if !exists || cached.AccessToken != rejectedAccessToken {
			return nil
		}
		delete(tokens, key)
		return store.saveUnlocked(tokens)
	})
}

// tokenStorageKey 为 user 身份生成隔离 key；旧 app token 仍使用 profile 名称。
// 入参：profileName string 为 Profile 名称；identity config.IdentityKind 为业务身份。
// 返回值：string，为 tokens.json 中的稳定 key。
func tokenStorageKey(profileName string, identity config.IdentityKind) string {
	if identity == config.IdentityUser {
		return profileName + "::user"
	}
	return profileName
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
