package rule

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
)

// recordingClient 记录规则服务生成的最后一个 HTTP Adapter 请求。
type recordingClient struct {
	request openplatform.Request
}

// Do 保存请求并返回固定的 JSON data。
// 入参：context.Context 仅满足接口；request openplatform.Request 为待记录契约。
// 返回值：openplatform.Response 为固定对象；error 始终为 nil。
func (client *recordingClient) Do(_ context.Context, request openplatform.Request) (openplatform.Response, error) {
	client.request = request
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

// TestRuleOperationMappings 验证七个分组内规则接口的 operation、method 和 path。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestRuleOperationMappings(t *testing.T) {
	payload := Rule{Name: "付款期限", RiskLevel: 2, Content: "付款期限不得超过 60 天"}
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
