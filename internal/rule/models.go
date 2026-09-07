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

// NormalizeUpdate 校验路径 ID 与可选 body ID 一致，并移除单项分组更新请求体中的冗余 ID。
// 入参：pathID string 为 URL 路径中的分组 ID。
// 返回值：string 为去除空白的路径 ID；Group 为不含 body ID 的请求；error 为 ID 冲突或字段校验错误。
func (group Group) NormalizeUpdate(pathID string) (string, Group, error) {
	resolvedID := strings.TrimSpace(pathID)
	if resolvedID == "" {
		return "", Group{}, fmt.Errorf("id 不能为空")
	}
	bodyID := strings.TrimSpace(group.ID)
	if bodyID != "" && bodyID != resolvedID {
		return "", Group{}, fmt.Errorf("请求体 id %q 与路径 id %q 不一致", bodyID, resolvedID)
	}
	// 单项更新的资源身份只由路径确定，避免发送两个可能漂移的真相源。
	group.ID = ""
	if err := group.Validate(false); err != nil {
		return "", Group{}, err
	}
	return resolvedID, group, nil
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
	if group.SourceType != nil && *group.SourceType < 0 {
		return fmt.Errorf("sourceType 不能小于 0")
	}
	return nil
}

// Rule 是审查规则创建和更新的字段级请求契约；所属分组由路径 groupId 确定。
type Rule struct {
	ID         string `json:"id,omitempty" yaml:"id,omitempty"`
	Name       string `json:"name" yaml:"name"`
	RiskLevel  *int32 `json:"riskLevel" yaml:"riskLevel"`
	RiskTips   string `json:"riskTips,omitempty" yaml:"riskTips,omitempty"`
	Content    string `json:"content" yaml:"content"`
	SourceType *int   `json:"sourceType,omitempty" yaml:"sourceType,omitempty"`
}

// NormalizeUpdate 校验路径 ID 与可选 body ID 一致，并移除单项规则更新请求体中的冗余 ID。
// 入参：pathID string 为 URL 路径中的规则 ID。
// 返回值：string 为去除空白的路径 ID；Rule 为不含 body ID 的请求；error 为 ID 冲突或字段校验错误。
func (rule Rule) NormalizeUpdate(pathID string) (string, Rule, error) {
	resolvedID := strings.TrimSpace(pathID)
	if resolvedID == "" {
		return "", Rule{}, fmt.Errorf("rule-id 不能为空")
	}
	bodyID := strings.TrimSpace(rule.ID)
	if bodyID != "" && bodyID != resolvedID {
		return "", Rule{}, fmt.Errorf("请求体 id %q 与路径 rule-id %q 不一致", bodyID, resolvedID)
	}
	// 单项更新的资源身份只由路径确定，避免发送两个可能漂移的真相源。
	rule.ID = ""
	if err := rule.Validate(false); err != nil {
		return "", Rule{}, err
	}
	return resolvedID, rule, nil
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
	if rule.RiskLevel == nil {
		return fmt.Errorf("riskLevel 不能为空")
	}
	if *rule.RiskLevel < 0 {
		return fmt.Errorf("riskLevel 不能小于 0")
	}
	if rule.SourceType != nil && *rule.SourceType < 0 {
		return fmt.Errorf("sourceType 不能小于 0")
	}
	return nil
}

// Query 是规则分组和规则列表接口共用的分页排序参数。
type Query struct {
	Sort      []string `json:"sort,omitempty"`
	PageIndex int      `json:"pageIndex,omitempty"`
	PageSize  int      `json:"pageSize,omitempty"`
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
