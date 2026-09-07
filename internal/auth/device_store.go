package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/filelock"

	keyring "github.com/zalando/go-keyring"
)

const (
	deviceCredentialKeyEnvironment = "EVERYLINE_CLI_CREDENTIAL_KEY_V1"
	deviceKeyringService           = "git.qtech.cn.ai.everyline-cli"
	doubaoCredentialKeyPurpose     = "everyline-cli/doubao-work-task/credential-key/v1\x00"
	doubaoSessionNamespacePurpose  = "everyline-cli/doubao-work-task/session-namespace/v1\x00"
)

var (
	ErrDeviceCredentialNotFound = errors.New("Device 凭证不存在")
	ErrDeviceRuntimeUnavailable = errors.New("当前环境不是受支持的豆包或 WorkBuddy Device 运行时")
)

// DevicePendingStatus 表示一次 Device 授权事务的可恢复状态。
type DevicePendingStatus string

const (
	DevicePending             DevicePendingStatus = "pending"
	DevicePendingChecking     DevicePendingStatus = "checking"
	DevicePendingUncertain    DevicePendingStatus = "uncertain"
	DevicePendingDenied       DevicePendingStatus = "denied"
	DevicePendingExpired      DevicePendingStatus = "expired"
	DevicePendingInvalidGrant DevicePendingStatus = "invalid_grant"
)

// DevicePendingTransaction 保存 auth init 与 auth complete 之间的短期授权事务。
type DevicePendingTransaction struct {
	Status                  DevicePendingStatus `json:"status"`
	DeviceCode              string              `json:"device_code"`
	VerificationURIComplete string              `json:"verification_uri_complete"`
	TokenEndpoint           string              `json:"token_endpoint"`
	RevocationEndpoint      string              `json:"revocation_endpoint,omitempty"`
	ClientID                string              `json:"client_id"`
	ExpiresAt               time.Time           `json:"expires_at"`
	FirstInstallEventID     string              `json:"first_install_event_id,omitempty"`
}

/*
EffectiveStatus 统一解析 Device 事务状态，让等待、异常和进程中断状态在授权期限结束后进入 expired。
入参：now time.Time 为当前时间；接收者 DevicePendingTransaction 为持久化的授权事务。
返回值：DevicePendingStatus 为对当前时间生效的状态；已明确拒绝或失效的终态保持原值。
*/
func (pending DevicePendingTransaction) EffectiveStatus(now time.Time) DevicePendingStatus {
	status := pending.Status
	if status == "" {
		status = DevicePending
	}
	// checking/uncertain 不重复兑换旧 code，但到期后必须允许用户显式发起新事务。
	if (status == DevicePending || status == DevicePendingChecking || status == DevicePendingUncertain) && !now.Before(pending.ExpiresAt) {
		return DevicePendingExpired
	}
	return status
}

// DeviceCredential 把沙箱需要恢复的 Profile、授权事务和 user token 一起保存。
type DeviceCredential struct {
	Pending *DevicePendingTransaction `json:"pending,omitempty"`
	Token   *Token                    `json:"token,omitempty"`
	Profile *config.Profile           `json:"profile,omitempty"`
	// TokenFirstInstallEventID 随成功 token 一起落盘，支持门禁写入失败后的本地恢复，不作为新事件的授权证明。
	TokenFirstInstallEventID string `json:"token_first_install_event_id,omitempty"`
}

// DeviceCredentialStore 定义沙箱 Device 凭证的安全读写和跨进程刷新锁。
type DeviceCredentialStore interface {
	Load(string) (DeviceCredential, error)
	Save(string, DeviceCredential) error
	Delete(string) error
	WithRefreshLock(string, func() error) error
}

// DeviceCredentialOptions 提供运行时环境和可测试 Keyring 边界。
type DeviceCredentialOptions struct {
	LookupEnv func(string) (string, bool)
	Keyring   DeviceKeyring
	Getwd     func() (string, error)
}

// DeviceKeyring 抽象 WorkBuddy 使用的系统安全凭证存储。
type DeviceKeyring interface {
	Get(string, string) (string, error)
	Set(string, string, string) error
	Delete(string, string) error
}

type deviceRuntimeKind string

const (
	deviceRuntimeDoubaoCloud deviceRuntimeKind = "doubao_cloud"
	deviceRuntimeWorkBuddy   deviceRuntimeKind = "workbuddy"
	deviceRuntimeDoubaoTask  deviceRuntimeKind = "doubao_work_task"
)

