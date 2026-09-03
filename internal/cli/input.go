package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// normalizeRequiredID 去除路径 ID 两端空白，并拒绝 Cobra 仅检查“已传入”但实际为空白的值。
// 入参：value string 为原始 flag 值；flagName string 为错误消息中的参数名。
// 返回值：string 为规范化 ID；error 在 ID 为空时非 nil。
func normalizeRequiredID(value string, flagName string) (string, error) {
	resolved := strings.TrimSpace(value)
	if resolved == "" {
		return "", fmt.Errorf("%s 不能为空", flagName)
	}
	return resolved, nil
}

const maxInputBytes = 2 << 20

// readJSONInput 从 --input 或 --data 二选一读取严格 JSON，并拒绝多余顶层内容。
// 入参：inputPath string 为 JSON 文件；inline string 为内联 JSON；target any 为解码目标指针。
// 返回值：error，在来源数量不为一、超限、未知字段或 JSON 非法时非 nil。
func readJSONInput(inputPath string, inline string, target any) error {
	if (inputPath == "") == (inline == "") {
		return fmt.Errorf("必须且只能提供 --input 或 --data 之一")
	}
	var content []byte
	if inputPath != "" {
		file, err := os.Open(inputPath)
		if err != nil {
			return fmt.Errorf("打开输入文件: %w", err)
		}
		defer file.Close()
		content, err = io.ReadAll(io.LimitReader(file, maxInputBytes+1))
		if err != nil {
			return fmt.Errorf("读取输入文件: %w", err)
		}
	} else {
		content = []byte(inline)
	}
	if len(content) > maxInputBytes {
		return fmt.Errorf("JSON 输入不能超过 %d 字节", maxInputBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("解析 JSON 输入: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("JSON 输入只能包含一个顶层对象")
	}
	return nil
}

// readSecret 从 stdin 读取一段 app secret；真实终端关闭回显并以回车结束，管道仍读取到 EOF。
// 入参：reader io.Reader 为 stdin；prompt io.Writer 为不含密钥的交互提示输出。
// 返回值：string 为内存中的密钥；error 为读取失败或空密钥。
func readSecret(reader io.Reader, prompt io.Writer) (string, error) {
	return readSecretWithTerminal(reader, prompt, term.IsTerminal, term.ReadPassword)
}

// readSecretWithTerminal 注入终端探测和密码读取能力，保证隐藏输入分支可独立验证。
// 入参：reader io.Reader 为 stdin；prompt io.Writer 为 stderr；isTerminal func(int) bool 判断文件描述符；readPassword func(int) ([]byte,error) 负责关闭回显读取。
// 返回值：string 为去除首尾空白的密钥；error 为提示、读取或空值错误。
func readSecretWithTerminal(
	reader io.Reader,
	prompt io.Writer,
	isTerminal func(int) bool,
	readPassword func(int) ([]byte, error),
) (string, error) {
	if inputFile, ok := reader.(*os.File); ok && isTerminal(int(inputFile.Fd())) {
		// 提示只写 stderr，密码由终端驱动关闭回显，避免污染结构化 stdout 或进入 shell 历史。
		if _, err := fmt.Fprint(prompt, "App secret: "); err != nil {
			return "", fmt.Errorf("输出 app secret 提示: %w", err)
		}
		content, err := readPassword(int(inputFile.Fd()))
		_, newlineErr := fmt.Fprintln(prompt)
		if err != nil {
			return "", fmt.Errorf("隐藏读取 app secret: %w", err)
		}
		if newlineErr != nil {
			return "", fmt.Errorf("结束 app secret 提示: %w", newlineErr)
		}
		return normalizeSecret(content)
	}
	content, err := io.ReadAll(io.LimitReader(reader, 64<<10))
	if err != nil {
		return "", fmt.Errorf("读取 app secret: %w", err)
	}
	return normalizeSecret(content)
}

// normalizeSecret 统一清理终端或管道输入的换行和两端空白，并拒绝空密钥。
// 入参：content []byte 为内存中的原始输入。
// 返回值：string 为规范化密钥；error 为空输入错误。
func normalizeSecret(content []byte) (string, error) {
	secret := strings.TrimSpace(string(content))
	if secret == "" {
		return "", fmt.Errorf("app secret 不能为空")
	}
	return secret, nil
}
