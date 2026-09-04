# 审查清单对话分页规格增量

## Requirement: Skill 必须分页展示全部规则来源候选

EveryLine Skill MUST 完整读取 CLI 返回的所有清单分页，把固定内置项与真实清单组成统一候选，并以每页最多 4 项的方式展示，不得因宿主单次选项容量截断候选。

### Scenario: 清单超过一页

- **GIVEN** 内置项与 CLI 返回的真实清单组成超过 4 项的统一候选
- **WHEN** Skill 进入清单选择阶段
- **THEN** 每页必须展示最多 4 个统一候选
- **AND** 必须显示 `第 X/Y 页`
- **AND** 必须提供上一页、下一页和按名称搜索
- **AND** 6 个真实清单加内置项时，第 1 页必须为 0、1、2、3，第 2 页必须为 4、5、6

### Scenario: 用户跨页浏览后选择

- **GIVEN** 用户已浏览多个清单页面
- **WHEN** 用户一次回复一个或多个此前展示过的稳定全局编号
- **THEN** Skill 必须从首次完整查询建立的映射中取得全部真实清单 ID
- **AND** 立即冻结选择并进入下一业务阶段

### Scenario: 用户输入未展示清单的完整名称

- **GIVEN** 目标清单存在于完整候选集合但不在当前页
- **WHEN** 用户输入该清单完整名称
- **THEN** Skill 必须在完整候选中匹配并加入选择
- **AND** 名称不唯一时必须展示可区分候选，不得猜测

## Requirement: 清单与立场方必须分为两个独立阶段

EveryLine Skill MUST 先完成清单选择，再通过新的独立交互完成立场方选择，不得把两类问题放入同一个选择卡片。

### Scenario: 清单尚未选择

- **GIVEN** 用户仍在浏览或搜索清单
- **WHEN** Skill 准备下一次交互
- **THEN** 只能继续清单阶段
- **AND** 不得同时展示立场方问题

### Scenario: 清单选择完成

- **GIVEN** 用户一次回复至少一个有效规则来源
- **WHEN** Skill 固化清单选择
- **THEN** 下一次独立交互必须进入立场方选择
- **AND** 不得要求额外的完成或确认回复
- **AND** 最终任务参数中的清单 ID 必须来自完整候选映射

## Requirement: WorkBuddy 必须使用原生清单多选组件

EveryLine Skill MUST 在 WorkBuddy 清单阶段调用 `AskUserQuestion`，把当前页统一候选放入 `options` 并设置 `multiSelect=true`；主体和强度使用同一工具的单选模式。用户已经明确选择时不得重复询问。

### Scenario: WorkBuddy 展示清单页

- **GIVEN** WorkBuddy 已暴露 `AskUserQuestion`
- **AND** Skill 已冻结统一候选、稳定编号和当前页
- **WHEN** Skill 需要收集清单选择
- **THEN** 必须调用 `AskUserQuestion`
- **AND** 必须设置 `multiSelect=true`
- **AND** 业务 `options` 只能包含当前页统一候选；当前页只有 1 个业务候选时可补充 1 个不映射为规则来源的导航项
- **AND** 翻页只更新当前页并沿用原快照

### Scenario: WorkBuddy 组件未就绪

- **GIVEN** WorkBuddy 未暴露或拒绝 `AskUserQuestion`
- **WHEN** Skill 需要收集清单选择
- **THEN** 必须保留当前页和完整编号映射
- **AND** 必须报告组件能力缺口
- **AND** 不得使用 Markdown 表格模拟选项组件

## Requirement: Codex 与豆包必须复用统一候选快照

EveryLine Skill MUST 在 Codex 当前工具只支持互斥单选或豆包未暴露选项能力时，使用与 WorkBuddy 相同统一候选快照的稳定编号协议。

### Scenario: Codex 提供单选选项工具

- **GIVEN** Codex 当前回合提供 `request_user_input` 等原生结构化选项工具
- **AND** 当前问题是立场方或审查强度单选
- **WHEN** Skill 需要收集该项输入
- **THEN** 应使用独立原生选项卡
- **AND** 展示值必须映射到本次冻结候选或固定中文强度

### Scenario: Codex 清单需要多选或分页

- **GIVEN** 当前宿主选项工具只支持互斥单选
- **WHEN** Codex 收集审查清单
- **THEN** 必须使用稳定全局编号文字协议
- **AND** 不得把多选拆成多轮是/否问题或新增完成确认

### Scenario: Codex 当前模式没有选项工具

- **GIVEN** 当前回合未提供原生结构化选项工具
- **WHEN** Skill 需要收集任一选择
- **THEN** 必须使用相同冻结映射的编号文字协议
- **AND** 不得仅为展示选项卡切换协作模式
