package checklist

import (
	"fmt"
	"strings"
)

// Checklist 是审查清单创建和更新的字段级请求契约。
type Checklist struct {
	ID               string   `json:"id,omitempty" yaml:"id,omitempty"`
	Name             string   `json:"name" yaml:"name"`
	ContractCategory []string `json:"contractCategory,omitempty" yaml:"contractCategory,omitempty"`
	ReviewStage      []int32  `json:"reviewStage,omitempty" yaml:"reviewStage,omitempty"`
	Enabled          *bool    `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	ReviewRuleIDs    []string `json:"reviewRuleIds,omitempty" yaml:"reviewRuleIds,omitempty"`
}

// Validate 校验公开接口声明的清单名称、规则 ID 列表以及批量更新所需 ID。
// 入参：requireID bool 表示是否要求 body 中包含 id。
// 返回值：error，请求满足创建或更新契约时为 nil。
func (checklist Checklist) Validate(requireID bool) error {
	if requireID && strings.TrimSpace(checklist.ID) == "" {
		return fmt.Errorf("id 不能为空")
	}
	if strings.TrimSpace(checklist.Name) == "" {
		return fmt.Errorf("name 不能为空")
	}
	if len(checklist.ReviewRuleIDs) == 0 {
		return fmt.Errorf("reviewRuleIds 不能为空")
	}
	for index, ruleID := range checklist.ReviewRuleIDs {
		if strings.TrimSpace(ruleID) == "" {
			return fmt.Errorf("第 %d 个 reviewRuleIds 不能为空", index+1)
		}
	}
	return nil
}

// Query 是审查清单列表接口的稳定过滤与分页参数。
type Query struct {
	Name               string
	ReviewStages       []string
	ContractCategories []string
	StartTime          int64
	EndTime            int64
	Enabled            *bool
	CreateEmployeeIDs  []string
	UpdateEmployeeIDs  []string
	Sort               []string
	PageIndex          int
	PageSize           int
}

// Validate 校验分页和时间范围，避免生成远端无法解释的查询。
// 入参：无，接收者 Query 为列表条件。
// 返回值：error，查询参数有效时为 nil。
func (query Query) Validate() error {
	if query.PageIndex < 0 || query.PageSize < 0 {
		return fmt.Errorf("page-index 和 page-size 不能小于 0")
	}
	if query.StartTime < 0 || query.EndTime < 0 {
		return fmt.Errorf("start-time 和 end-time 不能小于 0")
	}
	if query.StartTime > 0 && query.EndTime > 0 && query.StartTime > query.EndTime {
		return fmt.Errorf("start-time 不能晚于 end-time")
	}
	return nil
}