type deviceRuntime struct {
	kind      deviceRuntimeKind
	workspace string
	sessionID string
	dataDir   string
}

// NewDeviceCredentialStore 根据运行时选择豆包加密文件或 WorkBuddy 系统 Keyring。
// 入参：options DeviceCredentialOptions 注入环境变量、工作目录和 Keyring。
// 返回值：DeviceCredentialStore 为安全存储；error 为环境未识别或安全存储配置错误。
func NewDeviceCredentialStore(options DeviceCredentialOptions) (DeviceCredentialStore, error) {
	runtimeContext, err := resolveDeviceRuntime(options)
	if err != nil {
		return nil, err
	}
	switch runtimeContext.kind {
	case deviceRuntimeDoubaoCloud:
		lookupEnv := options.LookupEnv
		if lookupEnv == nil {
			lookupEnv = os.LookupEnv
		}
		encodedKey, ok := lookupEnv(deviceCredentialKeyEnvironment)
		key, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
		if !ok || decodeErr != nil || len(key) != 32 {
			return nil, fmt.Errorf("豆包 Skill 环境必须提供 base64 编码的 32 字节 %s", deviceCredentialKeyEnvironment)
		}
		return &encryptedDeviceStore{dir: filepath.Join(runtimeContext.workspace, ".everyline-cli", "credentials"), key: key}, nil
	case deviceRuntimeDoubaoTask:
		return &encryptedDeviceStore{
			dir: filepath.Join(runtimeContext.dataDir, "credentials"),
			key: deriveDoubaoCredentialKey(runtimeContext.sessionID),
		}, nil
	case deviceRuntimeWorkBuddy:
		backend := options.Keyring
		if backend == nil {
			backend = systemDeviceKeyring{}
		}
		return &keyringDeviceStore{sessionID: runtimeContext.sessionID, keyring: backend}, nil
	default:
		return nil, ErrDeviceRuntimeUnavailable
	}
}

// resolveDeviceRuntime 按 AgentKit、WorkBuddy、豆包工作任务的优先级识别凭证隔离域。
// 入参：options DeviceCredentialOptions 提供环境变量和工作目录读取函数。
// 返回值：deviceRuntime 为隔离目录或会话；error 为运行时缺失或目录非法。
func resolveDeviceRuntime(options DeviceCredentialOptions) (deviceRuntime, error) {
	lookupEnv := options.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	if workspace, ok := lookupEnv("SKILL_SESSION_WORKSPACE"); ok && strings.TrimSpace(workspace) != "" {
		workspace = strings.TrimSpace(workspace)
		if !filepath.IsAbs(workspace) {
			return deviceRuntime{}, fmt.Errorf("SKILL_SESSION_WORKSPACE 必须是绝对路径")
		}
		return deviceRuntime{kind: deviceRuntimeDoubaoCloud, workspace: workspace}, nil
	}
	if sessionID, ok := lookupEnv("CODEBUDDY_SESSION_ID"); ok && strings.TrimSpace(sessionID) != "" {
		return deviceRuntime{kind: deviceRuntimeWorkBuddy, sessionID: strings.TrimSpace(sessionID)}, nil
	}
	if sessionID, ok := lookupEnv("SESSION_ID"); ok && strings.TrimSpace(sessionID) != "" {
		getwd := options.Getwd
		if getwd == nil {
			getwd = os.Getwd
		}
		workspace, getwdErr := getwd()
		if getwdErr != nil {
			return deviceRuntime{}, fmt.Errorf("解析豆包工作任务目录: %w", getwdErr)
		}
		if !filepath.IsAbs(workspace) {
			return deviceRuntime{}, fmt.Errorf("豆包工作任务目录必须是绝对路径: %q", workspace)
		}
		sessionID = strings.TrimSpace(sessionID)
		namespace := hashText(doubaoSessionNamespacePurpose + sessionID)
		return deviceRuntime{
			kind: deviceRuntimeDoubaoTask, workspace: workspace, sessionID: sessionID,
			dataDir: filepath.Join(workspace, ".everyline-cli", "sessions", namespace),
		}, nil
	}
	return deviceRuntime{}, ErrDeviceRuntimeUnavailable
}

type encryptedDeviceStore struct {
	dir string
	key []byte
}

