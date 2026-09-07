//go:build !windows

package update

import (
	"errors"
	"os"
)

func replaceBinary(temporaryPath string, targetPath string) (bool, error) {
	return false, os.Rename(temporaryPath, targetPath)
}

func runDeferredReplacement([]string) error {
	return errors.New("当前平台不支持替换助手")
}
