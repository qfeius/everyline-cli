package main

import (
	"context"
	"fmt"
	"os"

	"git.qtech.cn/ai/everyline-cli/internal/app"
	"git.qtech.cn/ai/everyline-cli/internal/update"
)

// main 启动 everyline-cli，并将应用层返回的稳定退出码交给操作系统。
// 入参：无。
// 返回值：无；进程通过 os.Exit 返回退出码。
func main() {
	if len(os.Args) > 1 && os.Args[1] == "__everyline-cli-replace" {
		if err := update.RunDeferredReplacement(os.Args[2:]); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	os.Exit(app.Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
