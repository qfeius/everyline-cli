# 测试计划

## 自动化门禁

```bash
go test ./...
go vet ./...
go build ./cmd/everyline-cli
```

覆盖范围：

- Profile 原子落盘、默认选择和权限。
- user 默认身份的 Profile 可不配置 app-id；app 默认身份和 app 登录仍拒绝缺失 app-id。
- token 请求字段、`code=0`、过期时间与缓存脱敏。
- app 登录的 flag/env/Profile 凭证优先级、app-id 更新、secret 脱敏和失败不污染配置。
- app secret 的显式保存、本地复用、授权失败不落盘、Profile 隔离、Keychain 优先与文件回退、secret 不进入命令参数、并发写入和 `0700/0600` 权限。
- user OAuth metadata 读取、PKCE authorization URL、loopback callback、state/error 拒绝、token form exchange、浏览器打开失败降级、user token 缓存和 raw token 入口删除。
- token 生命周期：服务端 `expire`/`expires_in`、`issued_at`、未知过期 token、status 的 `expiresKnown` 和旧缓存兼容；在服务端契约未支持前不发送 refresh grant。
- HTTP method/path/query/header/body、普通接口 `code=200` 和结构化 APIError。
- `startReview` 请求固定携带 `usageReportContext.reportBusinessCode=everyLine_100_openApi_cli`，且 CLI 输入不能覆盖该值。
- 本地上传业务文件名扩展名、无扩展名路径、2 MiB 边界与 multipart 字段。
- 普通 V3 工作流的统一 deadline、上传、发起、多次轮询、详情顺序和失败阶段结果输出。
- CLI 严格 JSON 输入、dry-run、stdout/stderr 与退出码。
- 帮助输出中各命令业务 flags 的连续顺序、help flag 位置及 Global Flags 分区。

## 真实环境冒烟

只有提供测试 Profile 与凭证时才运行：

1. `auth login` 后确认输出不含 token。
2. 对小于等于 2 MiB 的 `.doc/.docx/.pdf` 各上传一次。
3. 使用测试清单发起审查，并以 2 秒间隔等待终态。
4. 确认 JSON stdout 可直接由 `jq` 解析，result 轮询 status 和 verbose 进度均只出现在 stderr。
