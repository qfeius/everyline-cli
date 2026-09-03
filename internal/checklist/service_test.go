package checklist

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/contracts"
	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
)

// recordingClient 记录清单服务生成的最后一个 HTTP Adapter 请求。
type recordingClient struct {
	request openplatform.Request
	data    json.RawMessage
}

// Do 保存请求并返回固定的 JSON data。
// 入参：context.Context 仅满足接口；request openplatform.Request 为待记录契约。
// 返回值：openplatform.Response 为固定对象；error 在请求偏离契约目录时非 nil。
func (client *recordingClient) Do(_ context.Context, request openplatform.Request) (openplatform.Response, error) {
	client.request = request
	if err := contracts.ValidateRequest(request.OperationID, request.Method, request.Path, request.ContractInput); err != nil {
		return openplatform.Response{}, err
	}
	// data 用于精确模拟各接口的服务端响应；未指定时维持原有固定成功对象。
	responseData := client.data
	if responseData == nil {
		responseData = json.RawMessage(`{"ok":true}`)
	}
	return openplatform.Response{Data: responseData}, nil
}

// TestChecklistCreateCompletesNullReviewRuleIDs 验证创建成功响应缺少规则关联时，CLI 使用已确认写入的请求补齐展示结果。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；响应中的 reviewRuleIds 仍为 nil 或发生类型漂移时通过 t.Fatal 报告。
func TestChecklistCreateCompletesNullReviewRuleIDs(t *testing.T) {
	payload := Checklist{Name: "采购清单", ReviewRuleIDs: []string{"rule-1", "rule-2"}}
	client := &recordingClient{data: json.RawMessage(`{"id":"check-1","name":"采购清单","reviewRuleIds":null}`)}

	result, err := NewService(client, time.Second).Create(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	created, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result=%#v，期望创建响应对象", result)
	}
	ruleIDs, ok := created["reviewRuleIds"].([]string)
	if !ok || len(ruleIDs) != 2 || ruleIDs[0] != "rule-1" || ruleIDs[1] != "rule-2" {
		t.Fatalf("reviewRuleIds=%#v，期望使用创建请求补齐规则 ID", created["reviewRuleIds"])
	}
}

// TestServiceOperationMappings 验证七个清单接口的 operation、method 和 path，包含批量创建与批量更新。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestServiceOperationMappings(t *testing.T) {
	enabled := true
	payload := Checklist{Name: "采购清单", ContractCategory: []string{"PURCHASE"}, ReviewStage: []int32{1}, Enabled: &enabled, ReviewRuleIDs: []string{"rule-1"}}
	updatePayload := payload
	updatePayload.ID = "check-1"
	tests := []struct {
		name        string
		operationID string
		method      string
		path        string
		invoke      func(*Service) error
	}{
		{"create", OperationCreate, http.MethodPost, pathCollection, func(service *Service) error { _, err := service.Create(context.Background(), payload); return err }},
		{"batch-create", OperationBatchCreate, http.MethodPost, pathBatch, func(service *Service) error {
			_, err := service.BatchCreate(context.Background(), []Checklist{payload})
			return err
		}},
		{"list", OperationList, http.MethodGet, pathCollection, func(service *Service) error {
			_, err := service.List(context.Background(), Query{PageIndex: 1, PageSize: 20})
			return err
		}},
		{"update", OperationUpdate, http.MethodPut, pathCollection + "/check-1", func(service *Service) error {
			_, err := service.Update(context.Background(), "check-1", payload)
			return err
		}},
		{"batch-update", OperationBatchUpdate, http.MethodPut, pathBatch, func(service *Service) error {
			_, err := service.BatchUpdate(context.Background(), []Checklist{updatePayload})
			return err
		}},
		{"delete", OperationDelete, http.MethodDelete, pathCollection + "/check-1", func(service *Service) error { _, err := service.Delete(context.Background(), "check-1"); return err }},
		{"batch-delete", OperationBatchDelete, http.MethodDelete, pathBatch, func(service *Service) error {
			_, err := service.BatchDelete(context.Background(), []string{"check-1"})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingClient{}
			if err := test.invoke(NewService(client, time.Second)); err != nil {
				t.Fatal(err)
			}
			if client.request.OperationID != test.operationID || client.request.Method != test.method || client.request.Path != test.path {
				t.Fatalf("request=%#v", client.request)
			}
		})
	}
}