// Load 解密读取指定 Profile 的 Device 凭证。
// 入参：profileName string 为 Profile 名称。
// 返回值：DeviceCredential 为凭证；error 为不存在、读取或认证解密失败。
func (store *encryptedDeviceStore) Load(profileName string) (DeviceCredential, error) {
	content, err := os.ReadFile(store.path(profileName))
	if errors.Is(err, os.ErrNotExist) {
		return DeviceCredential{}, ErrDeviceCredentialNotFound
	}
	if err != nil {
		return DeviceCredential{}, fmt.Errorf("读取加密 Device 凭证: %w", err)
	}
	plaintext, err := decryptDeviceCredential(store.key, content)
	if err != nil {
		return DeviceCredential{}, err
	}
	var credential DeviceCredential
	if err := json.Unmarshal(plaintext, &credential); err != nil {
		return DeviceCredential{}, fmt.Errorf("解析 Device 凭证: %w", err)
	}
	return credential, nil
}

// Save 加密并原子保存指定 Profile 的 Device 凭证。
// 入参：profileName string 为 Profile 名称；credential DeviceCredential 为完整凭证。
// 返回值：error，编码、加密或落盘失败时非 nil。
func (store *encryptedDeviceStore) Save(profileName string, credential DeviceCredential) error {
	return filelock.With(store.path(profileName)+".lock", func() error {
		plaintext, err := json.Marshal(credential)
		if err != nil {
			return fmt.Errorf("编码 Device 凭证: %w", err)
		}
		ciphertext, err := encryptDeviceCredential(store.key, plaintext)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(store.dir, 0o700); err != nil {
			return fmt.Errorf("创建 Device 凭证目录: %w", err)
		}
		if err := os.Chmod(store.dir, 0o700); err != nil {
			return fmt.Errorf("设置 Device 凭证目录权限: %w", err)
		}
		temporary, err := os.CreateTemp(store.dir, ".device-credential-*.tmp")
		if err != nil {
			return fmt.Errorf("创建设备凭证临时文件: %w", err)
		}
		temporaryPath := temporary.Name()
		defer os.Remove(temporaryPath)
		if err := temporary.Chmod(0o600); err != nil {
			_ = temporary.Close()
			return err
		}
		if _, err := temporary.Write(ciphertext); err != nil {
			_ = temporary.Close()
			return fmt.Errorf("写入 Device 凭证: %w", err)
		}
		if err := temporary.Close(); err != nil {
			return fmt.Errorf("关闭 Device 凭证: %w", err)
		}
		if err := os.Rename(temporaryPath, store.path(profileName)); err != nil {
			return fmt.Errorf("替换 Device 凭证: %w", err)
		}
		return nil
	})
}

