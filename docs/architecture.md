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

- Profile 只保存环境地址、默认身份、默认输出、app 身份使用的非敏感 `app_id` 和 user OAuth 配置；user 身份不要求 `app_id`。Profile 不保存 `app_secret`、OAuth code/verifier 或用户 access token；`auth login --app-id` 成功后可以更新非敏感的 `app_id`。显式 `--save-app-secret` 使用 macOS Keychain 或文件回退，仅按 Profile 保存 app secret。
- `app` 身份使用 tenant token；`user` 身份统一走 authorization code + PKCE、loopback callback 和浏览器授权。OpenPlatform Client 在 Service 请求前按身份选择 token 和业务基址；未声明专用 user 路由的接口暂复用已验证的相对路径。
- token 过期时间只使用服务端返回的 `expire`/`expires_in`；app 环境变量 token 的过期时间未知但可用，CLI 不伪造固定 24 小时，也不在服务端未声明支持前发送 refresh grant。
- Profile、token 和 secret 缓存目录权限为 `0700`、文件权限为 `0600`；跨进程文件锁保护完整的读改写事务。app secret 只有在 token endpoint 成功后才落盘，`auth logout` 不删除它。
- 业务 HTTP Adapter 只接受 `/open-apis/` 相对路径，防止 Bearer token 被转发到任意主机；自更新模块仅接受显式 HTTPS manifest 和制品地址。
- GET 可对网络错误、429 和 5xx 做至多三次退避重试；POST 不自动重试；token、请求和退避共享一次 `--timeout` 总预算。
- stdout 只承载业务结果，轮询 status 和 `--verbose` 进度只写 stderr。

## 兼容策略

合同审查请求使用已确认的字段；文件快照和字段捷径接口不纳入 CLI。清单创建按公开说明要求 `name` 和 `reviewRuleIds`。清单与规则的批量创建、批量更新均使用已核验的数组请求体并支持真实调用，保持单次请求的全有或全无语义。响应 `data` 保留对象、数组或标量原始形状，平台增加展示字段时不会截断 JSON/YAML/Raw 输出。自更新独立于业务 Profile，使用 HTTPS manifest、平台制品和 SHA-256 校验，并在校验成功后原子替换本地二进制。
