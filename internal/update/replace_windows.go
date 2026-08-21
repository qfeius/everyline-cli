//go:build windows

package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
)

func replaceBinary(temporaryPath string, targetPath string) (bool, error) {
	if err := os.Rename(temporaryPath, targetPath); err == nil {
		return false, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("定位替换助手失败: %w", err)
	}
	command := exec.Command(executable, "__everyline-cli-replace", temporaryPath, targetPath, strconv.Itoa(os.Getpid()))
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return false, fmt.Errorf("启动替换助手失败: %w", err)
	}
	return true, nil
}

func runDeferredReplacement(args []string) error {
	if len(args) != 3 {
		return errors.New("替换助手参数数量错误")
	}
	temporaryPath, targetPath := args[0], args[1]
	parentPID, err := strconv.ParseUint(args[2], 10, 32)
	if err != nil {
		return fmt.Errorf("替换助手父进程无效: %w", err)
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(parentPID))
	if err != nil {
		return fmt.Errorf("打开父进程失败: %w", err)
	}
	defer windows.CloseHandle(handle)
	if _, err := windows.WaitForSingleObject(handle, windows.INFINITE); err != nil {
		return fmt.Errorf("等待父进程退出失败: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		if err := os.Rename(temporaryPath, targetPath); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("替换目标文件失败: %w", lastErr)
}
