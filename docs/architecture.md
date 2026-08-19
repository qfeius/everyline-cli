# 架构

CLI handler 只负责参数解析、输入校验、调用模块和输出渲染。`internal/openplatform.Client` 持有基础 URL、Bearer token、响应 envelope、request ID、超时与 GET 安全重试，并在发送前按 operation 校验 method/path 与嵌入的 JSON Schema；`review run` 的复合输入也直接由 `review-run.schema.json` 在 `internal/contracts` 中校验，避免 CLI 与 Schema 各自维护一套结构规则；`internal/review.Workflow` 隐藏上传、主体提取、发起、轮询和详情编排。

```text
Shell / Agent / CI
        |
      Cobra
        |
        +--> Profile Store / Identity Token Provider (app / user)
        +--> Review Workflow --> Review Service ----+--> OpenPlatform Client --> HTTP
        +--> Checklist / Rule Service --------------+
        +--> Renderer (json / yaml / table / raw)
```

## 安全边界

- Profile 只保存 `base_url/user_base_url/auth_url/token_url/app_id/default_identity/default_output`，不保存 `app_secret` 或用户 access token。
- `app` 身份使用 tenant token；`user` 身份使用 everyline 自有认证页面交接的用户 token。OpenPlatform Client 在 Service 请求前按身份选择 token 和业务基址；未声明专用 user 路由的接口暂复用已验证的相对路径。
- Profile 和 token 缓存目录权限为 `0700`、文件权限为 `0600`；跨进程文件锁保护完整的读改写事务。
- 业务 HTTP Adapter 只接受 `/open-apis/` 相对路径，防止 Bearer token 被转发到任意主机。
- GET 可对网络错误、429 和 5xx 做至多三次退避重试；POST 不自动重试；token、请求和退避共享一次 `--timeout` 总预算。
- stdout 只承载业务结果，`--verbose` 进度只写 stderr。

## 兼容策略

合同审查请求使用后端 V3.1.9 已确认字段；字段捷径入口同步到后端 `/open-api/feishu/v1/smartAudit/init` 的文件、位置、额度和签名字段，并通过 `/open-api/v1/smartAudit/info` 完成独立的状态与结果轮询。文件快照按当前范围不纳入 CLI。M3 清单与 M4 规则命令已实现。清单创建按公开说明要求 `name` 和 `reviewRuleIds`。四个仍缺字段级详情页的批量创建/更新操作只允许本地 `--dry-run`，真实请求会返回“接口契约尚未核验”，避免猜测 DTO。响应 `data` 保留对象、数组或标量原始形状，平台增加展示字段时不会截断 JSON/YAML/Raw 输出。
