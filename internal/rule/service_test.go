package rule

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/contracts"
	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
)

// recordingClient 记录规则服务生成的最后一个 HTTP Adapter 请求。
type recordingClient struct {
	request openplatform.Request
}

// Do 保存请求并返回固定的 JSON data。
// 入参：context.Context 仅满足接口；request openplatform.Request 为待记录契约。
// 返回值：openplatform.Response 为固定对象；error 在请求偏离契约目录时非 nil。
func (client *recordingClient) Do(_ context.Context, request openplatform.Request) (openplatform.Response, error) {
	client.request = request
	if err := contracts.ValidateRequest(request.OperationID, request.Method, request.Path, request.ContractInput); err != nil {
		return openplatform.Response{}, err
	}
	return openplatform.Response{Data: json.RawMessage(`{"ok":true}`)}, nil
}

// TestGroupOperationMappings 验证四个规则分组接口的 operation、method 和 path。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestGroupOperationMappings(t *testing.T) {
	payload := Group{Name: "付款规则"}
	tests := []struct {
		name        string
		operationID string
		method      string
		path        string
		invoke      func(*Service) error
	}{
		{"create", OperationCreateGroup, http.MethodPost, pathGroups, func(service *Service) error { _, err := service.CreateGroup(context.Background(), payload); return err }},
		{"list", OperationListGroups, http.MethodGet, pathGroups, func(service *Service) error {
			_, err := service.ListGroups(context.Background(), Query{PageIndex: 1})
			return err
		}},
		{"update", OperationUpdateGroup, http.MethodPut, pathGroups + "/group-1", func(service *Service) error {
			_, err := service.UpdateGroup(context.Background(), "group-1", payload)
			return err
		}},
		{"delete", OperationDeleteGroup, http.MethodDelete, pathGroups + "/group-1", func(service *Service) error {
			_, err := service.DeleteGroup(context.Background(), "group-1")
			return err
		}},
	}
	assertMappings(t, tests)
}

// TestRuleOperationMappings 验证七个分组内规则接口的 operation、method 和 path，包含批量创建与批量更新。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestRuleOperationMappings(t *testing.T) {
	riskLevel := int32(2)
	payload := Rule{Name: "付款期限", RiskLevel: &riskLevel, Content: "付款期限不得超过 60 天"}
	updatePayload := payload
	updatePayload.ID = "rule-1"
	collection := pathGroups + "/group-1/rules"
	tests := []struct {
		name        string
		operationID string
		method      string
		path        string
		invoke      func(*Service) error
	}{
		{"create", OperationCreateRule, http.MethodPost, collection, func(service *Service) error {
			_, err := service.CreateRule(context.Background(), "group-1", payload)
			return err
		}},
		{"batch-create", OperationBatchCreateRule, http.MethodPost, collection + "/batch", func(service *Service) error {
			_, err := service.BatchCreateRules(context.Background(), "group-1", []Rule{payload})
			return err
		}},
		{"list", OperationListRules, http.MethodGet, collection, func(service *Service) error {
			_, err := service.ListRules(context.Background(), "group-1", Query{PageSize: 20})
			return err
		}},
		{"update", OperationUpdateRule, http.MethodPut, collection + "/rule-1", func(service *Service) error {
			_, err := service.UpdateRule(context.Background(), "group-1", "rule-1", payload)
			return err
		}},
		{"batch-update", OperationBatchUpdateRule, http.MethodPut, collection + "/batch", func(service *Service) error {
			_, err := service.BatchUpdateRules(context.Background(), "group-1", []Rule{updatePayload})
			return err
		}},
		{"delete", OperationDeleteRule, http.MethodDelete, collection + "/rule-1", func(service *Service) error {
			_, err := service.DeleteRule(context.Background(), "group-1", "rule-1")
			return err
		}},
		{"batch-delete", OperationBatchDeleteRule, http.MethodDelete, collection + "/batch", func(service *Service) error {
			_, err := service.BatchDeleteRules(context.Background(), "group-1", []string{"rule-1"})
			return err
		}},
	}
	assertMappings(t, tests)
}

