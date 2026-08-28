# 批量创建与批量更新远端执行变更记录

## 2026-08-28 四个批量接口统一支持真实调用

### 修改

- `checklist batch-create` 使用一次 `POST` 请求发送 Checklist 数组。
- `checklist batch-update` 使用一次 `PUT` 请求发送包含 `id` 的 Checklist 数组。
- `rule batch-create` 继续使用一次 `POST` 请求发送 Rule 数组。
- `rule batch-update` 使用一次 `PUT` 请求发送包含 `id` 的 Rule 数组。
- 四个命令继续支持 `--dry-run` 和 `--print-input`，本地预览时不访问 Profile、token 或远端服务。
- 成功响应继续透传服务端 envelope 中的 `data`，不新增批量响应包装。

### 测试

- 补充四个批量 operation 的 method、path 和数组请求体测试。
- 补充 CLI 真实 HTTP 测试，验证批量命令只发送一次请求并透传服务端 `data`。
- 保留批量更新每项必须包含 `id` 的 Schema 与运行时校验。

### 兼容性

- 单项创建、单项更新、查询和删除命令保持不变。
- 原有批量命令语法保持不变；省略 `--dry-run` 后从本地能力错误变为真实远端调用。
