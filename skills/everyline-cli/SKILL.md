---
name: everyline-cli
description: Use the installed EveryLine/智审 CLI as an interactive Agent workflow for app or user authorization, contract review, and custom checklist or rule management. Trigger when the user mentions EveryLine, everyline-cli, 智审 CLI, CLI 合同审查, 审查清单, or 审查规则; do not use for ordinary 智书 contract-cli work.
---

# EveryLine CLI 交互流程

## 目标与职责

把现有 `everyline-cli` 作为确定性执行模块，在对话中完成授权、合同审查以及自定义清单或规则管理。

- Agent 负责识别意图、只追问缺失信息、展示真实候选项、维护本次对话状态，以及在危险写操作前取得确认。
- CLI 负责授权、上传、主体提取、查询、字段校验、任务发起、结果轮询和资源写入。
- 不修改、替换或模拟 CLI 现有接口；只调用实时帮助中存在的命令和参数。
- 不把合同正文、token、app secret、授权码或回调参数输出到对话。

合同审查或授权任务必须读取 [references/review-flow.md](references/review-flow.md)。只有用户明确要求管理清单或规则时，才读取 [references/management.md](references/management.md)。

## 首次使用检查

每个新会话第一次使用时执行：

```bash
command -v everyline-cli
everyline-cli version --output json
everyline-cli --help
```

- 已安装时直接使用，不自动升级。
- 未安装且用户只想临时运行时，建议 `npx everyline-cli`。
- 只有用户明确要求长期安装时，才执行全局安装。
- 执行业务操作前读取对应命令的实时 `--help`；帮助中缺少必要命令或参数时停止该操作，并列出缺口。
- 默认请求 `--output json`。stdout 只作为结构化业务结果解析；stderr 的进度或诊断不能代替最终状态。

合同审查至少需要以下现有命令：

```text
auth login
auth status
review file upload
review subject extract
review task start
review task result
checklist list
```

开始上传合同前，分别读取以下实时帮助：

```bash
everyline-cli review task start --help
everyline-cli review task result --help
```

从对应命令的帮助中确认目标能力：

- `reviewStrength` 直接接受“弱势 / 中立 / 强势”；
- 非空 `selectedCheckListIds` 与 `matchContractTypeRulePackage=true` 可以组合执行；
- `review task result` 会等待终态并获取详情。

任一能力缺失时，说明当前 CLI 版本未满足交互审查流程并停止本次审查，不把中文强度猜成数字、不自动升级，也不先上传合同。授权、帮助查询和不依赖该缺口的只读管理仍可继续。

## 授权交互

授权和业务调用前，先固定本次会话使用的 Profile 与身份：

1. 用户明确提供 Profile 时执行 `everyline-cli config show <profile> --output json` 校验并读取该 Profile；未提供时执行 `everyline-cli config show --output json` 读取当前 Profile。
2. 当前 Profile 不存在或与用户明确指定的环境不一致时停止，请用户提供或修正 Profile；不自动创建、修改或切换 Profile。
3. 用户已经明确 `user` 或 `app` 时直接使用。身份会影响资源范围而用户未指定时，只询问一次使用哪种身份，不因一种身份失败而自动切换另一种身份。
4. Profile 和身份确定后，本次会话的每条授权、查询和写入命令都显式携带 `--profile <profile> --as <identity>`，不依赖当前 Profile 或默认身份。

1. 先执行 `auth status --profile <profile> --as <identity> --output json`。
2. 只有结构化结果中的 `authenticated=true` 表示已授权，退出码为零不等价于已授权。
3. user 未授权时执行 `auth login --profile <profile> --as user` 发起 OAuth 登录。CLI 已打开浏览器时让用户在该页面完成授权；使用 `--no-open-browser` 返回链接时，将链接原样交给用户。不要为了切换呈现方式重启当前 OAuth 会话。
4. app secret 只通过 stdin 或等价安全凭证源传入，不放入命令参数、JSON、日志或回复。
5. 登录完成后重新查询结构化状态；取消、失败或失效时停止业务调用并返回真实原因。

## 对话规则

- 已经从用户消息或可靠 CLI 结果取得的信息不重复询问。
- 当前请求或紧接着的用户回复中只有一个可读取的 DOC、DOCX 或 PDF 附件时，直接把它作为合同来源，使用宿主暴露的本地路径或下载能力准备原始文件，不再要求用户复制绝对路径。具体处理读取 [references/review-flow.md](references/review-flow.md)。
- 候选项必须来自本次身份下的真实 CLI 查询，不猜测主体、清单、规则、分类或资源 ID。
- 用户输入能唯一匹配候选项时直接采用；无匹配或匹配不唯一时，展示可区分候选项并继续询问。
- Agent 可以使用编号帮助用户选择，但传给 CLI 的 ID 必须来自本次查询，不能让用户手工输入内部 ID。
- 审查输入完整后按审查流程自动发起，不额外询问是否开始或是否消耗点数。
- 创建、更新、删除等管理写操作必须遵循管理参考中的确认规则；一次审查中的清单选择不授权修改清单。

## 错误与停止条件

- 解析 CLI 的结构化最终结果；保留真实失败阶段、原因和 CLI 已返回的 request ID 或 task ID。
- 只有远端明确返回可识别的 AI 点数不足语义时，回复“可用 AI 点数余额不足，请充值”。通用计费异常不得包装成点数不足。
- 发起请求超时且没有取得 task ID 时，不自动重试创建；说明结果不确定并停止，避免重复任务或重复扣点。
- 已取得 task ID 后只查询该任务，不再次调用创建。
- 当前 CLI 或服务端不具备原子写入能力时停止对应写操作，不拆成多个请求模拟事务。
