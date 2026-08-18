package cli

import (
	"fmt"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/contracts"
	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
	"git.qtech.cn/ai/everyline-cli/internal/rule"

	"github.com/spf13/cobra"
)

// newRuleCommand 创建规则分组和分组内规则的管理命令树。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，包含 group CRUD 和规则单条/批量 CRUD。
func newRuleCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "rule", Short: "管理审查规则"}
	command.AddCommand(
		newRuleGroupCommand(runtime, root),
		newRuleCreateCommand(runtime, root),
		newRuleBatchCreateCommand(runtime, root),
		newRuleListCommand(runtime, root),
		newRuleUpdateCommand(runtime, root),
		newRuleBatchUpdateCommand(runtime, root),
		newRuleDeleteCommand(runtime, root),
		newRuleBatchDeleteCommand(runtime, root),
	)
	return command
}

// newRuleGroupCommand 创建规则分组 CRUD 命令组。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，包含 create/list/update/delete。
func newRuleGroupCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "group", Short: "管理审查规则分组"}
	command.AddCommand(
		newRuleGroupCreateCommand(runtime, root),
		newRuleGroupListCommand(runtime, root),
		newRuleGroupUpdateCommand(runtime, root),
		newRuleGroupDeleteCommand(runtime, root),
	)
	return command
}

// newRuleGroupCreateCommand 创建复杂 JSON 输入的规则分组创建命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 --input/--data 和 dry-run。
func newRuleGroupCreateCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var inputPath, inline string
	var dryRun, printInput bool
	command := &cobra.Command{
		Use: "create", Short: "创建规则分组", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			var payload rule.Group
			if err := readJSONInput(inputPath, inline, &payload); err != nil {
				return err
			}
			if err := payload.Validate(false); err != nil {
				return err
			}
			if dryRun || printInput {
				return render(runtime, root, "json", payload)
			}
			service, profile, err := buildRuleService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.CreateGroup(command.Context(), payload)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	addJSONInputFlags(command, &inputPath, &inline)
	addDryRunFlags(command, &dryRun, &printInput)
	return command
}

// newRuleGroupListCommand 创建规则分组分页排序查询命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，可查询分组集合。
func newRuleGroupListCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	query := rule.Query{}
	command := &cobra.Command{
		Use: "list", Short: "查询规则分组", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			service, profile, err := buildRuleService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.ListGroups(command.Context(), query)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	addRuleListFlags(command, &query)
	return command
}

// newRuleGroupUpdateCommand 创建按路径 ID 更新规则分组的命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 JSON 输入和 dry-run。
func newRuleGroupUpdateCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var id, inputPath, inline string
	var dryRun, printInput bool
	command := &cobra.Command{
		Use: "update", Short: "更新规则分组", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			resolvedID, err := normalizeRequiredID(id, "id")
			if err != nil {
				return err
			}
			var payload rule.Group
			if err := readJSONInput(inputPath, inline, &payload); err != nil {
				return err
			}
			if err := payload.Validate(false); err != nil {
				return err
			}
			if dryRun || printInput {
				return render(runtime, root, "json", map[string]any{"id": resolvedID, "data": payload})
			}
			service, profile, err := buildRuleService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.UpdateGroup(command.Context(), resolvedID, payload)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	command.Flags().StringVar(&id, "id", "", "规则分组 ID")
	_ = command.MarkFlagRequired("id")
	addJSONInputFlags(command, &inputPath, &inline)
	addDryRunFlags(command, &dryRun, &printInput)
	return command
}

// newRuleGroupDeleteCommand 创建级联删除规则分组命令并要求显式 --yes。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 dry-run。
func newRuleGroupDeleteCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var id string
	var yes, dryRun, printInput bool
	command := &cobra.Command{
		Use: "delete", Short: "级联删除规则分组及其规则", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			resolvedID, err := normalizeRequiredID(id, "id")
			if err != nil {
				return err
			}
			input := map[string]any{"id": resolvedID, "cascade": true}
			if dryRun || printInput {
				return render(runtime, root, "json", input)
			}
			if !yes {
				return fmt.Errorf("级联删除规则分组必须显式传 --yes")
			}
			service, profile, err := buildRuleService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.DeleteGroup(command.Context(), resolvedID)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	command.Flags().StringVar(&id, "id", "", "规则分组 ID")
	command.Flags().BoolVar(&yes, "yes", false, "确认级联删除")
	_ = command.MarkFlagRequired("id")
	addDryRunFlags(command, &dryRun, &printInput)
	return command
}

