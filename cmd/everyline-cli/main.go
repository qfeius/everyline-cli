package main

import (
	"context"
	"os"

	"git.qtech.cn/ai/everyline-cli/internal/app"
)

// main 启动 everyline-cli，并将应用层返回的稳定退出码交给操作系统。
// 入参：无。
// 返回值：无；进程通过 os.Exit 返回退出码。
func main() {
	os.Exit(app.Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
