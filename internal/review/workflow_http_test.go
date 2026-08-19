package review

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/auth"
	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
)

// integrationTokenProvider 为真实 HTTP 链路测试提供固定访问令牌，绕开 token endpoint 以聚焦业务请求。
type integrationTokenProvider struct{}

// Token 返回测试令牌。
// 入参：context.Context 和 config.Profile 仅满足 TokenProvider 接口。
// 返回值：auth.Token 为固定 Bearer 凭证；error 始终为 nil。
func (integrationTokenProvider) Token(context.Context, config.Profile) (auth.Token, error) {
	return auth.Token{AccessToken: "integration-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// writeIntegrationEnvelope 写入 openplatform.Client 可解封装的成功响应。
// 入参：writer http.ResponseWriter 为 HTTP 响应；data string 为 JSON data 对象。
// 返回值：无。
func writeIntegrationEnvelope(writer http.ResponseWriter, data string) {
	writer.Header().Set("Content-Type", "application/json")
	_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":` + data + `}`))
}

// TestWorkflowRunHTTPIntegration 验证真实 HTTP Adapter、Schema、Service 和 Workflow 的完整文件审查链路。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestWorkflowRunHTTPIntegration(t *testing.T) {
	const fileContent = "contract-content"
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var calls []string
	statusCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer integration-token" {
			t.Errorf("authorization=%q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case pathUploadFile:
			if request.Method != http.MethodPost || !strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/form-data") {
				t.Errorf("upload method/content-type=%s/%q", request.Method, request.Header.Get("Content-Type"))
			}
			if err := request.ParseMultipartForm(4 << 20); err != nil {
				t.Errorf("parse upload form: %v", err)
			} else {
				file, _, err := request.FormFile("file")
				if err != nil {
					t.Errorf("file part: %v", err)
				} else {
					content, readErr := io.ReadAll(file)
					_ = file.Close()
					if readErr != nil || string(content) != fileContent {
						t.Errorf("file content=%q err=%v", content, readErr)
					}
				}
				if request.FormValue("name") != "合同.pdf" || request.FormValue("appType") != AppTypeCLM || request.FormValue("businessId") != "biz-1" {
					t.Errorf("upload fields name=%q appType=%q businessId=%q", request.FormValue("name"), request.FormValue("appType"), request.FormValue("businessId"))
				}
			}
			calls = append(calls, "upload")
			writeIntegrationEnvelope(writer, `{"fileId":11,"businessId":"biz-1","fileHash":"`+strings.ToUpper(hash)+`"}`)
		case pathExtractSubjects:
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("subjects body: %v", err)
			}
			if body["businessId"] != "biz-1" || body["appType"] != AppTypeCLM || body["fileId"] != "11" {
				t.Errorf("subjects body=%#v", body)
			}
			calls = append(calls, "subjects")
			writeIntegrationEnvelope(writer, `{"counterparts":["甲方"]}`)
		case pathStartReview:
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("start body: %v", err)
			}
			if body["businessId"] != "biz-1" || body["appType"] != AppTypeCLM || body["fileId"] != float64(11) || body["fileHash"] != hash {
				t.Errorf("start body=%#v", body)
			}
			calls = append(calls, "start")
			writeIntegrationEnvelope(writer, `{"taskId":88,"status":"running"}`)
		case pathTaskStatus:
			assertIntegrationTaskQuery(t, request)
			statusCalls++
			calls = append(calls, "status")
			if statusCalls == 1 {
				writeIntegrationEnvelope(writer, `{"taskId":88,"status":"running"}`)
			} else {
				writeIntegrationEnvelope(writer, `{"taskId":88,"status":"success"}`)
			}
		case pathTaskInfo:
			assertIntegrationTaskQuery(t, request)
			calls = append(calls, "info")
			writeIntegrationEnvelope(writer, `{"taskId":88,"status":"success","result":[]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	filePath := filepath.Join(t.TempDir(), "合同.pdf")
	if err := os.WriteFile(filePath, []byte(fileContent), 0o600); err != nil {
		t.Fatal(err)
	}
	client := openplatform.NewClient(config.Profile{BaseURL: server.URL}, integrationTokenProvider{}, server.Client())
	service := NewService(client, time.Second)
	workflow := NewWorkflow(service, instantClock{}, WorkflowOptions{Interval: time.Millisecond, Deadline: time.Second})
	result, err := workflow.Run(context.Background(), RunSpec{
		Source:          RunSource{Type: "file", Path: filePath, Name: "合同.pdf"},
		BusinessID:      "biz-1",
		AppType:         AppTypeCLM,
		ExtractSubjects: true,
		Wait:            true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := StringValue(result.Final, "status"); status != "success" {
		t.Fatalf("final=%#v", result.Final)
	}
	expected := []string{"upload", "subjects", "start", "status", "status", "info"}
	if strings.Join(calls, ",") != strings.Join(expected, ",") {
		t.Fatalf("calls=%#v，期望完整 HTTP 链路=%#v", calls, expected)
	}
}

// assertIntegrationTaskQuery 校验 status/info 共享的业务可见性查询参数。
// 入参：t *testing.T 为测试上下文；request *http.Request 为待检查的 HTTP 请求。
// 返回值：无；失败通过 t.Errorf 报告。
func assertIntegrationTaskQuery(t *testing.T, request *http.Request) {
	query := request.URL.Query()
	if query.Get("taskId") != "88" || query.Get("businessId") != "biz-1" || query.Get("appType") != AppTypeCLM || query.Get("visibilityScope") != VisibilityScopeContractResult {
		t.Errorf("task query=%s", request.URL.RawQuery)
	}
}

// TestServiceStartFeishuHTTPPath 验证网关外部路径经 rewrite 后可到达后端 /open-api/feishu/v1 路由。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestServiceStartFeishuHTTPPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != pathStartFeishu {
			t.Errorf("method/path=%s %s", request.Method, request.URL.Path)
		}
		writeIntegrationEnvelope(writer, `{"smartAuditId":89,"taskStatus":0,"taskStatusName":"RUNNING"}`)
	}))
	defer server.Close()
	client := openplatform.NewClient(config.Profile{BaseURL: server.URL}, integrationTokenProvider{}, server.Client())
	service := NewService(client, time.Second)
	reviewStrength := 0
	if _, err := service.StartFeishu(context.Background(), FeishuStartRequest{
		FileID: 11, ReviewStrength: &reviewStrength, SelectedPosition: "legal", HasQuota: true,
		BaseSignature: "payload.signature", PackID: "pack-1",
	}); err != nil {
		t.Fatal(err)
	}
}

// TestServiceFeishuInfoHTTPPath 验证字段捷径生命周期查询使用标准 v1 smartAudit/info 路由和 query 别名。
func TestServiceFeishuInfoHTTPPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != pathFeishuTaskInfo {
			t.Errorf("method/path=%s %s", request.Method, request.URL.Path)
		}
		if request.URL.Query().Get("smartAuditId") != "89" || request.URL.Query().Get("reviewPosition") != "legal" {
			t.Errorf("query=%s", request.URL.RawQuery)
		}
		writeIntegrationEnvelope(writer, `{"smartAuditId":89,"taskStatus":1,"taskStatusName":"COMPLETE","result":[]}`)
	}))
	defer server.Close()
	client := openplatform.NewClient(config.Profile{BaseURL: server.URL}, integrationTokenProvider{}, server.Client())
	service := NewService(client, time.Second)
	result, err := service.FeishuInfo(context.Background(), FeishuTaskQuery{SmartAuditID: 89, ReviewPosition: "legal"})
	if err != nil {
		t.Fatal(err)
	}
	if status := FeishuTaskStatus(result); status != "success" {
		t.Fatalf("result=%#v status=%q", result, status)
	}
}
