package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"

	"git.qtech.cn/ai/everyline-cli/schemas"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaBaseURL = "https://git.qtech.cn/ai/everyline-cli/schemas/"

var (
	compiledSchemas     map[string]*jsonschema.Schema
	compileSchemasError error
	compileSchemasOnce  sync.Once
)

// validateInput 使用 operation 绑定的 JSON Schema 校验已规范化的逻辑请求输入。
// 入参：spec Spec 为目录项；input any 为 body、query 或路径资源 ID 组成的逻辑输入。
// 返回值：error，Schema 缺失、编译失败或输入不符合契约时包装 ErrContractMismatch。
func validateInput(spec Spec, input any) error {
	compileSchemasOnce.Do(compileSchemas)
	if compileSchemasError != nil {
		return fmt.Errorf("%w: 加载 JSON Schema: %v", ErrContractMismatch, compileSchemasError)
	}
	schema, exists := compiledSchemas[spec.Schema]
	if !exists {
		return fmt.Errorf("%w: %s 未绑定可执行 Schema %s", ErrContractMismatch, spec.OperationID, spec.Schema)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("%w: 编码 %s 契约输入: %v", ErrContractMismatch, spec.OperationID, err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("%w: 解析 %s 契约输入: %v", ErrContractMismatch, spec.OperationID, err)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("%w: %s 不符合 %s: %v", ErrContractMismatch, spec.OperationID, spec.Schema, err)
	}
	return nil
}

// compileSchemas 从嵌入文件中一次性编译全部 Schema，并支持目录内相对 $ref。
// 入参：无。
// 返回值：无；结果写入 compiledSchemas 和 compileSchemasError，供 sync.Once 保护的调用方读取。
func compileSchemas() {
	files, err := fs.Glob(schemas.Files, "*.schema.json")
	if err != nil {
		compileSchemasError = err
		return
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	for _, name := range files {
		content, readErr := schemas.Files.ReadFile(name)
		if readErr != nil {
			compileSchemasError = readErr
			return
		}
		document, parseErr := jsonschema.UnmarshalJSON(bytes.NewReader(content))
		if parseErr != nil {
			compileSchemasError = fmt.Errorf("解析 %s: %w", name, parseErr)
			return
		}
		if addErr := compiler.AddResource(schemaBaseURL+name, document); addErr != nil {
			compileSchemasError = fmt.Errorf("注册 %s: %w", name, addErr)
			return
		}
	}
	compiledSchemas = make(map[string]*jsonschema.Schema, len(files))
	for _, name := range files {
		compiled, compileErr := compiler.Compile(schemaBaseURL + name)
		if compileErr != nil {
			compileSchemasError = fmt.Errorf("编译 %s: %w", name, compileErr)
			return
		}
		compiledSchemas[name] = compiled
	}
}
