# `review task result` 行为契约

## 1. 目标与范围

### 1.1 用户确认的目标

用户在发起审查任务后，只关心最终审查结果，不需要手工编排 `status -> success -> info`。

### 1.2 目标行为

提供以下分步流程：

```text
review task start -> review task result <task-id>
```

其中 `review task result` 在 CLI 内部：

1. 重复调用任务状态接口；
2. 任务处于 `running` 时按轮询间隔继续等待；
3. 任务进入 `success` 后调用一次详情接口；
4. 校验并规范化可用详情链接；
5. 输出最终审查详情；后端提供顶层 `url` 时补充 `reviewDetailUrl`。

### 1.3 非目标

- 不修改远端 OpenAPI 接口、请求参数或服务端状态机。
- 不修改 `review task status` 的单次查询语义。
- 不修改 `review task info` 的单次详情查询语义。
- 不改变 `review run` 从文件上传到最终结果的业务目标。
- 不增加本地缓存、后台守护进程或新的异步任务存储。

### 1.4 命令兼容边界

`review task` 命令树只提供 `start`、`result`、`status` 和 `info`；不提供 `wait` 别名或兼容开关。

## 2. 运行契约

- `Workflow.Wait` 轮询 `Status` 并返回最后状态快照。
- `Workflow.WaitForResult` 在 `success` 后调用一次 `Info`；响应包含可用顶层 `url` 时规范化为 `reviewDetailUrl`，没有预览链接时仍返回成功详情。
- `review task result` 注册等待与详情编排；`review task status` 和 `review task info` 保持单次查询。
- 每次轮询状态默认写入 stderr，最终业务结果写入 stdout。

## 3. 目标命令契约

```text
review task start (--input file.json | --data '{...}')
review task result --task-id [--business-id]
                    [--visibility-scope contractResult]
                    [--interval 2s] [--deadline 10m]
review task status --task-id [--business-id]
                   [--visibility-scope contractResult]
review task info --task-id [--business-id]
                 [--visibility-scope contractResult]
```

CLI 不再暴露 `--app-type`；任务查询不发送 appType。发起审查的 start/run 输入契约另行只保留 API 必填字段。

命令提示：

```text
result: 等待审查完成并获取最终结果
```

详情说明：

```text
等待已有审查任务进入终态。
任务成功后，CLI 会自动获取并输出最终审查结果。
```

## 4. 运行链路与行为规则

```text
CLI result
  -> Workflow 的任务结果编排
  -> API.Status（running 时按间隔轮询）
  -> success
  -> API.Info（仅调用一次）
  -> 校验可选详情链接
  -> 输出完整详情，并按需补充 reviewDetailUrl
```

### 4.1 成功

- `Status` 返回 `success` 后只调用一次 `Info`。
- `Info` 可包含顶层 `url`；存在时补充 `reviewDetailUrl`，缺失时保留完整成功详情。
- 命令以退出码 `0` 结束。
- 标准输出只包含最终结果；每次轮询收到的 status 默认写入 stderr，不污染 JSON stdout。

### 4.2 失败

- `Status` 返回 `fail` 时停止轮询，不调用 `Info`。
- 保留失败状态快照用于诊断。
- 命令以非零退出码结束，并返回可识别的任务失败错误。

### 4.3 超时或取消

- 达到 `deadline` 或收到取消信号时停止轮询。
- 返回最后一次状态快照。
- 命令以非零退出码结束，不声称已获取最终结果。

### 4.4 成功后详情获取失败

- `Status` 已确认成功后调用一次 `Info`。
- `Info` 失败时保留成功状态快照，并返回错误。
- 命令不得以成功退出码或“结果完整”语义结束。

### 4.5 状态边界

- `running` 是唯一允许继续轮询的中间态。
- `success` 是成功终态，`fail` 是失败终态。
- 空状态按任务不存在或当前用户无权访问处理。
- 其他状态保留快照并返回未知状态错误。仓库没有后端完整状态枚举，因此不把 `queued`、`pending` 或其他未确认值静默转换为可等待状态。

### 4.6 身份与查询上下文

调用 `Info` 时必须复用 `result` 命令构造的同一个 `TaskQuery`，包括 `task-id`、业务标识和可见性范围；不得因为内部编排而丢失 `contractResult` 查询上下文。

## 5. 测试契约

