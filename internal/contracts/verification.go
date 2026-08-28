package contracts

import (
	"errors"
	"fmt"
	"strings"
)

// ErrContractUnverified 表示技术方案尚未提供字段级详情，CLI 不得猜测写请求契约。
var ErrContractUnverified = errors.New("接口契约尚未核验")

// ErrContractMismatch 表示业务服务生成的 method/path 或逻辑输入与映射目录不一致。
var ErrContractMismatch = errors.New("请求与接口映射不一致")

var unverifiedOperations = map[string]struct{}{}

// RequireVerified 阻止缺少字段级详情页依据的远端写操作，同时允许命令层继续执行本地 dry-run。
// 入参：operationID string 为待执行的远端操作标识。
// 返回值：error，已核验时为 nil；未核验时包装 ErrContractUnverified。
func RequireVerified(operationID string) error {
	found := false
	for _, spec := range catalog {
		if spec.OperationID == operationID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: 未知 operation %s", ErrContractMismatch, operationID)
	}
	if _, exists := unverifiedOperations[operationID]; exists {
		return fmt.Errorf("%w: %s；请先补齐接口详情页中的请求体定义", ErrContractUnverified, operationID)
	}
	return nil
}

// IsVerified 报告 operation 是否具有可用于真实请求的字段级契约。
// 入参：operationID string 为远端操作标识。
// 返回值：bool，允许真实调用时为 true。
func IsVerified(operationID string) bool {
	found := false
	for _, spec := range catalog {
		if spec.OperationID == operationID {
			found = true
			break
		}
	}
	_, exists := unverifiedOperations[operationID]
	return found && !exists
}

// ValidateRequest 以契约目录为运行时单一真相源，校验 method/path 和对应 Schema 输入。
// 入参：operationID/method/path string 分别为操作标识、HTTP 方法和已展开路径；input any 为逻辑请求输入。
// 返回值：error，操作未知、路由漂移或 Schema 输入不匹配时包装 ErrContractMismatch。
func ValidateRequest(operationID string, method string, path string, input any) error {
	for _, spec := range catalog {
		if spec.OperationID != operationID {
			continue
		}
		if spec.Method != method || !matchesPath(spec.Path, path) {
			return fmt.Errorf("%w: %s 期望 %s %s，实际 %s %s", ErrContractMismatch, operationID, spec.Method, spec.Path, method, path)
		}
		return validateInput(spec, input)
	}
	return fmt.Errorf("%w: 未知 operation %s", ErrContractMismatch, operationID)
}

// matchesPath 比较目录路径模板和已展开路径，花括号参数必须对应一个非空路径段。
// 入参：template/path string 分别为目录模板和实际相对路径。
// 返回值：bool，固定段一致且所有参数段非空时为 true。
func matchesPath(template string, path string) bool {
	templateParts := strings.Split(template, "/")
	pathParts := strings.Split(path, "/")
	if len(templateParts) != len(pathParts) {
		return false
	}
	for index, templatePart := range templateParts {
		isParameter := strings.HasPrefix(templatePart, "{") && strings.HasSuffix(templatePart, "}")
		if isParameter {
			if pathParts[index] == "" {
				return false
			}
			continue
		}
		if templatePart != pathParts[index] {
			return false
		}
	}
	return true
}
