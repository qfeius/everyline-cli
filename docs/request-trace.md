# 请求 Trace

## CLI 协议

与 contract-cli 当前实现保持一致，使用 Go 标准库 `crypto/rand` 生成非零的 16 字节 Trace ID 和 8 字节 Span ID，不引入新的依赖。

| 字段 | 行为 |
|---|---|
| `traceparent` | `00-<32位小写Trace ID>-<16位小写Span ID>-01` |
| `X-Log-Id` | 与该请求 `traceparent` 的 Trace ID 相同 |
| `Response.TraceID` | 保存 CLI 生成的 Trace ID，不使用服务端返回值覆盖 |
| `Response.RequestID` | 保持原有服务端 request ID 解析规则，与 Trace ID 独立 |

所有经过 `openplatform.Client` 的 app/user 业务请求均自动接入，包括 multipart 上传、JSON 写入、清单、规则和审查查询。Trace 在每次 HTTP 发送前统一附加，覆盖调用方或来源 Hook 中的旧同名 Header，不修改调用方 Header map、认证和请求体。

一个逻辑请求生成一次 Trace ID；GET 重试保持 Trace ID，每次发送使用新 Span ID。`review run` 的上传、主体提取、发起审查、每次轮询和详情读取分别产生独立 Trace。本次不新增工作流父 Trace、业务幂等或 Span 上报。

本地输入校验和 token 获取完成后才创建业务 Trace；登录、token 刷新、help、config、version、dry-run 和探测辅助进程均不携带业务 Trace。保持既有超时预算和 GET 重试策略，写操作不因 Trace 增加重试；沿用 upstream 的用户令牌刷新策略，可信鉴权失败刷新成功后可重放一次。重放仍使用同一 Trace ID、新 Span ID，并重新探测来源。

## 排查方式

在已有业务命令追加 `--verbose`：stderr 输出 `request_trace operation=... method=... trace_id=... traceparent=...`。每次实际发送输出一行，不记录 token、合同内容或完整 URL/query。stdout 的 JSON、YAML、table、raw 结果保持原有格式。

业务 HTTP 阶段失败时，错误末尾附加 `(trace_id=...)`；无需开启 verbose 即可获得。`TraceError.Unwrap` 保留原始 API、网络和取消错误，`errors.As`、`errors.Is` 和 CLI 退出码继续有效。Trace 只说明本地生成了关联 ID，不证明请求已到达服务端。

## 服务端边界

本次交付限于 Everyline CLI。按当前本地服务端源码：智审平台 API、review-rule 已能读取 `X-Log-Id`，也能从合法 `traceparent` 提取 Trace ID，并用于日志关联；平台 API 的部分内部 AI 客户端可继续透传 `X-Log-Id`。

这些能力不等同于完整的分布式 Trace。平台 API 的现有 HTTP Hook 只显式注入 `X-Log-Id`；Trace 上下文接续、各层 Span 采集、异步/MQ 与持久化任务传播、Collector 部署配置需要另立专项验证和补齐。CLI 不修改这些服务端实现，也不改变后端业务 `x-trace-id`、request ID 或 Langfuse 语义。

## 本地验证

`go test -race ./...`、`go vet ./...`、`npm test`；测试覆盖合法 Header、重试和轮询、错误分类、token 排除与输出隔离。编译实际 CLI 入口并通过本机 HTTP 接收端核对请求 Header、verbose 诊断及失败退出码。真实环境的日志和调用树仍需单独验收。