// newRuleCreateCommand 创建指定分组内的规则创建命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 JSON 输入和 dry-run。
func newRuleCreateCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var groupID, inputPath, inline string
	var dryRun, printInput bool
	command := &cobra.Command{
		Use: "create", Short: "创建审查规则", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			var payload rule.Rule
			if err := readJSONInput(inputPath, inline, &payload); err != nil {
				return err
			}
			if err := payload.Validate(false); err != nil {
				return err
			}
			if dryRun || printInput {
				return render(runtime, root, "json", map[string]any{"groupId": groupID, "data": payload})
			}
			service, profile, err := buildRuleService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.CreateRule(command.Context(), groupID, payload)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	addGroupIDFlag(command, &groupID)
	addJSONInputFlags(command, &inputPath, &inline)
	addDryRunFlags(command, &dryRun, &printInput)
	return command
}

// newRuleBatchCreateCommand 创建同一分组内全有或全无的规则批量创建命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，输入为规则数组。
func newRuleBatchCreateCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var groupID, inputPath, inline string
	var dryRun, printInput bool
	command := &cobra.Command{
		Use: "batch-create", Short: "批量创建审查规则", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			var payload []rule.Rule
			if err := readJSONInput(inputPath, inline, &payload); err != nil {
				return err
			}
			if err := validateRuleBatch(payload, false); err != nil {
				return err
			}
			if dryRun || printInput {
				return render(runtime, root, "json", map[string]any{"groupId": groupID, "data": payload})
			}
			if err := contracts.RequireVerified(rule.OperationBatchCreateRule); err != nil {
				return err
			}
			service, profile, err := buildRuleService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.BatchCreateRules(command.Context(), groupID, payload)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	addGroupIDFlag(command, &groupID)
	addJSONInputFlags(command, &inputPath, &inline)
	addDryRunFlags(command, &dryRun, &printInput)
	return command
}

// newRuleListCommand 创建指定分组内的规则分页排序查询命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，可查询分组内规则。
func newRuleListCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var groupID string
	query := rule.Query{}
	command := &cobra.Command{
		Use: "list", Short: "查询审查规则", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			service, profile, err := buildRuleService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.ListRules(command.Context(), groupID, query)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	addGroupIDFlag(command, &groupID)
	addRuleListFlags(command, &query)
	return command
}

// newRuleUpdateCommand 创建指定分组内的单条规则更新命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 JSON 输入和 dry-run。
func newRuleUpdateCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var groupID, ruleID, inputPath, inline string
	var dryRun, printInput bool
	command := &cobra.Command{
		Use: "update", Short: "更新审查规则", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			resolvedRuleID, err := normalizeRequiredID(ruleID, "rule-id")
			if err != nil {
				return err
			}
			var payload rule.Rule
			if err := readJSONInput(inputPath, inline, &payload); err != nil {
				return err
			}
			if err := payload.Validate(false); err != nil {
				return err
			}
			if dryRun || printInput {
				return render(runtime, root, "json", map[string]any{"groupId": groupID, "ruleId": resolvedRuleID, "data": payload})
			}
			service, profile, err := buildRuleService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.UpdateRule(command.Context(), groupID, resolvedRuleID, payload)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	addGroupIDFlag(command, &groupID)
	command.Flags().StringVar(&ruleID, "rule-id", "", "审查规则 ID")
	_ = command.MarkFlagRequired("rule-id")
	addJSONInputFlags(command, &inputPath, &inline)
	addDryRunFlags(command, &dryRun, &printInput)
	return command
}

// newRuleBatchUpdateCommand 创建同一分组内全有或全无的规则批量更新命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，数组中每项必须带 id。
func newRuleBatchUpdateCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var groupID, inputPath, inline string
	var dryRun, printInput bool
	command := &cobra.Command{
		Use: "batch-update", Short: "批量更新审查规则", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			var payload []rule.Rule
			if err := readJSONInput(inputPath, inline, &payload); err != nil {
				return err
			}
			if err := validateRuleBatch(payload, true); err != nil {
				return err
			}
			if dryRun || printInput {
				return render(runtime, root, "json", map[string]any{"groupId": groupID, "data": payload})
			}
			if err := contracts.RequireVerified(rule.OperationBatchUpdateRule); err != nil {
				return err
			}
			service, profile, err := buildRuleService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.BatchUpdateRules(command.Context(), groupID, payload)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	addGroupIDFlag(command, &groupID)
	addJSONInputFlags(command, &inputPath, &inline)
	addDryRunFlags(command, &dryRun, &printInput)
	return command
}

