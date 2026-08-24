//go:build windows

package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
)

// replaceBinary 尝试直接替换 Windows 可执行文件；目标被占用时启动独立 helper 延后替换。
// 入参：temporaryPath/targetPath string 分别为已校验制品和当前可执行文件路径。
// 返回值：bool 表示是否已交给 helper 调度；error 表示直接替换、helper 创建或启动失败。
func replaceBinary(temporaryPath string, targetPath string) (bool, error) {
	if err := os.Rename(temporaryPath, targetPath); err == nil {
		return false, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("定位替换助手失败: %w", err)
	}
	// helper 必须是独立副本；继续运行目标 EXE 会让 helper 自己占用待替换文件。
	helperPath, err := createReplacementHelper(executable, filepath.Dir(targetPath))
	if err != nil {
		return false, err
	}
	command := exec.Command(helperPath, "__everyline-cli-replace", temporaryPath, targetPath, strconv.Itoa(os.Getpid()), helperPath)
	command.Stdin = nil
	command.Stdout = io.Discard
	// helper 在父进程退出后才产生最终结果；继承 stderr 才能让异步成功或失败对用户可见。
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		_ = os.Remove(helperPath)
		return false, fmt.Errorf("启动替换助手失败: %w", err)
	}
	return true, nil
}

// createReplacementHelper 把当前 CLI 复制为独立 Windows helper，避免 helper 占用目标文件。
// 入参：executable/targetDirectory string 分别为当前 CLI 路径和 helper 放置目录。
// 返回值：string 为 helper 路径；error 为创建、复制、同步或关闭文件失败。
func createReplacementHelper(executable string, targetDirectory string) (string, error) {
	source, err := os.Open(executable)
	if err != nil {
		return "", fmt.Errorf("打开替换助手源文件失败: %w", err)
	}
	defer source.Close()
	helper, err := os.CreateTemp(targetDirectory, ".everyline-cli-replace-*.exe")
	if err != nil {
		return "", fmt.Errorf("创建替换助手失败: %w", err)
	}
	helperPath := helper.Name()
	cleanup := true
	defer func() {
		_ = helper.Close()
		if cleanup {
			_ = os.Remove(helperPath)
		}
	}()
	if _, err := io.Copy(helper, source); err != nil {
		return "", fmt.Errorf("复制替换助手失败: %w", err)
	}
	if err := helper.Sync(); err != nil {
		return "", fmt.Errorf("写入替换助手失败: %w", err)
	}
	if err := helper.Close(); err != nil {
		return "", fmt.Errorf("关闭替换助手失败: %w", err)
	}
	cleanup = false
	return helperPath, nil
}

// runDeferredReplacement 等待父进程释放目标文件，再由独立 helper 完成重试替换。
// 入参：args []string 依次包含制品临时路径、目标路径、父进程 PID 和 helper 自身路径。
// 返回值：error，参数、父进程等待或最终替换失败时非 nil。
func runDeferredReplacement(args []string) error {
	if len(args) != 4 {
		return errors.New("替换助手参数数量错误")
	}
	temporaryPath, targetPath, helperPath := args[0], args[1], args[3]
	// 无论最终替换是否成功，都清理未被 rename 消耗的下载文件。
	defer os.Remove(temporaryPath)
	// helper 正在运行时不能立即删除自身，登记为重启时清理，避免长期残留副本。
	if helperPointer, pointerErr := windows.UTF16PtrFromString(helperPath); pointerErr == nil {
		_ = windows.MoveFileEx(helperPointer, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	}
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
	return replaceWithRetry(temporaryPath, targetPath, 20, 250*time.Millisecond, os.Rename, time.Sleep)
}
