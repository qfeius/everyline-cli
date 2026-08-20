package cli

import (
	"fmt"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/checklist"
	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/contracts"
	"git.qtech.cn/ai/everyline-cli/internal/openplatform"

	"github.com/spf13/cobra"
)

// newChecklistCommand 创建审查清单单条和批量 CRUD 命令组。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，包含七个远端操作。
func newChecklistCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "checklist",
		Short: "管理审查清单",
		Long: `管理审查清单。

审查清单由名称和关联审查规则组成。
		创建、更新和删除属于写操作；批量创建和批量更新当前仅支持 dry-run。`,
	}
	withNotes(command,
		"batch-create 和 batch-update 当前仅支持 dry-run。",
		"delete 和 batch-delete 必须显式提供 --yes。",
	)
	command.AddCommand(
		newChecklistListCommand(runtime, root),
		newChecklistCreateCommand(runtime, root),
		newChecklistBatchCreateCommand(runtime, root),
		newChecklistUpdateCommand(runtime, root),
		newChecklistBatchUpdateCommand(runtime, root),
		newChecklistDeleteCommand(runtime, root),
		newChecklistBatchDeleteCommand(runtime, root),
	)
	return command
}

// newChecklistCreateCommand 创建复杂 JSON 输入的清单创建命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 --input/--data 和 dry-run。
func newChecklistCreateCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var inputPath, inline string
	var dryRun, printInput bool
	command := &cobra.Command{
		Use:   "create",
		Short: "创建审查清单",
		Long:  "创建审查清单，需要通过 --input 或 --data 提供 JSON 请求。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			var payload checklist.Checklist
			if err := readJSONInput(inputPath, inline, &payload); err != nil {
				return err
			}
			if err := payload.Validate(false); err != nil {
				return err
			}
			if dryRun || printInput {
				return render(runtime, root, "json", payload)
			}
			service, profile, err := buildChecklistService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.Create(command.Context(), payload)
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

// newChecklistBatchCreateCommand 创建全有或全无的清单批量创建命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，输入为清单数组。
func newChecklistBatchCreateCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var inputPath, inline string
	var dryRun, printInput bool
	command := &cobra.Command{
		Use:   "batch-create",
		Short: "批量创建审查清单",
		Long:  "批量创建审查清单；当前仅支持 dry-run，真实调用需等待接口契约核验。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			var payload []checklist.Checklist
			if err := readJSONInput(inputPath, inline, &payload); err != nil {
				return err
			}
			if err := validateChecklistBatch(payload, false); err != nil {
				return err
			}
			if dryRun || printInput {
				return render(runtime, root, "json", payload)
			}
			if err := contracts.RequireVerified(checklist.OperationBatchCreate); err != nil {
				return err
			}
			service, profile, err := buildChecklistService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.BatchCreate(command.Context(), payload)
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

// newChecklistListCommand 创建清单过滤、排序和分页查询命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，可查询清单集合。
func newChecklistListCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	query := checklist.Query{}
	var enabled bool
	command := &cobra.Command{
		Use:   "list",
		Short: "查询审查清单",
		Long:  "查询审查清单，支持名称、分类、阶段、启用状态和分页过滤。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if command.Flags().Changed("enabled") {
				query.Enabled = &enabled
			}
			service, profile, err := buildChecklistService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.List(command.Context(), query)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	command.Flags().StringVar(&query.Name, "name", "", "按清单名称过滤")
	command.Flags().StringSliceVar(&query.ReviewStages, "review-stage", nil, "审查阶段，可重复或逗号分隔")
	command.Flags().StringSliceVar(&query.ContractCategories, "contract-category", nil, "合同分类，可重复或逗号分隔")
	command.Flags().Int64Var(&query.StartTime, "start-time", 0, "更新时间开始，Unix 毫秒")
	command.Flags().Int64Var(&query.EndTime, "end-time", 0, "更新时间结束，Unix 毫秒")
	command.Flags().BoolVar(&enabled, "enabled", false, "按启用状态过滤")
	command.Flags().StringSliceVar(&query.CreateEmployeeIDs, "create-employee-id", nil, "创建员工 ID")
	command.Flags().StringSliceVar(&query.UpdateEmployeeIDs, "update-employee-id", nil, "更新员工 ID")
	command.Flags().StringSliceVar(&query.Sort, "sort", nil, "排序：property,(asc|desc)")
	command.Flags().IntVar(&query.PageIndex, "page-index", 0, "页码，从 1 开始")
	command.Flags().IntVar(&query.PageSize, "page-size", 0, "每页数量")
	return command
}

// newChecklistUpdateCommand 创建按路径 ID 更新清单的命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 JSON 输入和 dry-run。
func newChecklistUpdateCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var id, inputPath, inline string
	var dryRun, printInput bool
	command := &cobra.Command{
		Use:   "update",
		Short: "更新审查清单",
		Long:  "更新指定审查清单，需要通过 --id 指定目标清单。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			var payload checklist.Checklist
			if err := readJSONInput(inputPath, inline, &payload); err != nil {
				return err
			}
			resolvedID, normalized, err := payload.NormalizeUpdate(id)
			if err != nil {
				return err
			}
			if dryRun || printInput {
				return render(runtime, root, "json", map[string]any{"id": resolvedID, "data": normalized})
			}
			service, profile, err := buildChecklistService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.Update(command.Context(), resolvedID, normalized)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	command.Flags().StringVar(&id, "id", "", "审查清单 ID")
	_ = command.MarkFlagRequired("id")
	addJSONInputFlags(command, &inputPath, &inline)
	addDryRunFlags(command, &dryRun, &printInput)
	return command
}

// newChecklistBatchUpdateCommand 创建全有或全无的清单批量更新命令。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，数组中每项必须带 id。
func newChecklistBatchUpdateCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var inputPath, inline string
	var dryRun, printInput bool
	command := &cobra.Command{
		Use:   "batch-update",
		Short: "批量更新审查清单",
		Long:  "批量更新审查清单；当前仅支持 dry-run，真实调用需等待接口契约核验。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			var payload []checklist.Checklist
			if err := readJSONInput(inputPath, inline, &payload); err != nil {
				return err
			}
			if err := validateChecklistBatch(payload, true); err != nil {
				return err
			}
			if dryRun || printInput {
				return render(runtime, root, "json", payload)
			}
			if err := contracts.RequireVerified(checklist.OperationBatchUpdate); err != nil {
				return err
			}
			service, profile, err := buildChecklistService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.BatchUpdate(command.Context(), payload)
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

// newChecklistDeleteCommand 创建单条清单删除命令并要求显式 --yes。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，支持 dry-run。
func newChecklistDeleteCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var id string
	var yes, dryRun, printInput bool
	command := &cobra.Command{
		Use:   "delete",
		Short: "删除审查清单",
		Long:  "删除指定审查清单，属于写操作，必须传 --yes。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			resolvedID, err := normalizeRequiredID(id, "id")
			if err != nil {
				return err
			}
			input := map[string]any{"id": resolvedID}
			if dryRun || printInput {
				return render(runtime, root, "json", input)
			}
			if !yes {
				return fmt.Errorf("删除操作必须显式传 --yes")
			}
			service, profile, err := buildChecklistService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.Delete(command.Context(), resolvedID)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	command.Flags().StringVar(&id, "id", "", "审查清单 ID")
	command.Flags().BoolVar(&yes, "yes", false, "确认删除")
	_ = command.MarkFlagRequired("id")
	addDryRunFlags(command, &dryRun, &printInput)
	return command
}

// newChecklistBatchDeleteCommand 创建批量删除命令，支持重复 --id 或 JSON 数组。
// 入参：runtime *Runtime 为运行时；root *rootOptions 为公共 flags。
// 返回值：*cobra.Command，执行前要求显式 --yes。
func newChecklistBatchDeleteCommand(runtime *Runtime, root *rootOptions) *cobra.Command {
	var ids []string
	var inputPath, inline string
	var yes, dryRun, printInput bool
	command := &cobra.Command{
		Use:   "batch-delete",
		Short: "批量删除审查清单",
		Long:  "批量删除指定审查清单，属于写操作，必须传 --yes。",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			resolved, err := readIDList(ids, inputPath, inline)
			if err != nil {
				return err
			}
			if dryRun || printInput {
				// dry-run 与真实 DELETE body 使用同一对象形状，避免脚本依据错误预览生成请求。
				return render(runtime, root, "json", map[string]any{"ids": resolved})
			}
			if !yes {
				return fmt.Errorf("批量删除必须显式传 --yes")
			}
			service, profile, err := buildChecklistService(runtime, root)
			if err != nil {
				return err
			}
			result, err := service.BatchDelete(command.Context(), resolved)
			if err != nil {
				return err
			}
			return render(runtime, root, profile.DefaultOutput, result)
		},
	}
	command.Flags().StringSliceVar(&ids, "id", nil, "审查清单 ID，可重复或逗号分隔")
	addJSONInputFlags(command, &inputPath, &inline)
	command.Flags().BoolVar(&yes, "yes", false, "确认批量删除")
	addDryRunFlags(command, &dryRun, &printInput)
	return command
}

// buildChecklistService 装配 Profile、TokenProvider、HTTP Adapter 和清单 Service。
// 入参：runtime *Runtime 为依赖；root *rootOptions 为 Profile 和超时 flags。
// 返回值：*checklist.Service 为领域客户端；config.Profile 为渲染默认值；error 为配置失败。
func buildChecklistService(runtime *Runtime, root *rootOptions) (*checklist.Service, config.Profile, error) {
	profile, err := selectedProfile(runtime, root)
	if err != nil {
		return nil, config.Profile{}, err
	}
	identity, err := selectedIdentity(profile, root.Identity)
	if err != nil {
		return nil, config.Profile{}, err
	}
	provider := auth.NewProvider(runtime.Tokens, runtime.HTTP, runtime.Now, runtime.Secrets)
	client := openplatform.NewClientForIdentity(profile, provider, runtime.HTTP, identity)
	return checklist.NewService(client, root.Timeout), profile, nil
}

// validateChecklistBatch 在 dry-run 前执行与 Service 一致的批量输入校验。
// 入参：payload []checklist.Checklist 为数组；requireID bool 表示每项是否必须带 ID。
// 返回值：error，数组有效时为 nil。
func validateChecklistBatch(payload []checklist.Checklist, requireID bool) error {
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

// addDryRunFlags 为写操作注册一致的本地校验和规范化输出参数。
// 入参：command *cobra.Command 为目标命令；dryRun/printInput *bool 接收 flag 值。
// 返回值：无。
func addDryRunFlags(command *cobra.Command, dryRun *bool, printInput *bool) {
	command.Flags().BoolVar(dryRun, "dry-run", false, "只校验并输出请求，不调用远端")
	command.Flags().BoolVar(printInput, "print-input", false, "输出规范化请求，不调用远端")
}

// readIDList 从重复 --id 或 --input/--data JSON 数组二选一读取删除 ID。
// 入参：ids []string 为 flags；inputPath/inline string 为 JSON 来源。
// 返回值：[]string 为去除空白的 ID；error 为来源冲突、空值或 JSON 错误。
func readIDList(ids []string, inputPath string, inline string) ([]string, error) {
	hasFlags := len(ids) > 0
	hasJSON := inputPath != "" || inline != ""
	if hasFlags == hasJSON {
		return nil, fmt.Errorf("必须且只能使用 --id 或 --input/--data 之一")
	}
	resolved := ids
	if hasJSON {
		if err := readJSONInput(inputPath, inline, &resolved); err != nil {
			return nil, err
		}
	}
	return contracts.NormalizeUniqueIDs(resolved, "id")
}
