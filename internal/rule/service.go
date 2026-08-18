package rule

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/contracts"
	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
)

const (
	OperationCreateGroup = "createReviewRuleGroup"
	OperationListGroups  = "listReviewRuleGroups"
	OperationUpdateGroup = "updateReviewRuleGroup"
	OperationDeleteGroup = "deleteReviewRuleGroup"

	OperationCreateRule      = "createReviewRule"
	OperationBatchCreateRule = "batchCreateReviewRules"
	OperationListRules       = "listReviewRules"
	OperationUpdateRule      = "updateReviewRule"
	OperationBatchUpdateRule = "batchUpdateReviewRules"
	OperationDeleteRule      = "deleteReviewRule"
	OperationBatchDeleteRule = "batchDeleteReviewRules"

	pathGroups = "/open-apis/review-rules/review-rule-groups"
)

// Client 是 rule service 依赖的受控 HTTP Adapter 边界。
type Client interface {
	Do(context.Context, openplatform.Request) (openplatform.Response, error)
}

// Service 将四个规则分组和七个规则操作映射为 method/path/body 契约。
type Service struct {
	client  Client
	timeout time.Duration
}

// NewService 创建审查规则领域服务。
// 入参：client Client 为 HTTP Adapter；timeout time.Duration 为远端请求超时。
// 返回值：*Service，可执行规则分组与规则 CRUD。
func NewService(client Client, timeout time.Duration) *Service {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Service{client: client, timeout: timeout}
}

// CreateGroup 创建规则分组。
// 入参：ctx context.Context 控制取消；payload Group 为分组字段。
// 返回值：any 为业务 data；error 为校验、编码或远端错误。
func (service *Service) CreateGroup(ctx context.Context, payload Group) (any, error) {
	if err := payload.Validate(false); err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationCreateGroup, http.MethodPost, pathGroups, nil, payload)
}

// ListGroups 查询规则分组。
// 入参：ctx context.Context 控制取消；query Query 为分页排序条件。
// 返回值：any 为业务 data；error 为校验或远端错误。
func (service *Service) ListGroups(ctx context.Context, query Query) (any, error) {
	values, err := queryValues(query)
	if err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationListGroups, http.MethodGet, pathGroups, values, nil)
}

// UpdateGroup 更新路径 ID 指定的规则分组。
// 入参：ctx context.Context 控制取消；id string 为分组 ID；payload Group 为更新字段。
// 返回值：any 为业务 data；error 为输入、编码或远端错误。
func (service *Service) UpdateGroup(ctx context.Context, id string, payload Group) (any, error) {
	resolvedID, err := requireID(id, "id")
	if err != nil {
		return nil, err
	}
	if err := payload.Validate(false); err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationUpdateGroup, http.MethodPut, pathGroups+"/"+url.PathEscape(resolvedID), nil, payload)
}

// DeleteGroup 删除规则分组；调用方负责在执行前取得级联删除确认。
// 入参：ctx context.Context 控制取消；id string 为分组 ID。
// 返回值：any 为业务 data；error 为输入或远端错误。
func (service *Service) DeleteGroup(ctx context.Context, id string) (any, error) {
	resolvedID, err := requireID(id, "id")
	if err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationDeleteGroup, http.MethodDelete, pathGroups+"/"+url.PathEscape(resolvedID), nil, nil)
}

// CreateRule 在指定分组内创建规则。
// 入参：ctx context.Context 控制取消；groupID string 为分组 ID；payload Rule 为规则字段。
// 返回值：any 为业务 data；error 为校验、编码或远端错误。
func (service *Service) CreateRule(ctx context.Context, groupID string, payload Rule) (any, error) {
	collection, err := ruleCollectionPath(groupID)
	if err != nil {
		return nil, err
	}
	if err := payload.Validate(false); err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationCreateRule, http.MethodPost, collection, nil, payload)
}

// BatchCreateRules 在同一分组内以全有或全无语义批量创建规则。
// 入参：ctx context.Context 控制取消；groupID string 为分组 ID；payload []Rule 为规则数组。
// 返回值：any 为业务 data；error 为校验、编码或远端错误。
func (service *Service) BatchCreateRules(ctx context.Context, groupID string, payload []Rule) (any, error) {
	collection, err := ruleCollectionPath(groupID)
	if err != nil {
		return nil, err
	}
	if err := validateBatch(payload, false); err != nil {
		return nil, err
	}
	if err := contracts.RequireVerified(OperationBatchCreateRule); err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationBatchCreateRule, http.MethodPost, collection+"/batch", nil, payload)
}

// ListRules 查询指定分组内的规则。
// 入参：ctx context.Context 控制取消；groupID string 为分组 ID；query Query 为分页排序条件。
// 返回值：any 为业务 data；error 为输入、校验或远端错误。
func (service *Service) ListRules(ctx context.Context, groupID string, query Query) (any, error) {
	collection, err := ruleCollectionPath(groupID)
	if err != nil {
		return nil, err
	}
	values, err := queryValues(query)
	if err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationListRules, http.MethodGet, collection, values, nil)
}