// TestChecklistBatchWritesUseChecklistArrays 验证清单批量创建和批量更新直接发送清单数组，更新项保留 id。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestChecklistBatchWritesUseChecklistArrays(t *testing.T) {
	payload := Checklist{Name: "采购清单", ReviewRuleIDs: []string{"rule-1"}}
	updatePayload := payload
	updatePayload.ID = "check-1"
	tests := []struct {
		name       string
		invoke     func(*Service) error
		expectedID string
	}{
		{"batch-create", func(service *Service) error {
			_, err := service.BatchCreate(context.Background(), []Checklist{payload})
			return err
		}, ""},
		{"batch-update", func(service *Service) error {
			_, err := service.BatchUpdate(context.Background(), []Checklist{updatePayload})
			return err
		}, "check-1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingClient{}
			if err := test.invoke(NewService(client, time.Second)); err != nil {
				t.Fatal(err)
			}
			var body []Checklist
			if err := json.Unmarshal(client.request.Body, &body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 1 || body[0].Name != "采购清单" || body[0].ID != test.expectedID {
				t.Fatalf("body=%#v", body)
			}
		})
	}
}

// TestListQueryContract 验证数组过滤使用重复 query key，布尔值和分页保留类型语义。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestListQueryContract(t *testing.T) {
	enabled := false
	client := &recordingClient{}
	service := NewService(client, time.Second)
	_, err := service.List(context.Background(), Query{
		ReviewStages: []string{"DRAFT", "SIGN"}, ContractCategories: []string{"PURCHASE"}, Enabled: &enabled, PageIndex: 2, PageSize: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.request.Query["reviewStages"]) != 2 || client.request.Query.Get("enabled") != "false" || client.request.Query.Get("pageIndex") != "2" {
		t.Fatalf("query=%s", client.request.Query.Encode())
	}
}

// TestSingleUpdateIDContract 验证单项更新拒绝路径/body ID 冲突，并在一致时只发送路径 ID。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestSingleUpdateIDContract(t *testing.T) {
	payload := Checklist{ID: "other", Name: "清单", ReviewRuleIDs: []string{"rule-1"}}
	client := &recordingClient{}
	service := NewService(client, time.Second)
	if _, err := service.Update(context.Background(), "check-1", payload); err == nil {
		t.Fatal("路径 ID 与 body ID 冲突时应拒绝")
	}
	if client.request.OperationID != "" {
		t.Fatalf("冲突请求不得调用 client: %#v", client.request)
	}
	payload.ID = "check-1"
	if _, err := service.Update(context.Background(), "check-1", payload); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(client.request.Body, &body); err != nil {
		t.Fatal(err)
	}
	if _, exists := body["id"]; exists {
		t.Fatalf("单项更新 body 不应包含 id: %#v", body)
	}
}

// TestBatchDeleteRejectsDuplicateIDs 验证服务边界按 resource-ids Schema 拒绝规范化后的重复 ID。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestBatchDeleteRejectsDuplicateIDs(t *testing.T) {
	client := &recordingClient{}
	_, err := NewService(client, time.Second).BatchDelete(context.Background(), []string{"check-1", " check-1 "})
	if err == nil {
		t.Fatal("重复 ID 应被拒绝")
	}
	if client.request.OperationID != "" {
		t.Fatalf("重复 ID 不得调用 client: %#v", client.request)
	}
}

// TestBatchDeleteUsesIDsObject 验证批量删除请求体按平台要求包装为 ids 对象。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestBatchDeleteUsesIDsObject(t *testing.T) {
	client := &recordingClient{}
	if _, err := NewService(client, time.Second).BatchDelete(context.Background(), []string{"check-1", "check-2"}); err != nil {
		t.Fatal(err)
	}
	var body map[string][]string
	if err := json.Unmarshal(client.request.Body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body["ids"]) != 2 || body["ids"][0] != "check-1" || body["ids"][1] != "check-2" {
		t.Fatalf("body=%#v", body)
	}
}
