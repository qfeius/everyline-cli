// Package schemas 暴露随 CLI 二进制发布的 JSON Schema 契约。
package schemas

import "embed"

// Files 包含仓库 schemas 目录中的全部 Draft 2020-12 契约。
//
//go:embed *.schema.json
var Files embed.FS