// TestBatchUpdateRulesUsesRuleArray 验证规则批量更新直接发送包含 id 的规则数组，不额外包装 data 字段。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestBatchUpdateRulesUsesRuleArray(t *testing.T) {
	riskLevel := int32(1)
	client := &recordingClient{}
	payload := []Rule{{ID: "rule-1", Name: "规则", RiskLevel: &riskLevel, Content: "内容"}}
	if _, err := NewService(client, time.Second).BatchUpdateRules(context.Background(), "group-1", payload); err != nil {
		t.Fatal(err)
	}
	var body []Rule
	if err := json.Unmarshal(client.request.Body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body[0].ID != "rule-1" || body[0].Name != "规则" {
		t.Fatalf("body=%#v", body)
	}
}

// TestBatchCreateRulesUsesRuleArray 验证规则批量创建直接发送规则数组，不额外包装 data 字段。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestBatchCreateRulesUsesRuleArray(t *testing.T) {
	riskLevel := int32(2)
	client := &recordingClient{}
	payload := []Rule{{Name: "付款期限", RiskLevel: &riskLevel, Content: "付款期限不得超过 60 天"}}
	if _, err := NewService(client, time.Second).BatchCreateRules(context.Background(), "group-1", payload); err != nil {
		t.Fatal(err)
	}
	var body []Rule
	if err := json.Unmarshal(client.request.Body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body[0].Name != payload[0].Name {
		t.Fatalf("body=%#v", body)
	}
}

// TestSingleUpdateIDContracts 验证分组和规则单项更新均拒绝冲突 ID，并移除一致的 body ID。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestSingleUpdateIDContracts(t *testing.T) {
	riskLevel := int32(1)
	tests := []struct {
		name   string
		invoke func(*Service, string) error
	}{
		{"group", func(service *Service, bodyID string) error {
			_, err := service.UpdateGroup(context.Background(), "group-1", Group{ID: bodyID, Name: "分组"})
			return err
		}},
		{"rule", func(service *Service, bodyID string) error {
			_, err := service.UpdateRule(context.Background(), "group-1", "rule-1", Rule{ID: bodyID, Name: "规则", RiskLevel: &riskLevel, Content: "内容"})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingClient{}
			service := NewService(client, time.Second)
			if err := test.invoke(service, "other"); err == nil {
				t.Fatal("路径 ID 与 body ID 冲突时应拒绝")
			}
			expectedID := "group-1"
			if test.name == "rule" {
				expectedID = "rule-1"
			}
			if err := test.invoke(service, expectedID); err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			if err := json.Unmarshal(client.request.Body, &body); err != nil {
				t.Fatal(err)
			}
			if _, exists := body["id"]; exists {
				t.Fatalf("单项更新 body 不应包含 id: %#v", body)
			}
		})
	}
}

// TestBatchDeleteRulesRejectsDuplicateIDs 验证规则批量删除拒绝规范化后的重复 ID。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestBatchDeleteRulesRejectsDuplicateIDs(t *testing.T) {
	client := &recordingClient{}
	_, err := NewService(client, time.Second).BatchDeleteRules(context.Background(), "group-1", []string{"rule-1", " rule-1 "})
	if err == nil {
		t.Fatal("重复 ID 应被拒绝")
	}
	if client.request.OperationID != "" {
		t.Fatalf("重复 ID 不得调用 client: %#v", client.request)
	}
}

// TestBatchDeleteRulesUsesIDsObject 验证批量删除规则请求体按平台要求包装为 ids 对象。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestBatchDeleteRulesUsesIDsObject(t *testing.T) {
	client := &recordingClient{}
	if _, err := NewService(client, time.Second).BatchDeleteRules(context.Background(), "group-1", []string{"rule-1", "rule-2"}); err != nil {
		t.Fatal(err)
	}
	var body map[string][]string
	if err := json.Unmarshal(client.request.Body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body["ids"]) != 2 || body["ids"][0] != "rule-1" || body["ids"][1] != "rule-2" {
		t.Fatalf("body=%#v", body)
	}
}

// assertMappings 执行映射测试表并校验请求三元组。
// 入参：t *testing.T 为测试上下文；tests 为 operation/method/path 期望及调用函数。
// 返回值：无；失败通过 t.Fatal 报告。
func assertMappings(t *testing.T, tests []struct {
	name        string
	operationID string
	method      string
	path        string
	invoke      func(*Service) error
}) {
	t.Helper()
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
