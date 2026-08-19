package contracts_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.qtech.cn/ai/everyline-cli/internal/cli"
	"git.qtech.cn/ai/everyline-cli/internal/contracts"
)

// TestCatalogCompleteness 验证当前 operation 的 ID、CLI、method/path、Schema 和文档同步存在。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestCatalogCompleteness(t *testing.T) {
	projectRoot := filepath.Clean(filepath.Join("..", ".."))
	mappingContent, err := os.ReadFile(filepath.Join(projectRoot, "docs", "api-mapping.md"))
	if err != nil {
		t.Fatal(err)
	}
	runtime := cli.NewRuntime(t.TempDir(), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	root := cli.NewRootCommand(runtime)
	seen := map[string]struct{}{}
	specs := contracts.All()
	if len(specs) != 27 {
		t.Fatalf("operation 数量=%d，期望 27", len(specs))
	}
	for _, spec := range specs {
		if _, exists := seen[spec.OperationID]; exists {
			t.Errorf("重复 operation ID: %s", spec.OperationID)
		}
		seen[spec.OperationID] = struct{}{}
		if spec.Method != "GET" && spec.Method != "POST" && spec.Method != "PUT" && spec.Method != "DELETE" {
			t.Errorf("%s method=%s", spec.OperationID, spec.Method)
		}
		if spec.Path != "profile.token_url" && !strings.HasPrefix(spec.Path, "/open-apis/") {
			t.Errorf("%s path=%s", spec.OperationID, spec.Path)
		}
		if _, _, findErr := root.Find(strings.Fields(spec.Command)); findErr != nil {
			t.Errorf("%s 缺少 CLI 命令 %q: %v", spec.OperationID, spec.Command, findErr)
		}
		assertSchema(t, projectRoot, spec)
		row := "| `" + spec.OperationID + "` | `" + spec.Command + "` | `" + spec.Method + " " + spec.Path + "` |"
		if !strings.Contains(string(mappingContent), row) {
			t.Errorf("docs/api-mapping.md 缺少精确映射行: %s", row)
		}
	}
}

// assertSchema 验证 operation 显式绑定的 Schema 存在且声明 Draft 2020-12 与唯一 $id。
// 入参：t *testing.T 为测试上下文；projectRoot string 为仓库根；spec contracts.Spec 为操作契约。
// 返回值：无；失败通过 t.Error 报告。
func assertSchema(t *testing.T, projectRoot string, spec contracts.Spec) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(projectRoot, "schemas", spec.Schema))
	if err != nil {
		t.Errorf("%s 读取 schema %s: %v", spec.OperationID, spec.Schema, err)
		return
	}
	var schema map[string]any
	if err := json.Unmarshal(content, &schema); err != nil {
		t.Errorf("%s 解析 schema %s: %v", spec.OperationID, spec.Schema, err)
		return
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" || schema["$id"] == "" {
		t.Errorf("%s schema 元数据不完整: %s", spec.OperationID, spec.Schema)
	}
}
