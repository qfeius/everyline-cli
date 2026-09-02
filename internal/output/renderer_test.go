package output

import (
	"bytes"
	"strings"
	"testing"
)

// TestRendererFormats 验证 JSON/YAML/Table/Raw 都保留业务字段且以换行结束。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；失败通过 t.Fatal 报告。
func TestRendererFormats(t *testing.T) {
	tests := []struct {
		format   Format
		contains string
	}{
		{format: FormatJSON, contains: `"taskId": 88`},
		{format: FormatYAML, contains: "taskId: 88"},
		{format: FormatTable, contains: "taskId"},
		{format: FormatRaw, contains: `"taskId":88`},
	}
	for _, test := range tests {
		t.Run(string(test.format), func(t *testing.T) {
			buffer := &bytes.Buffer{}
			if err := (DefaultRenderer{}).Render(buffer, test.format, map[string]any{"taskId": 88, "status": "success"}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(buffer.String(), test.contains) || !strings.HasSuffix(buffer.String(), "\n") {
				t.Fatalf("output=%q", buffer.String())
			}
		})
	}
}

// TestRendererPreservesSignedURLCharacters 验证 JSON/raw 不把签名链接中的 & 转义为 Unicode 序列。
// 入参：t *testing.T 为测试上下文。
// 返回值：无；链接未逐字出现或 token 丢失时通过 t.Fatal 报告。
func TestRendererPreservesSignedURLCharacters(t *testing.T) {
	const signedURL = "https://test-everyline.qtech.cn/intelligent-review?id=998960487&taskId=2092175373579059803&appType=THIRD_PARTY&token=preview-token"
	for _, format := range []Format{FormatJSON, FormatRaw} {
		buffer := &bytes.Buffer{}
		if err := (DefaultRenderer{}).Render(buffer, format, map[string]any{"reviewDetailUrl": signedURL}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buffer.String(), signedURL) || strings.Contains(buffer.String(), `\u0026`) {
			t.Fatalf("format=%s output=%q", format, buffer.String())
		}
	}
}
