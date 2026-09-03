package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Format 是稳定输出协议支持的格式枚举。
type Format string

const (
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
	FormatTable Format = "table"
	FormatRaw   Format = "raw"
)

// Renderer 将领域 ViewModel 渲染到指定输出流。
type Renderer interface {
	Render(io.Writer, Format, any) error
}

// DefaultRenderer 提供 JSON、YAML、Table 和 Raw 四种稳定实现。
type DefaultRenderer struct{}

// ParseFormat 校验用户或 Profile 提供的输出格式。
// 入参：value string 为格式名称。
// 返回值：Format 为规范枚举；error 在不支持时非 nil。
func ParseFormat(value string) (Format, error) {
	format := Format(strings.ToLower(strings.TrimSpace(value)))
	switch format {
	case FormatJSON, FormatYAML, FormatTable, FormatRaw:
		return format, nil
	default:
		return "", fmt.Errorf("output 必须是 json、yaml、table 或 raw")
	}
}

// Render 将业务结果写入 writer；所有格式都以换行结束，适合 Shell 管道。
// 入参：writer io.Writer 为 stdout；format Format 为输出协议；value any 为领域结果。
// 返回值：error，编码或写入失败时非 nil。
func (DefaultRenderer) Render(writer io.Writer, format Format, value any) error {
	switch format {
	case FormatJSON:
		return encodeJSONOutput(writer, value, "  ", "JSON")
	case FormatRaw:
		return encodeJSONOutput(writer, value, "", "Raw")
	case FormatYAML:
		content, err := yaml.Marshal(normalizeForYAML(value))
		if err != nil {
			return fmt.Errorf("编码 YAML 输出: %w", err)
		}
		_, err = writer.Write(content)
		return err
	case FormatTable:
		return renderTable(writer, value)
	default:
		return fmt.Errorf("不支持的输出格式: %s", format)
	}
}

// encodeJSONOutput 写出不进行 HTML 转义的 JSON，保证签名 URL 中的 & 等字符逐字保留。
// 入参：writer io.Writer 为 stdout；value any 为业务结果；indent/formatName string 为缩进和错误标签。
// 返回值：error，编码或写入失败时非 nil。
func encodeJSONOutput(writer io.Writer, value any, indent string, formatName string) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if indent != "" {
		encoder.SetIndent("", indent)
	}
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("编码 %s 输出: %w", formatName, err)
	}
	return nil
}

// renderTable 将对象渲染为 KEY/VALUE，将对象数组渲染为稳定列集合。
// 入参：writer io.Writer 为 stdout；value any 为业务结果。
// 返回值：error，标准化或写入失败时非 nil。
func renderTable(writer io.Writer, value any) error {
	normalized, err := normalizeValue(value)
	if err != nil {
		return err
	}
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	switch typed := normalized.(type) {
	case map[string]any:
		keys := sortedKeys(typed)
		if _, err := fmt.Fprintln(table, "KEY\tVALUE"); err != nil {
			return err
		}
		for _, key := range keys {
			if _, err := fmt.Fprintf(table, "%s\t%s\n", key, compactValue(typed[key])); err != nil {
				return err
			}
		}
	case []any:
		if err := renderRows(table, typed); err != nil {
			return err
		}
	default:
		if _, err := fmt.Fprintln(table, compactValue(typed)); err != nil {
			return err
		}
	}
	return table.Flush()
}

// renderRows 渲染对象数组；非对象元素退化为单 VALUE 列。
// 入参：writer io.Writer 为表格 writer；rows []any 为行数据。
// 返回值：error，写入失败时非 nil。
func renderRows(writer io.Writer, rows []any) error {
	columnSet := map[string]struct{}{}
	objects := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		object, ok := row.(map[string]any)
		if !ok {
			if _, err := fmt.Fprintln(writer, compactValue(row)); err != nil {
				return err
			}
			continue
		}
		objects = append(objects, object)
		for key := range object {
			columnSet[key] = struct{}{}
		}
	}
	columns := make([]string, 0, len(columnSet))
	for key := range columnSet {
		columns = append(columns, key)
	}
	sort.Strings(columns)
	if len(columns) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(writer, strings.Join(columns, "\t")); err != nil {
		return err
	}
	for _, object := range objects {
		values := make([]string, len(columns))
		for index, column := range columns {
			values[index] = compactValue(object[column])
		}
		if _, err := fmt.Fprintln(writer, strings.Join(values, "\t")); err != nil {
			return err
		}
	}
	return nil
}

// normalizeValue 通过 JSON 往返把结构体和别名 map 统一成通用树。
// 入参：value any 为任意可 JSON 序列化值。
// 返回值：any 为通用 JSON 树；error 为编码或解码失败。
func normalizeValue(value any) (any, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("标准化表格数据: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return nil, fmt.Errorf("解析表格数据: %w", err)
	}
	return normalized, nil
}

// normalizeForYAML 先应用 JSON 标签和 json.Number 语义，再交给 YAML 编码器。
// 入参：value any 为领域结果。
// 返回值：any，为 YAML 编码准备的通用树；失败时原样返回以便上层得到明确编码错误。
func normalizeForYAML(value any) any {
	normalized, err := normalizeValue(value)
	if err != nil {
		return value
	}
	return normalizeYAMLNumbers(normalized)
}

// normalizeYAMLNumbers 递归把 json.Number 还原为整数或浮点数，避免 YAML 把标识数字错误加引号。
// 入参：value any 为通用 JSON 树节点。
// 返回值：any，为 YAML 编码器可正确识别数值类型的树节点。
func normalizeYAMLNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer
		}
		if floating, err := typed.Float64(); err == nil {
			return floating
		}
		return typed.String()
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = normalizeYAMLNumbers(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = normalizeYAMLNumbers(item)
		}
		return result
	default:
		return typed
	}
}

// compactValue 将嵌套值编码成单行 JSON，保持表格一行一个字段。
// 入参：value any 为单元格值。
// 返回值：string，为人可读的单行文本。
func compactValue(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		return fmt.Sprint(typed)
	default:
		content, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(content)
	}
}

// sortedKeys 返回稳定排序的对象键。
// 入参：value map[string]any 为业务对象。
// 返回值：[]string，为升序键集合。
func sortedKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
