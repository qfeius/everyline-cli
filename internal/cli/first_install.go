package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/build"
	"git.qtech.cn/ai/everyline-cli/internal/config"

	"github.com/spf13/cobra"
)

// 首次安装说明能力并提供授权帮助，用户要求登录后再选择授权身份。
const firstInstallMessage = "EveryLine CLI 已安装完成。目前支持合同审查，以及审查清单、规则和规则分组配置。使用前需要先完成账号授权，我现在可以为你打开授权页面或生成授权链接。"

// firstInstallEvent 是首次安装期间写入 stderr 的稳定 NDJSON 事件。
type firstInstallEvent struct {
	Schema                string `json:"schema"`
	Event                 string `json:"event"`
	EventID               string `json:"eventId"`
	CLIVersion            string `json:"cliVersion"`
	RecommendedSkill      string `json:"recommendedSkill"`
	AuthorizationRequired bool   `json:"authorizationRequired"`
	NextAction            string `json:"nextAction"`
	Message               string `json:"message"`
}

/*
newRecordInstallCommand 为 npm 安装器提供隐藏的本地状态登记入口，复用 CLI 的跨进程锁。
入参：runtime *Runtime 为安装状态仓库和标准输出。
返回值：*cobra.Command，仅输出安装状态与 updated 标记，不进行授权或网络请求。
*/
func newRecordInstallCommand(runtime *Runtime) *cobra.Command {
	var version, eventID string
	var previouslyInstalled bool
	command := &cobra.Command{
		Use: "_record-install", Hidden: true, GroupID: "cli", Args: cobra.NoArgs,
		// 安装器在锁内处理状态；不提前输出首次安装事件或检查业务门禁。
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(*cobra.Command, []string) error {
			if runtime.InstallState == nil {
				return fmt.Errorf("当前运行时缺少安装状态仓库")
			}
			state, updated, err := runtime.InstallState.RecordInstallation(version, eventID, previouslyInstalled)
			if err != nil {
				return err
			}
			return json.NewEncoder(runtime.Output).Encode(struct {
				config.InstallState
				Updated bool `json:"updated"`
			}{state, updated})
		},
	}
	command.Flags().StringVar(&version, "installed-version", "", "npm 包版本")
	command.Flags().StringVar(&eventID, "event-id", "", "候选安装事件 ID")
	command.Flags().BoolVar(&previouslyInstalled, "previously-installed", false, "已存在旧 Skill 登记")
	return command
}

// pendingFirstInstallAuthorization 读取首次安装授权门禁；旧版本没有状态文件时保持兼容。
// 入参：runtime *Runtime 为安装状态依赖。
// 返回值：InstallState 为状态；bool 表示是否待授权；error 为状态读取或校验失败。
func pendingFirstInstallAuthorization(runtime *Runtime) (config.InstallState, bool, error) {
	if runtime.InstallState == nil {
		return config.InstallState{}, false, nil
	}
	state, err := runtime.InstallState.Load()
	if errors.Is(err, config.ErrInstallStateNotFound) {
		return config.InstallState{}, false, nil
	}
	if err != nil {
		return config.InstallState{}, false, fmt.Errorf("读取首次安装状态: %w", err)
	}
	return state, state.AuthorizationRequired, nil
}

/*
completeFirstInstallAuthorization 在真实授权成功并保存凭证后解除门禁，支持按事件恢复 Device 的本地提交。
入参：runtime *Runtime 为安装状态依赖；expectedEventID ...string 为 Device token 对应事件，省略时用于本次新完成的本地登录。
返回值：error，事件不匹配或落盘失败时非 nil；旧安装没有状态仓库时幂等成功。
*/
func completeFirstInstallAuthorization(runtime *Runtime, expectedEventID ...string) error {
	if runtime.InstallState == nil {
		return nil
	}
	if err := runtime.InstallState.CompleteAuthorization(expectedEventID...); err != nil {
		if errors.Is(err, config.ErrInstallAuthorizationEventMismatch) {
			return fmt.Errorf("%w；当前首次安装需要新的 Device 授权，请执行 auth init --restart", auth.ErrUserAuthentication)
		}
		return fmt.Errorf("保存首次安装授权结果: %w", err)
	}
	return nil
}

// emitFirstInstallEvent 把首次安装状态作为单行 JSON 写入 stderr，避免污染业务 stdout。
// 入参：runtime *Runtime 提供 stderr；state config.InstallState 提供稳定 eventId。
// 返回值：error，编码或写入失败时非 nil。
func emitFirstInstallEvent(runtime *Runtime, state config.InstallState) error {
	payload, err := json.Marshal(firstInstallEvent{
		Schema:                "everyline.skill-event.v1",
		Event:                 "first_install",
		EventID:               state.EventID,
		CLIVersion:            build.Current().Version,
		RecommendedSkill:      "everyline-cli",
		AuthorizationRequired: true,
		NextAction:            "authorize",
		Message:               firstInstallMessage,
	})
	if err != nil {
		return fmt.Errorf("编码首次安装事件: %w", err)
	}
	if _, err := fmt.Fprintln(runtime.Error, string(payload)); err != nil {
		return fmt.Errorf("输出首次安装事件: %w", err)
	}
	return nil
}

// firstInstallAuthorizationCommand 按身份和宿主返回可直接执行的新授权命令。
// 入参：runtime *Runtime 用于识别 Device 沙箱；profile config.Profile 为当前 Profile；identity config.IdentityKind 为目标身份。
// 返回值：string，为不含凭证值的下一步命令。
func firstInstallAuthorizationCommand(runtime *Runtime, profile config.Profile, identity config.IdentityKind) string {
	if identity == config.IdentityUser && runtime.DeviceCredentials != nil {
		return "everyline-cli auth init --restart --profile " + profile.Name + " --as user --output json"
	}
	return "everyline-cli auth login --profile " + profile.Name + " --as " + string(identity) + " --output json"
}
