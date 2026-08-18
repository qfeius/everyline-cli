package cli

import (
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
	Profiles config.Store
	Tokens   auth.TokenStore
	HTTP     *http.Client
	Input    io.Reader
	Output   io.Writer
	Error    io.Writer
	Renderer output.Renderer
	Now      func() time.Time
}

// NewRuntime 使用指定配置目录创建生产运行时。
// 入参：configDir string 为 CLI 私有目录；input io.Reader 为 stdin；stdout/stderr io.Writer 为输出流。
// 返回值：*Runtime，包含文件 Store、HTTP Client、Renderer 和真实时钟。
func NewRuntime(configDir string, input io.Reader, stdout io.Writer, stderr io.Writer) *Runtime {
	return &Runtime{
		Profiles: config.NewFileStore(filepath.Join(configDir, "config.json")),
		Tokens:   auth.NewFileTokenStore(filepath.Join(configDir, "tokens.json")),
		HTTP:     &http.Client{},
		Input:    input,
		Output:   stdout,
		Error:    stderr,
		Renderer: output.DefaultRenderer{},
		Now:      time.Now,
	}
}
