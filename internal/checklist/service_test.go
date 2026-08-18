package checklist

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/contracts"
	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
)

// recordingClient 记录清单服务生成的最后一个 HTTP Adapter 请求。
type recordingClient struct {
	request openplatform.Request
}

// Do 保存请求并返回固定的 JSON data。
// 入参：context.Context 仅满足接口；request openplatform.Request 为待记录契约。
// 返回值：openplatform.Response 为固定对象；error 在请求偏离契约目录时非 nil。
func (client *recordingClient) Do(_ context.Context, request openplatform.Request) (openplatform.Response, error) {
	client.request = request
	if err := contracts.ValidateRequest(request.OperationID, request.Method, request.Path); err != nil {
		return openplatform.Response{}, err
	}
	return openplatform.Response{Data: json.RawMessage(`{"ok":true}`)}, nil
}

// TestServiceOperationMappings 验证七个清单接口的 operation、method 和 path。
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
		{"list", OperationList, http.MethodGet, pathCollection, func(service *Service) error {
			_, err := service.List(context.Background(), Query{PageIndex: 1, PageSize: 20})
			return err
		}},
		{"update", OperationUpdate, http.MethodPut, pathCollection + "/check-1", func(service *Service) error {
			_, err := service.Update(context.Background(), "check-1", payload)
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

// TestUnverifiedChecklistBatchWritesFailClosed 验证缺少详情页请求体定义时不会调用批量写接口。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestUnverifiedChecklistBatchWritesFailClosed(t *testing.T) {
	payload := Checklist{Name: "采购清单", ReviewRuleIDs: []string{"rule-1"}}
	updatePayload := payload
	updatePayload.ID = "check-1"
	client := &recordingClient{}
	service := NewService(client, time.Second)
	for _, invoke := range []func() error{
		func() error { _, err := service.BatchCreate(context.Background(), []Checklist{payload}); return err },
		func() error {
			_, err := service.BatchUpdate(context.Background(), []Checklist{updatePayload})
			return err
		},
	} {
		if err := invoke(); !errors.Is(err, contracts.ErrContractUnverified) {
			t.Fatalf("err=%v，期望 ErrContractUnverified", err)
		}
		if client.request.OperationID != "" {
			t.Fatalf("未核验接口不应调用 HTTP client: %#v", client.request)
		}
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