// newRuleDeleteCommand 创建指定分组内的单条规则删除命令并要求 --yes。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 dry-run。
func newRuleDeleteCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var groupID, ruleID string
	var yes, dryRun, printInput bool
	command := &cobra.Command{
		Use: "delete", Short: "删除审查规则", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			resolvedRuleID, err := normalizeRequiredID(ruleID, "rule-id")
			if err != nil {
				return err
			}
			input := map[string]any{"groupId": groupID, "ruleId": resolvedRuleID}
			if dryRun || printInput {
				return render(runtime, root, "json", input)
			}
			if !yes {
				return fmt.Errorf("删除规则必须显式传 --yes")
			}
			service, profile, err := buildRuleService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.DeleteRule(command.Context(), groupID, resolvedRuleID)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	addGroupIDFlag(command, &groupID)
	command.Flags().StringVar(&ruleID, "rule-id", "", "审查规则 ID")
	command.Flags().BoolVar(&yes, "yes", false, "确认删除")
	_ = command.MarkFlagRequired("rule-id")
	addDryRunFlags(command, &dryRun, &printInput)
	return command
}

// newRuleBatchDeleteCommand 创建同一分组内的规则批量删除命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持重复 --id 或 JSON 数组并要求 --yes。
func newRuleBatchDeleteCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var groupID, inputPath, inline string
	var ids []string
	var yes, dryRun, printInput bool
	command := &cobra.Command{
		Use: "batch-delete", Short: "批量删除审查规则", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			resolved, err := readIDList(ids, inputPath, inline)
			if err != nil {
				return err
			}
			input := map[string]any{"groupId": groupID, "ids": resolved}
			if dryRun || printInput {
				return render(runtime, root, "json", input)
			}
			if !yes {
				return fmt.Errorf("批量删除规则必须显式传 --yes")
			}
			service, profile, err := buildRuleService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.BatchDeleteRules(command.Context(), groupID, resolved)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	addGroupIDFlag(command, &groupID)
	command.Flags().StringSliceVar(&ids, "id", nil, "审查规则 ID，可重复或逗号分隔")
	addJSONInputFlags(command, &inputPath, &inline)
	command.Flags().BoolVar(&yes, "yes", false, "确认批量删除")
	addDryRunFlags(command, &dryRun, &printInput)
	return command
}

// buildRuleService 装配 Profile、TokenProvider、HTTP Adapter 和规则 Service。
// 入参：runtime *Runtime 为依赖；root *rootOptions 为 Profile 和超时 flags。
// 返回值：*rule.Service 为领域客户端；config.Profile 为渲染默认值；error 为配置失败。
func buildRuleService(runtime *Runtime, root *rootOptions) (*rule.Service, config.Profile, error) {
	profile, err := selectedProfile(runtime, root)
	if err != nil {
		return nil, config.Profile{}, err
	}
	provider := auth.NewProvider(runtime.Tokens, runtime.HTTP, runtime.Now)
	client := openplatform.NewClient(profile, provider, runtime.HTTP)
	return rule.NewService(client, root.Timeout), profile, nil
}

// addGroupIDFlag 注册规则操作共用的必填分组 ID。
// 入参：command *cobra.Command 为目标命令；groupID *string 接收 flag 值。
// 返回值：无。
func addGroupIDFlag(command *cobra.Command, groupID *string) {
	command.Flags().StringVar(groupID, "group-id", "", "规则分组 ID")
	_ = command.MarkFlagRequired("group-id")
	command.PreRunE = func(command *cobra.Command, args []string) error {
		resolved, err := normalizeRequiredID(*groupID, "group-id")
		if err != nil {
			return err
		}
		*groupID = resolved
		return nil
	}
}

// addRuleListFlags 注册规则分组和规则共用的分页排序参数。
// 入参：command *cobra.Command 为目标命令；query *rule.Query 接收值。
// 返回值：无。
func addRuleListFlags(command *cobra.Command, query *rule.Query) {
	command.Flags().StringSliceVar(&query.Sort, "sort", nil, "排序：property,(asc|desc)")
	command.Flags().IntVar(&query.PageIndex, "page-index", 0, "页码，从 1 开始")
	command.Flags().IntVar(&query.PageSize, "page-size", 0, "每页数量")
}

// validateRuleBatch 在 dry-run 前执行与 Service 一致的批量输入校验。
// 入参：payload []rule.Rule 为数组；requireID bool 表示每项是否必须带 ID。
// 返回值：error，数组有效时为 nil。
func validateRuleBatch(payload []rule.Rule, requireID bool) error {
	if len(payload) == 0 {
		return fmt.Errorf("批量请求不能为空")
	}
	for index, item := range payload {
		if err := item.Validate(requireID); err != nil {
			return fmt.Errorf("第 %d 项: %w", index+1, err)
		}
	}
	return nil
}
