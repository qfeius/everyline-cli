package contracts

import (
	"fmt"
	"strings"
)

// NormalizeUniqueIDs 去除资源 ID 首尾空白，并按 Schema uniqueItems 语义拒绝重复项。
// 入参：ids []string 为原始 ID；field string 为错误消息中的字段名。
// 返回值：[]string 为规范化副本；error 在列表为空、元素为空或重复时非 nil。
func NormalizeUniqueIDs(ids []string, field string) ([]string, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("%s 列表不能为空", field)
	}
	resolved := make([]string, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for index, id := range ids {
		resolved[index] = strings.TrimSpace(id)
		if resolved[index] == "" {
			return nil, fmt.Errorf("第 %d 个 %s 不能为空", index+1, field)
		}
		if _, exists := seen[resolved[index]]; exists {
			return nil, fmt.Errorf("第 %d 个 %s 与前项重复: %s", index+1, field, resolved[index])
		}
		seen[resolved[index]] = struct{}{}
	}
	return resolved, nil
}
