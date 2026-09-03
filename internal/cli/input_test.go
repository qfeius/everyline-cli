package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestReadSecretUsesHiddenTerminalInput 验证真实终端分支调用隐藏读取，并且提示不包含密钥。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；终端探测、规范化或输出泄密时通过 t.Fatal 报告。
func TestReadSecretUsesHiddenTerminalInput(t *testing.T) {
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer output.Close()
	prompt := &bytes.Buffer{}
	secret, err := readSecretWithTerminal(
		input,
		prompt,
		func(fileDescriptor int) bool { return fileDescriptor == int(input.Fd()) },
		func(fileDescriptor int) ([]byte, error) {
			if fileDescriptor != int(input.Fd()) {
				return nil, fmt.Errorf("fd=%d", fileDescriptor)
			}
			return []byte("  hidden-secret\r\n"), nil
		},
	)
	if err != nil || secret != "hidden-secret" {
		t.Fatalf("secret=%q err=%v", secret, err)
	}
	if prompt.String() != "App secret: \n" || strings.Contains(prompt.String(), secret) {
		t.Fatalf("prompt=%q", prompt.String())
	}
}

// TestReadSecretPreservesPipeInput 验证管道和 CI 仍按原协议读取到 EOF。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；管道值漂移或错误调用终端读取时通过 t.Fatal 报告。
func TestReadSecretPreservesPipeInput(t *testing.T) {
	secret, err := readSecretWithTerminal(
		strings.NewReader("  piped-secret\n"),
		&bytes.Buffer{},
		func(int) bool { return false },
		func(int) ([]byte, error) { return nil, fmt.Errorf("不应读取终端") },
	)
	if err != nil || secret != "piped-secret" {
		t.Fatalf("secret=%q err=%v", secret, err)
	}
}
