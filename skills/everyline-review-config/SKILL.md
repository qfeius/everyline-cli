---
name: everyline-review-config
description: "使用 EveryLine CLI 查询或管理审查清单、审查规则和规则分组，包括新增、修改、调整归属和删除；仅在合同审查中选择已有清单时不使用本 Skill。"
metadata:
  requires:
    bins: ["everyline-cli"]
    skills: ["everyline-cli"]
  cliHelp: "everyline-cli checklist --help;everyline-cli rule --help;everyline-cli rule group --help"
---

# EveryLine 审查配置管理

处理审查清单、审查规则和规则分组的查询与管理。执行具体操作前必须读取 [references/management.md](references/management.md)，并遵守 `everyline-cli` 公共 Skill 的 Profile、身份、授权、结构化输出和故障恢复约定。

## 触发与路由

在用户明确要求查询、创建、重命名、重新配置或删除清单，查询、创建、编辑、调整归属或删除规则，查询、创建、重命名或删除规则分组，或查看引用与删除影响时使用。

- 只有用户目标是查询或改变清单、规则、规则分组本身时才进入本 Skill；“用清单 A 审查这份合同”等在审查中选择已有清单的请求属于 `everyline-review`。
- “新建或修改清单后审查合同”拆分为两个连续阶段：先按本 Skill 的写入门槛完成单独确认、写入与回读，再把已确认的业务上下文交回 `everyline-review`；发起审查的请求本身不代表用户确认配置写入。
- 发起、继续或查询合同审查任务不使用本 Skill。
- 查询不授权写入；浏览过对象不表示同意修改，一次确认不沿用到其他目标或操作。

## 运行时就绪门

每个会话首次进入时执行：

```bash
command -v everyline-cli
everyline-cli version --output json
everyline-cli checklist --help
everyline-cli rule --help
everyline-cli rule group --help
```

先按 `everyline-cli` 公共 Skill 记录 `version` 的 `updateRequired`；完整完成当前查询或单次已确认写入及回读后，再执行延迟更新，不在写入链路中途替换 CLI。

每条 user 命令复用公共 Skill 已固定的 Device 会话上下文：豆包普通工作任务（含本地电脑）每次注入同一 `SESSION_ID` 并使用同一初始工作目录，WorkBuddy 每次注入同一 `CODEBUDDY_SESSION_ID`，AgentKit 保留平台工作区与注入密钥。

每次写入前读取具体子命令 `--help` 和输入约束。只有实时 CLI 能提供目标的完整当前状态、真实 ID、分页、引用/级联影响和所需原子写操作时才继续；能力缺失时列出缺口并保留可执行的只读查询。

## 写入门槛

创建、更新、调整归属和删除都遵循：

1. 查询并唯一定位目标，读取最新状态与全部受影响关系。
2. 展示准确目标和变化；更新列出「修改前 → 修改后」，集合列出新增与移除。
3. 针对这一次具体写入取得用户明确确认。
4. 确认后立即复查；状态或影响变化时重新展示并重新确认。
5. 实时帮助支持预检时先用同一请求预检，再调用一次与确认内容一致的原子写命令。
6. 写入后回读目标和关联，按真实最终状态报告。

请求体通过权限受限的临时 JSON 和 `--input` 传递，结束后清理本流程创建的临时文件。结果未知时先回读，不自动重复可能产生副作用的操作。

## 安全与真实性

- 只调用实时帮助中注册的 `everyline-cli` 命令，不使用裸 API、内部地址或自行拼接 HTTP 请求。
- 内置清单只查询或供审查选择，不执行修改或删除。
- 不猜测对象 ID、类型、归属、引用、级联结果、权限或业务成功。
- 不在用户确认前写入，不把预检、退出码或中间进度描述为写入成功。
- 合同、规则内容和附件只发送给用户选择的 EveryLine 流程，不进入其他服务。
