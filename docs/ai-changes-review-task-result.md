# AI 变更记录：以 result 替换 review task wait

## 需求目标

开发阶段调整审查任务命令树：用户发起任务后，通过一个面向结果的命令获取最终审查详情，不再手工执行状态轮询和详情查询。

## 用户确认范围

- 直接删除 `review task wait`，不保留兼容别名。
- 新增 `review task result`。
- `result` 内部执行 `status` 轮询，任务成功后调用一次 `info`。
- 保留 `review task status` 和 `review task info` 作为单次原子查询。
- 不修改远端接口、数据结构、权限模型和 `review run` 的业务目标。

## 实现内容

- 在审查工作流层增加等待并获取详情的共享编排能力。
- 让 `review run` 复用共享结果编排，保持原有行为。
- 将 CLI 任务命令从 `start/status/info/wait` 调整为 `start/status/info/result`。
- `result` 成功时输出最终详情并规范化 `reviewDetailUrl`；失败、超时、取消、详情获取失败或详情链接缺失时保留可诊断快照并返回错误。
- 同步更新命令帮助、命令参考、API 映射和测试。
- 命令帮助改为按用户流程、资源生命周期和风险顺序展示：核心审查命令优先，查询在写操作前，删除命令置后；`review task` 使用 `start → result → status → info`。
- 根帮助增加三个视觉分组，仅改变帮助展示，不增加任何命令路径深度。
- 命令帮助条目增加命令路径、`[command]`/`[flags]` 语法后缀，并增加基于实际实现约束的 `Notes` 区块；不改变命令路径和参数解析。
- 根帮助分组标题改为 `Review`、`CLI Management`，并将 config/auth 放入 CLI Management 顶部，参考 Lark CLI 使用英文分组名称；仅改变展示文案。
- 命令列表使用相对命令路径、紧凑列宽，移除重复的 `everyline-cli` 前缀和末尾 `Use` 引导，Notes 移至帮助末尾。

## 验证

- `go test ./...`：通过
- `go vet ./...`：通过
- `make build`：通过
- 实际检查 `review task -h`：显示 `result`，不显示 `wait`
- 实际检查 `review task result -h`：显示“等待审查完成并获取最终结果”及自动获取详情说明
