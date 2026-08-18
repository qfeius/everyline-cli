package rule

import (
	"fmt"
	"strings"
)

// Group 是审查规则分组创建和更新的字段级请求契约。
type Group struct {
	ID         string `json:"id,omitempty" yaml:"id,omitempty"`
	Name       string `json:"name" yaml:"name"`
	SourceType *int   `json:"sourceType,omitempty" yaml:"sourceType,omitempty"`
}

// Validate 校验分组名称以及批量场景可选的 body ID。
// 入参：requireID bool 表示是否要求 body 中包含 id。
// 返回值：error，请求有效时为 nil。
func (group Group) Validate(requireID bool) error {
	if requireID && strings.TrimSpace(group.ID) == "" {
		return fmt.Errorf("id 不能为空")
	}
	if strings.TrimSpace(group.Name) == "" {
		return fmt.Errorf("name 不能为空")
	}
	return nil
}

// Rule 是审查规则创建和更新的字段级请求契约；所属分组由路径 groupId 确定。
type Rule struct {
	ID         string `json:"id,omitempty" yaml:"id,omitempty"`
	Name       string `json:"name" yaml:"name"`
	RiskLevel  int32  `json:"riskLevel" yaml:"riskLevel"`
	RiskTips   string `json:"riskTips,omitempty" yaml:"riskTips,omitempty"`
	Content    string `json:"content" yaml:"content"`
	SourceType *int   `json:"sourceType,omitempty" yaml:"sourceType,omitempty"`
}

// Validate 校验规则名称、内容、风险等级以及批量更新所需 ID。
// 入参：requireID bool 表示是否要求 body 中包含 id。
// 返回值：error，请求有效时为 nil。
func (rule Rule) Validate(requireID bool) error {
	if requireID && strings.TrimSpace(rule.ID) == "" {
		return fmt.Errorf("id 不能为空")
	}
	if strings.TrimSpace(rule.Name) == "" {
		return fmt.Errorf("name 不能为空")
	}
	if strings.TrimSpace(rule.Content) == "" {
		return fmt.Errorf("content 不能为空")
	}
	if rule.RiskLevel < 0 {
		return fmt.Errorf("riskLevel 不能小于 0")
	}
	return nil
}

// Query 是规则分组和规则列表接口共用的分页排序参数。
type Query struct {
	Sort      []string
	PageIndex int
	PageSize  int
}

// Validate 校验分页参数不为负数。
// 入参：无，接收者 Query 为分页排序条件。
// 返回值：error，查询有效时为 nil。
func (query Query) Validate() error {
	if query.PageIndex < 0 || query.PageSize < 0 {
		return fmt.Errorf("page-index 和 page-size 不能小于 0")
	}
	return nil
}
