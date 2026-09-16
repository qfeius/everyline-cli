# CLI 来源 Header

## 请求行为

`review`（含上传、主体提取、审查、轮询）、`checklist`、`rule` 的实际业务请求，经统一 `openplatform.Client.doOnce` 在发送前执行来源 hook。GET 每次重试重新探测；`review run` 的每个阶段和每次轮询也重新探测。不在登录、安装或 Profile 中缓存来源。

| Header | 值 |
|---|---|
| X-Qfei-Channel-Type | cli |
| X-Qfei-Agent-Source-Type | doubao / doubaoWork / doubaoWorkmates / workbuddy / codex / unknown |
| X-Qfei-Product-Code | contract-review |
| X-Qfei-Evidence-Type | macos_code_signature / windows_package_identity / windows_authenticode / windows_runtime_environment / process_executable_path / process_name / none；产品路径冲突时为 conflicting_process_evidence |
| X-Qfei-Channel-Confidence | high / medium / low / unknown |
| X-Qfei-Detector-Version | process-ancestry-v6 |
| X-Qfei-Rule-Id | 命中的规则编号；无匹配时省略 |

不再发送旧字段 `X-Qfei-Request-Source-Type`。Header 为来源归因信息，不是客户端身份证明或鉴权依据。未识别到来源不会拒绝业务请求。

macOS 来源识别使用 `codesign --verify --strict --ignore-resources` 验证代码签名，再匹配登记的 Bundle ID 和 Team ID。资源内容不属于此处的完整性校验范围：例如 WorkBuddy 在应用内生成 Python `__pycache__`，不应因此失去来源归因。代码签名验证失败或身份变化时，保留路径／进程名识别结果；签名验证成功且身份登记匹配才提升为 high。Intel 与 ARM 使用相同规则。

飞书内豆包工作（Mac/Windows）、Office 云端、工作伙伴云端、WorkBuddy Web 和 Mac 执行工具受限时的组合兜底使用 low 置信度，新增证据类型 `macos_signed_host_runtime`、`windows_signed_host_runtime`、`macos_runtime_environment`、`linux_runtime_environment`；详见[增量覆盖与样本回放](runtime-host-coverage.md)。

## 代码结构

- `internal/invocation/`：从 contract-cli 当前已验证的探测模块移植，保留父进程回溯、macOS 签名、Windows 包身份/签名规则和隔离超时，EveryLine 产品编码使用 contract-review。不依赖同事的 cli-inspect 仓库，也不要求本机安装 contract-cli。
- `internal/cli/environment_hook.go`：统一装配三个业务模块的 HTTP 客户端，每次请求探测并填 Header；`invocation_source` 诊断只打印七个来源白名单字段，不输出 token 或完整进程信息。`--verbose` 另输出独立的 `request_trace` 诊断。
- `internal/openplatform/client.go`：有序 BeforeRequestHook 扩展点，位于每次 HTTP 尝试内、发送前，不挂到 token Provider。
- `internal/app/app.go`：在配置和认证初始化之前分派私有探测辅助入口，辅助进程不递归发请求。

探测模块当前是仓库内独立移植，并非两仓库已共享同一个发布模块。指纹规则升级时需同步两个 CLI 的规则和测试。使用与合同 CLI 相同的 gopsutil v4.26.7 和 x/sys v0.41.0；未改动合同 CLI、后端、认证协议或更新功能。

## 超时与兼容

- 一次探测预算 5 秒（含父进程发现与签名检查）；签名检查前先返回基础报告；超时保留已收到的报告，并终止回收辅助进程，Unix 同时终止其签名子进程组。清理允许少量调度开销。
- 未收到有效报告的失败/崩溃、非法报告或输出超限降级为 unknown；已收到有效报告后辅助进程失败或超时则保留该报告。依旧发送 cli、contract-review 和探测版本。Rule-Id 无值时不发送。
- 探测子 Context 超时不取消父业务 Context；但探测仍计入原有 `--timeout` 和工作流 deadline，不额外放宽业务截止时间。
- 仅处理来源字段；trim 后为空、超过 256 字节或包含非可打印 ASCII 的来源值丢弃，不改请求体、认证、成功码及写操作不重试的策略。
- macOS/Windows 使用对应身份探测；Windows 未命中时再检查宿主环境标记，详见 [Windows 环境兜底](windows-runtime-fallback.md)。Mac/Linux 在无既有命中且无签名不匹配时补充已登记的组合运行时证据。没有匹配证据时保持 unknown。
- help、version、config、dry-run、print-input 不探测；token 获取/刷新不携带来源 Header。来源不写入 JWT，不写入 Profile。
- 七个来源 Header 与独立的[请求 Trace](request-trace.md)并存；来源探测和 Trace 生成分别执行，不缓存客户端来源，也不把一次工作流的所有请求合并为一个 Trace。

## 验证

自动测试：`go test -race ./...`、`go vet ./...`、`npm test`。覆盖两种身份、三个业务模块、multipart/JSON、工作流多次轮询、GET 重试、非法字段、超时降级与辅助进程回收；实际编译入口到本机 HTTP 接收端的测试不使用真实业务凭证。

本地调试可先 `make build`，使用已有且明确选择的测试 Profile，在原业务命令上追加 `--verbose --output json`，例如：

```bash
./bin/everyline-cli checklist list --profile <测试Profile> --verbose --output json
```

分别从目标客户端执行。stderr 的 `invocation_source` 是实际附加值；dev 验收还需从业务服务接收到的 HTTP 请求核对字段。CLI 日志/本机测试不等于 dev 链路已验收。审查/写操作可能产生实际业务或计费，仅在明确授权的测试数据上执行。

## 交付边界与业务接收方

验收点是业务服务收到 CLI 的来源 Header：

- 智审接口：CLI → open-gateway → open-platform-contract-review → platform-contract-api。
- 清单/规则管理：CLI → open-gateway → open-platform → review-rule。

业务服务可直接从 HTTP Request 读取，例如 Python 的 `request.headers.get("X-Qfei-Agent-Source-Type")` 或 Java 的 `request.getHeader("X-Qfei-Agent-Source-Type")`。是否写入 Context、日志、任务数据或计费上报，由业务方自行决定。

本仓库交付 CLI 来源 Header 的生成与发送能力；dev 验收需核对上述业务服务实际接收到的字段。CLI 本地测试不能替代部署环境的透传验证。

后端 Context、日志采集及业务服务之后的计费中台等下游透传不属于本次交付范围。

Windows WorkBuddy 的证书轮换降级策略见 [升级兼容修复](workbuddy-upgrade-compatibility.md)；其签名不匹配不再阻断进程／路径归因，所有已登记客户端现统一使用 [v6 稳定性策略](attribution-stability.md)。
