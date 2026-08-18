# 测试计划

## 自动化门禁

```bash
go test ./...
go vet ./...
go build ./cmd/everyline-cli
```

覆盖范围：

- Profile 原子落盘、默认选择和权限。
- token 请求字段、`code=0`、过期时间与缓存脱敏。
- HTTP method/path/query/header/body、普通接口 `code=200` 和结构化 APIError。
- 本地上传扩展名、2 MiB 边界与 multipart 字段。
- 工作流的上传、快照、发起、多次轮询和详情顺序。
- CLI 严格 JSON 输入、dry-run、stdout/stderr 与退出码。

## 真实环境冒烟

只有提供测试 Profile 与凭证时才运行：

1. `auth login` 后确认输出不含 token。
2. 对小于等于 2 MiB 的 `.doc/.docx/.pdf` 各上传一次。
3. 使用测试清单发起审查，并以 2 秒间隔等待终态。
4. 确认 JSON stdout 可直接由 `jq` 解析，verbose 进度仅出现在 stderr。