// Delete 幂等删除指定 Profile 的加密 Device 凭证。
// 入参：profileName string 为 Profile 名称。
// 返回值：error，非文件不存在的删除失败。
func (store *encryptedDeviceStore) Delete(profileName string) error {
	err := os.Remove(store.path(profileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// WithRefreshLock 在指定 Profile 的跨进程锁中执行 token 刷新。
// 入参：profileName string 为 Profile 名称；action func() error 为锁内操作。
// 返回值：error，为加锁或 action 失败。
func (store *encryptedDeviceStore) WithRefreshLock(profileName string, action func() error) error {
	return filelock.With(store.path(profileName)+".refresh.lock", action)
}

// path 返回不泄露 Profile 原文的加密凭证路径。
// 入参：profileName string 为 Profile 名称。
// 返回值：string，为 SHA-256 文件名。
func (store *encryptedDeviceStore) path(profileName string) string {
	return filepath.Join(store.dir, hashText(profileName)+".json.enc")
}

type keyringDeviceStore struct {
	sessionID string
	keyring   DeviceKeyring
}

// Load 从 WorkBuddy 所在系统的安全凭证库读取 Device 凭证。
// 入参：profileName string 为 Profile 名称。
// 返回值：DeviceCredential 为凭证；error 为不存在或解析失败。
func (store *keyringDeviceStore) Load(profileName string) (DeviceCredential, error) {
	value, err := store.keyring.Get(deviceKeyringService, store.user(profileName))
	if errors.Is(err, keyring.ErrNotFound) || errors.Is(err, ErrDeviceCredentialNotFound) {
		return DeviceCredential{}, ErrDeviceCredentialNotFound
	}
	if err != nil {
		return DeviceCredential{}, fmt.Errorf("从 %s 安全凭证库读取 WorkBuddy Device 凭证: %w", runtime.GOOS, err)
	}
	var credential DeviceCredential
	if err := json.Unmarshal([]byte(value), &credential); err != nil {
		return DeviceCredential{}, fmt.Errorf("解析 WorkBuddy Device 凭证: %w", err)
	}
	return credential, nil
}

// Save 将 Device 凭证写入 WorkBuddy 所在系统的安全凭证库。
// 入参：profileName string 为 Profile 名称；credential DeviceCredential 为完整凭证。
// 返回值：error，编码或安全存储写入失败时非 nil。
func (store *keyringDeviceStore) Save(profileName string, credential DeviceCredential) error {
	content, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("编码 WorkBuddy Device 凭证: %w", err)
	}
	if err := store.keyring.Set(deviceKeyringService, store.user(profileName), string(content)); err != nil {
		return fmt.Errorf("写入 %s 安全凭证库: %w", runtime.GOOS, err)
	}
	return nil
}

// Delete 幂等删除 WorkBuddy Device 凭证。
// 入参：profileName string 为 Profile 名称。
// 返回值：error，非凭证不存在的删除失败。
func (store *keyringDeviceStore) Delete(profileName string) error {
	err := store.keyring.Delete(deviceKeyringService, store.user(profileName))
	if errors.Is(err, keyring.ErrNotFound) || errors.Is(err, ErrDeviceCredentialNotFound) {
		return nil
	}
	return err
}

// WithRefreshLock 使用会话和 Profile 隔离的临时锁串行化 WorkBuddy token 刷新。
// 入参：profileName string 为 Profile 名称；action func() error 为锁内操作。
// 返回值：error，为加锁或 action 失败。
func (store *keyringDeviceStore) WithRefreshLock(profileName string, action func() error) error {
	lockDirectory := filepath.Join(os.TempDir(), "everyline-cli-"+hashText(store.sessionID))
	if err := os.MkdirAll(lockDirectory, 0o700); err != nil {
		return fmt.Errorf("创建 WorkBuddy Device 锁目录: %w", err)
	}
	return filelock.With(filepath.Join(lockDirectory, hashText(profileName)+".refresh.lock"), action)
}

// user 构造 WorkBuddy 会话隔离的 Keyring 用户名。
// 入参：profileName string 为 Profile 名称。
// 返回值：string，为 session 与 Profile 组合值。
func (store *keyringDeviceStore) user(profileName string) string {
	return store.sessionID + ":" + profileName
}

type systemDeviceKeyring struct{}

func (systemDeviceKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (systemDeviceKeyring) Set(service, user, value string) error {
	return keyring.Set(service, user, value)
}

func (systemDeviceKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

// encryptDeviceCredential 使用 AES-256-GCM 加密并把 nonce 前置到密文。
// 入参：key []byte 为 32 字节密钥；plaintext []byte 为 JSON 明文。
// 返回值：[]byte 为 nonce+密文；error 为加密初始化或随机数失败。
func encryptDeviceCredential(key []byte, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("初始化 Device 凭证加密: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("初始化 Device 凭证 GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("生成 Device 凭证 nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptDeviceCredential 验证并解密 nonce 前置的 AES-GCM 密文。
// 入参：key []byte 为 32 字节密钥；ciphertext []byte 为持久化内容。
// 返回值：[]byte 为 JSON 明文；error 为截断、密钥或认证失败。
func decryptDeviceCredential(key []byte, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("初始化 Device 凭证解密: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("初始化 Device 凭证 GCM: %w", err)
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("加密 Device 凭证已截断")
	}
	plaintext, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("加密 Device 凭证认证失败")
	}
	return plaintext, nil
}

// deriveDoubaoCredentialKey 从豆包任务会话生成仅用于本地加密的 32 字节密钥。
// 入参：sessionID string 为豆包任务会话 ID。
// 返回值：[]byte，为固定域分离后的 SHA-256。
func deriveDoubaoCredentialKey(sessionID string) []byte {
	digest := sha256.Sum256([]byte(doubaoCredentialKeyPurpose + sessionID))
	return append([]byte(nil), digest[:]...)
}

// hashText 把隔离标识转换为固定长度且不泄露原文的文件名片段。
// 入参：value string 为 Profile 或会话文本。
// 返回值：string，为十六进制 SHA-256。
func hashText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