| REQ-ID | SC-ID | TC-ID | 可验证行为 |
|---|---|---|---|
| REQ-RESULT-001 | SC-RESULT-001 | TC-RESULT-001 | `running -> success` 时按顺序调用多次 `Status`、一次 `Info`，返回完整详情 |
| REQ-RESULT-001 | SC-RESULT-002 | TC-RESULT-002 | `fail` 时停止轮询、不调用 `Info`，保留失败快照并返回错误 |
| REQ-RESULT-001 | SC-RESULT-003 | TC-RESULT-003 | 轮询取消/截止时保留最后快照并返回非成功错误 |
| REQ-RESULT-001 | SC-RESULT-004 | TC-RESULT-004 | `Info` 失败时保留成功状态并返回详情错误 |
| REQ-RESULT-001 | SC-RESULT-005 | TC-RESULT-005 | `contractResult` 查询上下文完整传递到 `Status` 与 `Info` |
| REQ-RESULT-002 | SC-RESULT-006 | TC-RESULT-006 | 命令树只注册 `review task result` 作为等待并获取详情的命令 |
| REQ-RESULT-002 | SC-RESULT-007 | TC-RESULT-007 | `review task result --help` 说明会等待并自动获取最终结果 |
| REQ-RESULT-002 | SC-RESULT-008 | TC-RESULT-008 | 命令参考文档使用 `result` 作为等待并获取详情的命令 |
| REQ-RESULT-003 | SC-RESULT-009 | TC-RESULT-009 | 根帮助中的命令条目显示相对命令路径及 `[command]`/`[flags]` 语法后缀，Usage 保留完整程序名 |
| REQ-RESULT-003 | SC-RESULT-010 | TC-RESULT-010 | 根帮助显示基于实际实现的 `Notes` 使用提示 |
| REQ-RESULT-003 | SC-RESULT-011 | TC-RESULT-011 | `review task` 帮助显示结果编排、单次查询和可见性范围约束 |

## 6. 实现边界

- 复用 `Workflow.Wait` 的状态轮询和错误语义。
- 在领域层增加“等待并获取详情”的最小组合能力，供 `review run` 和 `review task result` 共用，避免命令层互相调用。
- 新增 CLI 层 `newReviewTaskResultCommand`，参数和输出约定与现有任务查询命令保持一致。
- 同步更新帮助测试、命令参考和 API 映射中的本地编排说明。
- 使用自定义帮助渲染显示完整命令语法；不改变 Cobra 命令路径和参数解析。
- 使用命令元数据渲染 `Notes`，只记录输入前置条件、权限/可见性、dry-run、确认参数和本地编排行为等实际约束。

## 7. 命令帮助展示顺序

命令帮助按用户主流程、资源生命周期和风险顺序展示，不采用默认字母排序：

```text
根命令：review -> checklist -> rule -> config -> auth -> completion -> version -> help
review：run -> file -> subject -> task
review task：start -> result -> status -> info
review file：upload -> upload-url
checklist：list -> create -> batch-create -> update -> batch-update -> delete -> batch-delete
rule：group -> list -> create -> batch-create -> update -> batch-update -> delete -> batch-delete
rule group：list -> create -> update -> delete
auth：login -> status -> use -> logout
config：add -> use -> list -> show
```

其中 `status` 按用户确认保留为可见命令，但在 `result` 之后、`info` 之前展示；删除类命令统一置于相关资源命令末尾。

根帮助只增加职责分组，不增加命令路径层级：

```text
Review：review、checklist、rule
CLI Management：config、auth、completion、version、help
```

上述命令仍分别通过 `everyline-cli <command>` 直接调用。

## 8. 命令语法与 Notes

命令条目显示相对命令路径，并统一补充语法后缀；`Usage` 仍显示完整的 `everyline-cli` 程序名：

```text
review [command] [flags]
review task result [flags]
config add <name> [flags]
```

帮助说明层次固定为：`Short` 表示动作，`Long` 表示语义，`Examples` 表示可复制用法，`Notes` 表示实际约束和特殊行为。Notes 不重复完整 API 文档，也不引入未实现的能力。
命令与描述之间使用紧凑列宽；帮助末尾不再重复输出 `Use "... --help"` 引导，Notes 放在 Flags 和 Global Flags 之后。
Short/Long 说明中的空行会被压缩为连续文本行，避免帮助顶部出现多余的多行空白。