// UpdateRule 更新指定分组中的一条规则。
// 入参：ctx context.Context 控制取消；groupID/ruleID string 为分组和规则 ID；payload Rule 为完整更新字段。
// 返回值：any 为业务 data；error 为输入、校验、编码或远端错误。
func (service *Service) UpdateRule(ctx context.Context, groupID string, ruleID string, payload Rule) (any, error) {
	collection, err := ruleCollectionPath(groupID)
	if err != nil {
		return nil, err
	}
	resolvedRuleID, err := requireID(ruleID, "rule-id")
	if err != nil {
		return nil, err
	}
	if err := payload.Validate(false); err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationUpdateRule, http.MethodPut, collection+"/"+url.PathEscape(resolvedRuleID), nil, payload)
}

// BatchUpdateRules 在同一分组内以全有或全无语义批量更新规则。
// 入参：ctx context.Context 控制取消；groupID string 为分组 ID；payload []Rule 中每项必须带 ID。
// 返回值：any 为业务 data；error 为校验、编码或远端错误。
func (service *Service) BatchUpdateRules(ctx context.Context, groupID string, payload []Rule) (any, error) {
	collection, err := ruleCollectionPath(groupID)
	if err != nil {
		return nil, err
	}
	if err := validateBatch(payload, true); err != nil {
		return nil, err
	}
	if err := contracts.RequireVerified(OperationBatchUpdateRule); err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationBatchUpdateRule, http.MethodPut, collection+"/batch", nil, payload)
}

// DeleteRule 删除指定分组中的一条规则。
// 入参：ctx context.Context 控制取消；groupID/ruleID string 为分组和规则 ID。
// 返回值：any 为业务 data；error 为输入或远端错误。
func (service *Service) DeleteRule(ctx context.Context, groupID string, ruleID string) (any, error) {
	collection, err := ruleCollectionPath(groupID)
	if err != nil {
		return nil, err
	}
	resolvedRuleID, err := requireID(ruleID, "rule-id")
	if err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationDeleteRule, http.MethodDelete, collection+"/"+url.PathEscape(resolvedRuleID), nil, nil)
}

// BatchDeleteRules 在同一分组内以全有或全无语义批量删除规则。
// 入参：ctx context.Context 控制取消；groupID string 为分组 ID；ids []string 为规则 ID 数组。
// 返回值：any 为业务 data；error 为输入、编码或远端错误。
func (service *Service) BatchDeleteRules(ctx context.Context, groupID string, ids []string) (any, error) {
	collection, err := ruleCollectionPath(groupID)
	if err != nil {
		return nil, err
	}
	if err := validateIDs(ids); err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationBatchDeleteRule, http.MethodDelete, collection+"/batch", nil, ids)
}

// ruleCollectionPath 构造受限的分组规则相对路径。
// 入参：groupID string 为分组 ID。
// 返回值：string 为规则集合路径；error 在 ID 为空时非 nil。
func ruleCollectionPath(groupID string) (string, error) {
	resolved, err := requireID(groupID, "group-id")
	if err != nil {
		return "", err
	}
	return pathGroups + "/" + url.PathEscape(resolved) + "/rules", nil
}

// validateBatch 校验规则数组非空且每项满足创建或批量更新契约。
// 入参：payload []Rule 为规则数组；requireID bool 表示每项是否必须带 ID。
// 返回值：error，数组有效时为 nil。
func validateBatch(payload []Rule, requireID bool) error {
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

// validateIDs 校验规则 ID 数组不为空且不含空值。
// 入参：ids []string 为规则 ID 数组。
// 返回值：error，数组有效时为 nil。
func validateIDs(ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("rule-id 列表不能为空")
	}
	for index, id := range ids {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("第 %d 个 rule-id 不能为空", index+1)
		}
	}
	return nil
}

// queryValues 将规则分页排序模型编码为 URL 参数。
// 入参：query Query 为分页排序条件。
// 返回值：url.Values 为编码结果；error 为校验错误。
func queryValues(query Query) (url.Values, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	values := url.Values{}
	for _, item := range query.Sort {
		values.Add("sort", item)
	}
	if query.PageIndex > 0 {
		values.Set("pageIndex", strconv.Itoa(query.PageIndex))
	}
	if query.PageSize > 0 {
		values.Set("pageSize", strconv.Itoa(query.PageSize))
	}
	return values, nil
}

// doPayload 编码可选 JSON body，执行请求并保留 data 的对象、数组或标量形状。
// 入参：ctx context.Context；operationID/method/path string 定义契约；query url.Values 为查询；payload any 为可选 body。
// 返回值：any 为业务 data；error 为编码、HTTP Adapter 或解码失败。
func (service *Service) doPayload(ctx context.Context, operationID string, method string, path string, query url.Values, payload any) (any, error) {
	var body []byte
	header := http.Header{}
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("编码 %s 请求: %w", operationID, err)
		}
		body = encoded
		header.Set("Content-Type", "application/json")
	}
	response, err := service.client.Do(ctx, openplatform.Request{OperationID: operationID, Method: method, Path: path, Query: query, Header: header, Body: body, Timeout: service.timeout, SuccessCode: 200})
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(response.Data))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 %s 响应: %w", operationID, err)
	}
	return result, nil
}

// requireID 去除 ID 首尾空白并拒绝空值。
// 入参：value string 为 ID；field string 为错误消息字段名。
// 返回值：string 为规范化 ID；error 在为空时非 nil。
func requireID(value string, field string) (string, error) {
	resolved := strings.TrimSpace(value)
	if resolved == "" {
		return "", fmt.Errorf("%s 不能为空", field)
	}
	return resolved, nil
}
