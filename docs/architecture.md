# 架构

CLI handler 只负责参数解析、输入校验、调用模块和输出渲染。`internal/openplatform.Client` 持有基础 URL、Bearer token、响应 envelope、request ID、超时与 GET 安全重试；`internal/review.Workflow` 隐藏上传、快照、主体提取、发起、轮询和详情编排。

```text
Shell / Agent / CI
        |
      Cobra
        |
        +--> Profile Store / Token Provider
        +--> Review Workflow --> Review Service --> OpenPlatform Client --> HTTP
        +--> Renderer (json / yaml / table / raw)
```

## 安全边界

- Profile 只保存 `base_url/token_url/app_id/default_output`，不保存 `app_secret`。
- token 缓存目录权限为 `0700`，文件权限为 `0600`。
- 业务 HTTP Adapter 只接受 `/open-apis/` 相对路径，防止 Bearer token 被转发到任意主机。
- GET 可对网络错误、429 和 5xx 做至多三次退避重试；POST 不自动重试。
- stdout 只承载业务结果，`--verbose` 进度只写 stderr。

## 兼容策略

请求使用后端 V3.1.9 已确认字段。响应 `data` 使用保留未知字段的 `Document`，平台增加展示字段时 CLI 不会截断 JSON/YAML/Raw 输出。M3 清单与 M4 规则接口尚未实现，避免在缺少字段级详情页时猜测 DTO。

