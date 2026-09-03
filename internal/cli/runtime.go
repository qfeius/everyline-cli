package cli

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/output"
)

// Runtime 汇总 CLI 可注入依赖，避免命令处理器直接读取全局状态。
type Runtime struct {
	Profiles              config.Store
	Tokens                auth.TokenStore
	Secrets               auth.AppSecretStore
	DeviceCredentials     auth.DeviceCredentialStore
	DeviceCredentialError error
	HTTP                  *http.Client
	OpenBrowser           func(string) error
	Input                 io.Reader
	Output                io.Writer
	Error                 io.Writer
	Renderer              output.Renderer
	Now                   func() time.Time
}

// NewRuntime 使用指定配置目录创建生产运行时。
// 入参：configDir string 为 CLI 私有目录；input io.Reader 为 stdin；stdout/stderr io.Writer 为输出流。
// 返回值：*Runtime，包含文件 Store、HTTP Client、Renderer 和真实时钟。
func NewRuntime(configDir string, input io.Reader, stdout io.Writer, stderr io.Writer) *Runtime {
	deviceCredentials, deviceCredentialError := auth.NewDeviceCredentialStore(auth.DeviceCredentialOptions{})
	if errors.Is(deviceCredentialError, auth.ErrDeviceRuntimeUnavailable) {
		deviceCredentialError = nil
	}
	return &Runtime{
		Profiles:              config.NewFileStore(filepath.Join(configDir, "config.json")),
		Tokens:                auth.NewFileTokenStore(filepath.Join(configDir, "tokens.json")),
		Secrets:               auth.NewDefaultAppSecretStore(configDir),
		DeviceCredentials:     deviceCredentials,
		DeviceCredentialError: deviceCredentialError,
		HTTP:                  &http.Client{},
		Input:                 input,
		Output:                stdout,
		Error:                 stderr,
		Renderer:              output.DefaultRenderer{},
		Now:                   time.Now,
	}
}

// newTokenProvider 统一装配兼容 token 缓存和可选的沙箱 Device 凭证存储。
// 入参：runtime *Runtime 为 HTTP、时钟和凭证依赖。
// 返回值：*auth.Provider，可为 app/user 请求提供并刷新 token。
func newTokenProvider(runtime *Runtime) *auth.Provider {
	provider := auth.NewProvider(runtime.Tokens, runtime.HTTP, runtime.Now, runtime.Secrets)
	if runtime.DeviceCredentials != nil {
		provider.WithDeviceCredentials(runtime.DeviceCredentials)
	}
	return provider
}
