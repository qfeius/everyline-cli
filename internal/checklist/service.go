package checklist

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
	OperationCreate      = "createReviewChecklist"
	OperationBatchCreate = "batchCreateReviewChecklists"
	OperationList        = "listReviewChecklists"
	OperationUpdate      = "updateReviewChecklist"
	OperationBatchUpdate = "batchUpdateReviewChecklists"
	OperationDelete      = "deleteReviewChecklist"
	OperationBatchDelete = "batchDeleteReviewChecklists"

	pathCollection = "/open-apis/review-rules/review-checklists"
	pathBatch      = "/open-apis/review-rules/review-checklists/batch"
)

// Client 是 checklist service 依赖的受控 HTTP Adapter 边界。
type Client interface {
	Do(context.Context, openplatform.Request) (openplatform.Response, error)
}

// Service 将七个审查清单操作映射为 method/path/body 契约。
type Service struct {
	client  Client
	timeout time.Duration
}

// NewService 创建审查清单领域服务。
// 入参：client Client 为 HTTP Adapter；timeout time.Duration 为远端请求超时。
// 返回值：*Service，可执行清单单条和批量 CRUD。
func NewService(client Client, timeout time.Duration) *Service {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Service{client: client, timeout: timeout}
}

// Create 创建一条审查清单。
// 入参：ctx context.Context 控制取消；payload Checklist 为清单字段。
// 返回值：any 为业务 data；error 为校验、编码或远端错误。
func (service *Service) Create(ctx context.Context, payload Checklist) (any, error) {
	if err := payload.Validate(false); err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationCreate, http.MethodPost, pathCollection, nil, payload)
}

// BatchCreate 以全有或全无语义批量创建审查清单。
// 入参：ctx context.Context 控制取消；payload []Checklist 为非空清单数组。
// 返回值：any 为业务 data；error 为校验、编码或远端错误。
func (service *Service) BatchCreate(ctx context.Context, payload []Checklist) (any, error) {
	if err := validateBatch(payload, false); err != nil {
		return nil, err
	}
	if err := contracts.RequireVerified(OperationBatchCreate); err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationBatchCreate, http.MethodPost, pathBatch, nil, payload)
}

// List 查询审查清单并透传已声明的过滤和分页参数。
// 入参：ctx context.Context 控制取消；query Query 为过滤条件。
// 返回值：any 为业务 data；error 为校验或远端错误。
func (service *Service) List(ctx context.Context, query Query) (any, error) {
	values, err := queryValues(query)
	if err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationList, http.MethodGet, pathCollection, values, nil)
}

// Update 更新路径 ID 指定的一条审查清单。
// 入参：ctx context.Context 控制取消；id string 为清单 ID；payload Checklist 为完整更新字段。
// 返回值：any 为业务 data；error 为校验、编码或远端错误。
func (service *Service) Update(ctx context.Context, id string, payload Checklist) (any, error) {
	resolvedID, err := requireID(id, "id")
	if err != nil {
		return nil, err
	}
	if err := payload.Validate(false); err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationUpdate, http.MethodPut, pathCollection+"/"+url.PathEscape(resolvedID), nil, payload)
}

// BatchUpdate 以全有或全无语义批量更新审查清单，数组中每项必须带 ID。
// 入参：ctx context.Context 控制取消；payload []Checklist 为非空更新数组。
// 返回值：any 为业务 data；error 为校验、编码或远端错误。
func (service *Service) BatchUpdate(ctx context.Context, payload []Checklist) (any, error) {
	if err := validateBatch(payload, true); err != nil {
		return nil, err
	}
	if err := contracts.RequireVerified(OperationBatchUpdate); err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationBatchUpdate, http.MethodPut, pathBatch, nil, payload)
}

// Delete 删除路径 ID 指定的一条审查清单。
// 入参：ctx context.Context 控制取消；id string 为清单 ID。
// 返回值：any 为业务 data；error 为输入或远端错误。
func (service *Service) Delete(ctx context.Context, id string) (any, error) {
	resolvedID, err := requireID(id, "id")
	if err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationDelete, http.MethodDelete, pathCollection+"/"+url.PathEscape(resolvedID), nil, nil)
}

// BatchDelete 以全有或全无语义批量删除审查清单。
// 入参：ctx context.Context 控制取消；ids []string 为非空清单 ID 数组。
// 返回值：any 为业务 data；error 为输入、编码或远端错误。
func (service *Service) BatchDelete(ctx context.Context, ids []string) (any, error) {
	if err := validateIDs(ids); err != nil {
		return nil, err
	}
	return service.doPayload(ctx, OperationBatchDelete, http.MethodDelete, pathBatch, nil, ids)
}

// validateBatch 校验批量数组非空且每项满足相应创建或更新契约。
// 入参：payload []Checklist 为数组；requireID bool 表示每项是否必须带 ID。
// 返回值：error，数组有效时为 nil。
func validateBatch(payload []Checklist, requireID bool) error {
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

// validateIDs 校验批量删除 ID 数组不为空且不含空值。
// 入参：ids []string 为待删除 ID。
// 返回值：error，数组有效时为 nil。
func validateIDs(ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("id 列表不能为空")
	}
	for index, id := range ids {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("第 %d 个 id 不能为空", index+1)
		}
	}
	return nil
}

// queryValues 将清单查询模型编码为重复 key 形式的 URL 参数。
// 入参：query Query 为过滤与分页条件。
// 返回值：url.Values 为编码结果；error 为校验错误。
func queryValues(query Query) (url.Values, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	values := url.Values{}
	setString(values, "name", query.Name)
	setStrings(values, "reviewStages", query.ReviewStages)
	setStrings(values, "contractCategories", query.ContractCategories)
	setInt64(values, "startTime", query.StartTime)
	setInt64(values, "endTime", query.EndTime)
	if query.Enabled != nil {
		values.Set("enabled", strconv.FormatBool(*query.Enabled))
	}
	setStrings(values, "createEmployeeIds", query.CreateEmployeeIDs)
	setStrings(values, "updateEmployeeIds", query.UpdateEmployeeIDs)
	setStrings(values, "sort", query.Sort)
	setInt(values, "pageIndex", query.PageIndex)
	setInt(values, "pageSize", query.PageSize)
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

// setString 在非空时设置单值查询参数。
// 入参：values url.Values；key/value string 为参数名和值。
// 返回值：无。
func setString(values url.Values, key string, value string) {
	if value != "" {
		values.Set(key, value)
	}
}

// setStrings 以重复 key 形式写入字符串数组查询参数。
// 入参：values url.Values；key string 为参数名；items []string 为值。
// 返回值：无。
func setStrings(values url.Values, key string, items []string) {
	for _, item := range items {
		values.Add(key, item)
	}
}

// setInt64 在正数时设置 int64 查询参数。
// 入参：values url.Values；key string 为参数名；value int64 为值。
// 返回值：无。
func setInt64(values url.Values, key string, value int64) {
	if value > 0 {
		values.Set(key, strconv.FormatInt(value, 10))
	}
}

// setInt 在正数时设置 int 查询参数。
// 入参：values url.Values；key string 为参数名；value int 为值。
// 返回值：无。
func setInt(values url.Values, key string, value int) {
	if value > 0 {
		values.Set(key, strconv.Itoa(value))
	}
}
