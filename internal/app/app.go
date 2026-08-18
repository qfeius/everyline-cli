package app

import (
	"context"
	"fmt"
	"io"

	"git.qtech.cn/ai/everyline-cli/internal/cli"
	"git.qtech.cn/ai/everyline-cli/internal/config"
)

// Run 装配生产运行时、执行命令，并把错误稳定映射为退出码。
// 入参：ctx context.Context 控制取消；args []string 为 CLI 参数；stdin io.Reader、stdout/stderr io.Writer 为进程 I/O。
// 返回值：int，为技术方案定义的稳定退出码。
func Run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	configDir, err := config.DefaultDir()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return cli.ExitAuth
	}
	runtime := cli.NewRuntime(configDir, stdin, stdout, stderr)
	err = cli.Execute(ctx, runtime, args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
	}
	return cli.ExitCode(err)
}
