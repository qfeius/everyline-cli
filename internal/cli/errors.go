package cli

import (
	"context"
	"errors"
	"net"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/contracts"
	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
	"git.qtech.cn/ai/everyline-cli/internal/review"
)

const (
	ExitSuccess = 0
	ExitUsage   = 2
	ExitAuth    = 3
	ExitAPI     = 4
	ExitNetwork = 5
)

// ExitCode 将领域错误映射为稳定的进程退出码。
// 入参：err error 为命令执行错误。
// 返回值：int，0/2/3/4/5 之一。
func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	if errors.Is(err, auth.ErrUserSessionExpired) {
		return ExitAuth
	}
	var apiError *openplatform.APIError
	if errors.As(err, &apiError) {
		return ExitAPI
	}
	if errors.Is(err, contracts.ErrContractUnverified) || errors.Is(err, contracts.ErrContractMismatch) || errors.Is(err, review.ErrTaskFailed) {
		return ExitAPI
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ExitNetwork
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return ExitNetwork
	}
	if errors.Is(err, auth.ErrCredentialsMissing) || errors.Is(err, auth.ErrAuthentication) || errors.Is(err, auth.ErrUserAuthentication) || errors.Is(err, config.ErrNoActiveProfile) || errors.Is(err, config.ErrProfileNotFound) {
		return ExitAuth
	}
	return ExitUsage
}
