# EveryLine CLI 交互 Skill 安装与验证（Codex / WorkBuddy / 豆包）

本文用于安装 `everyline-cli` CLI 及三项配套 Skill，并验证合同审查的多轮对话流程：`everyline-cli` Skill 负责接入与授权，`everyline-review` 负责单份合同审查，`everyline-review-config` 负责清单和规则管理。CLI 继续负责授权、上传、主体提取、查询和写入；全局 npm 安装默认同时登记 Codex 与 WorkBuddy Skills。

需要逐项评审或测试当前 Skill 的全部分支时，请结合 [EveryLine CLI Skill 全量交互场景](everyline-cli-skill-interaction-scenarios.md)。

Skill 的安装位置和验证入口由 Agent 宿主决定，不要跨宿主复用目录：

| 宿主 | 安装入口 | 用户级位置或管理方式 |
| --- | --- | --- |
| Codex | 本地 Skill 目录 | `$HOME/.agents/skills/everyline-{shared,review,review-config}` |
| WorkBuddy | 「专家·技能·连接器」或本地 Skill 目录 | `$HOME/.workbuddy/skills/everyline-{shared,review,review-config}` |
| 豆包 | 本地 npm 同步，或技能管理中的 ZIP 导入 | 本地使用已存在的 `workspace/.user_skills`；自定义目录用 `EVERYLINE_DOUBAO_SKILLS_DIR` 指定；云端仍分别导入三个 `*-skill.zip` |

OpenAI 官方文档将 Skill 定义为包含 `SKILL.md` 及可选 references、scripts、assets 的目录。Codex 支持显式 `$skill-name` 调用和按 description 自动触发，并扫描用户级 `$HOME/.agents/skills`。详见 [OpenAI Build skills](https://learn.chatgpt.com/docs/build-skills)。WorkBuddy 使用自己的 `$HOME/.workbuddy/skills`，可参考 [WorkBuddy 技能系统说明](https://cloud.tencent.com/developer/article/2693324)；豆包电脑版当前通过界面上传本地 Skill，可参考 [豆包工作任务自定义 Skill 说明](https://www.beating.news/flash/362784)，最终入口以客户端实际展示为准。

## 1. 验证前准备

准备以下内容：

- 已安装准备验证的 Agent 宿主：Codex、WorkBuddy，或带有“工作任务”和“技能·连接器·伙伴”的豆包电脑版。
- Node.js 18 或更高版本。
- `everyline-cli` 的 npm 版本号，或待验证的 `.tgz` 发布包。
- 一个可访问 EveryLine 的 user 或 app 身份。
- 一份用于验证的 DOC、DOCX 或 PDF 合同；建议使用测试文件。

Codex 本地任务直接使用宿主文件路径和 loopback OAuth。WorkBuddy 使用本机 CLI、系统凭证库和 Device Grant。豆包普通工作任务在本地电脑和远端沙箱中都使用 Device Grant，并固定 `SESSION_ID` 与初始工作目录；远端沙箱使用 Linux CLI，以及宿主提供的原始附件字节流或完整下载 URL，用户 macOS 路径不作为沙箱文件路径。

CLI 和 Skill 应来自同一个发布版本。每次 Skill 发布（包括纯文案调整）都提升 `package.json` 统一版本、发布同版本 npm 包并更新远端 manifest，让 `version.updateRequired` 可以触发强制更新门禁。

## 2. 全局安装包同步 CLI、Codex、WorkBuddy 和豆包本地 Skills

全局安装 `everyline-cli` npm 包时，`postinstall` 会校验原生 CLI，并把三项 Skill 分别链接到 `$HOME/.agents/skills` 和 `$HOME/.workbuddy/skills`：

- 只有全局安装登记用户级 Skill；项目局部安装和 `npx` 临时执行不写入用户 Skill 目录。
- 已存在并指向同一 npm 包的 Skill 链接会直接复用，重复安装不会嵌套目录。
- 已存在普通目录或指向其他来源的同名链接时停止安装，不覆盖用户文件。
- 显式设置 `EVERYLINE_SKIP_SKILL_INSTALL=1` 时只安装 CLI。
- 只跳过 WorkBuddy 时设置 `EVERYLINE_SKIP_WORKBUDDY_SKILL_INSTALL=1`。
- 测试或自定义宿主可分别通过 `EVERYLINE_CODEX_SKILLS_DIR`、`EVERYLINE_WORKBUDDY_SKILLS_DIR` 改写根目录。
- 全新安装 Agent Skill 时，安装器会在 `$HOME/.everyline-cli/install-state.json` 写入不含凭证的授权门禁。旧 token 保留，但在完成一次新授权前不会被当作已登录，`review/checklist/rule` 也不会发起远端请求。
- 旧版本升级时，若状态文件尚不存在，但检测到指向本包的 `everyline-shared` 旧链接或已有 Skill 登记，则补建非首次安装状态并输出 `event=updated`，由 `auth status` 检查现有授权，不强制重新授权。已有状态文件中的待授权门禁继续保留。

新版 npm 可能要求显式批准依赖包的安装脚本，因此推荐使用以下命令。该批准只针对 `everyline-cli`，用于执行包内的 CLI 校验和 Skill 登记。`--foreground-scripts` 使安装完成文案和安装事件可见，避免 npm 在后台运行 `postinstall` 时隐藏成功输出。

### 从 npm 仓库安装

```bash
npm install -g --foreground-scripts --allow-scripts=@qfeius/everyline-cli @qfeius/everyline-cli@RELEASE_VERSION
```

### 从待发布的 tgz 安装

```bash
npm install -g --foreground-scripts --allow-scripts=@qfeius/everyline-cli /absolute/path/qfeius-everyline-cli-RELEASE_VERSION.tgz
```

Windows PowerShell 同样可以使用：

```powershell
npm install -g --foreground-scripts --allow-scripts=@qfeius/everyline-cli C:\absolute\path\qfeius-everyline-cli-RELEASE_VERSION.tgz
```

检查安装结果：

```bash
everyline-cli version --output json
everyline-cli --help
```

预期结果：命令可以执行，版本与待验证版本一致，stdout 返回 JSON 版本信息。首次安装时还应包含 `firstInstall=true`、`authorizationRequired=true` 和 `nextAction=authorize`，随后必须通过 `everyline-cli` Skill 完成一次新的 user 或 app 授权。

同时检查 Codex 与 WorkBuddy Skills：

```bash
for skill_name in everyline-cli everyline-review everyline-review-config; do
  test -f "$HOME/.agents/skills/$skill_name/SKILL.md"
  test -f "$HOME/.workbuddy/skills/$skill_name/SKILL.md"
done
```

## 3. 按宿主安装 Skill

npm 包内的三项 Skill 位于：

```text
<npm-global-root>/everyline-cli/skills/everyline-cli/
<npm-global-root>/everyline-cli/skills/everyline-review/
<npm-global-root>/everyline-cli/skills/everyline-review-config/
```

全局 npm 安装会自动登记 Codex、WorkBuddy，同步已发现或显式配置的豆包本地技能目录，并建立首次安装门禁。豆包云端独立 ZIP 的静态导入不执行 npm 安装脚本。三个宿主都在确认 CLI 可执行、三项 Skill 可读取或启用后，由 Agent 在回复正文展示首次安装提示；终端日志或 JSON 事件不代替面向用户的回复。安装过程不创建 Profile、不直接发起远端授权或业务请求，展示后等待用户确认开始授权，再选择身份、匹配或创建 Profile 并执行对应授权流程。

全局 npm 安装路径在本次安装任务完成时展示；豆包 ZIP 导入路径在首次运行 Skill 并确认当前任务中 CLI 可用时展示。豆包、WorkBuddy 界面导入或手工安装路径，按已确认的首次导入/安装上下文触发，不依赖全局 npm 状态文件；普通新会话不重复介绍，安装失败不展示完成文案。首次安装统一提示：

EveryLine CLI 已安装完成。目前支持合同审查，以及审查清单、规则和规则分组配置。使用前需要先完成账号授权，我现在可以为你打开授权页面或生成授权链接。

以下命令默认使用 npm 的标准全局目录。CLI 若通过 `npm install -g --prefix "$HOME/.local"` 安装，先在当前终端设置：

```bash
export EVERYLINE_NPM_PREFIX="$HOME/.local"
export EVERYLINE_NPM_ROOT="$(npm root -g --prefix "$EVERYLINE_NPM_PREFIX")"
```

`EVERYLINE_NPM_PREFIX` 表示执行安装、更新和卸载时必须复用的 npm prefix；`EVERYLINE_NPM_ROOT` 表示该 prefix 下的 package root。后续命令会优先使用 `EVERYLINE_NPM_ROOT`，从而取得同一份 npm 包中的 CLI 和 Skill。重新打开终端后，应先按原安装位置重新设置这两个变量。

### 3.1 Codex

三端统一按「主体 → 强度 → 清单」逐步等待回复。WorkBuddy 的审查选择在正文使用编号列表；主体和强度单选，清单完整展示所有候选并支持一次回复多个编号，不使用选项组件或对话分页。Codex 当前回合提供原生结构化选项工具时，立场方与审查强度使用互斥单选选项卡；三端清单均完整展示所有候选，使用稳定编号文字协议。

#### macOS 或 Linux

正常情况下全局安装已经创建三项链接。跳过生命周期脚本或需要手工修复时执行：

```bash
npm_global_root="${EVERYLINE_NPM_ROOT:-$(npm root -g)}"
mkdir -p "$HOME/.agents/skills"
for skill_name in everyline-cli everyline-review everyline-review-config; do
  skill_source="$npm_global_root/@qfeius/everyline-cli/skills/$skill_name"
  skill_target="$HOME/.agents/skills/$skill_name"
  test -f "$skill_source/SKILL.md"
  test ! -e "$skill_target"
  ln -s "$skill_source" "$skill_target"
done
```

检查会在源文件缺失或目标已存在时停止，避免嵌套目录和覆盖用户文件。

#### Windows PowerShell

正常安装使用目录联接指向 npm 包内 Skill，后续原地升级会自动使用新内容。需要手工修复时执行：

```powershell
$NpmRoot = (npm root -g).Trim()
$SkillTargetRoot = Join-Path $HOME ".agents\skills"
New-Item -ItemType Directory -Force -Path $SkillTargetRoot | Out-Null
foreach ($SkillName in @("everyline-cli", "everyline-review", "everyline-review-config")) {
    $SkillSource = Join-Path $NpmRoot "@qfeius\everyline-cli\skills\$SkillName"
    $SkillTarget = Join-Path $SkillTargetRoot $SkillName
    if (-not (Test-Path (Join-Path $SkillSource "SKILL.md"))) {
        throw "npm 包中缺少 $SkillName Skill"
    }
    if (Test-Path $SkillTarget) {
        throw "目标 Skill 已存在，请先确认现有版本：$SkillTarget"
    }
    New-Item -ItemType Junction -Path $SkillTarget -Target $SkillSource | Out-Null
}
```

CLI 通过自定义 prefix 安装时，先把第一行替换为以下两行，并使用原安装位置：

```powershell
$EverylineNpmPrefix = Join-Path $HOME ".local"
$NpmRoot = (npm root -g --prefix $EverylineNpmPrefix).Trim()
```

Windows 目录联接与 macOS/Linux 符号链接一样，会持续指向 npm 包内的同一路径。

#### 从源码目录验证

从源码验证时，把 `skills/everyline-cli/`、`skills/everyline-review/` 和 `skills/everyline-review-config/` 分别复制或链接到：

```text
$HOME/.agents/skills/<同名 Skill>/
```

每个目标目录下直接包含 `SKILL.md` 以及它自己的 `references/`。

### 3.2 WorkBuddy

WorkBuddy 使用自己的 Skill 目录。当前全局安装会同步登记；手工修复时执行：

macOS 或 Linux：

```bash
npm_global_root="${EVERYLINE_NPM_ROOT:-$(npm root -g)}"
mkdir -p "$HOME/.workbuddy/skills"
for skill_name in everyline-cli everyline-review everyline-review-config; do
  skill_source="$npm_global_root/@qfeius/everyline-cli/skills/$skill_name"
  skill_target="$HOME/.workbuddy/skills/$skill_name"
  test -f "$skill_source/SKILL.md"
  test ! -e "$skill_target"
  ln -s "$skill_source" "$skill_target"
done
```

也可以在 WorkBuddy 左侧打开「专家·技能·连接器」，使用当前版本提供的添加或导入入口安装完整 Skill。通过界面安装时，WorkBuddy 可能把技能保存为 `skill_<id>` 并登记内部元数据，不要再额外创建同名手工副本。

### 3.3 豆包电脑版

豆包工作支持本地普通技能文件夹。npm 全局安装时，macOS 自动发现已存在的 `~/Library/Application Support/DoubaoWork/Default/.doubaowork/agent_mode/workspace/.user_skills`，同步三项完整技能；安装器不替用户创建猜测的宿主路径。其他平台、不同用户配置或远端运行时，应使用当前豆包环境实际提供的目录：

```bash
EVERYLINE_DOUBAO_SKILLS_DIR="<豆包实际技能根目录的绝对路径>" npm install -g --foreground-scripts --allow-scripts=@qfeius/everyline-cli <发布包.tgz>
```

本地同步会比较内容而非只比较版本号，更新 `SKILL.md` 和全部 `references/`；旧目录保留在扫描目录外，出错时回滚本轮各宿主的变更。同名目录未声明对应 EveryLine 技能、含符号链接或特殊文件时，保留原目录并报告冲突。`EVERYLINE_SKIP_DOUBAO_SKILL_INSTALL=1` 跳过豆包，`EVERYLINE_SKIP_SKILL_INSTALL=1` 跳过全部宿主。

同步事件 `event=skills_updated / host=doubao / nextAction=reload_skills` 表示文件更新成功；Agent 应重新读取 `skills[].target` 下的三项 `SKILL.md`，或新建任务加载。历史对话不自动替换旧内容。云端导入副本尚未接入自动发布；该路径仍使用 ZIP。仓库执行 `make skill-assets` 后生成：

```text
dist/everyline-cli-skill.zip
dist/everyline-review-skill.zip
dist/everyline-review-config-skill.zip
```

每个 ZIP 根目录都直接包含自己的 `SKILL.md`；审查和配置 ZIP 还包含对应 `references/`。构建过程会清理旧的 `dist/everyline-shared-skill.zip`，避免重复导入公共授权入口。

在豆包中安装：

1. 打开豆包工作任务的「技能·连接器·伙伴 → 我的技能 → 上传技能」。
2. 依次上传三个 ZIP，确认解析名称分别为 `everyline-cli`、`everyline-review`、`everyline-review-config`。
3. 启用三项 Skill 并新建任务验证；ZIP 应作为 Skill 导入，不作为普通聊天附件总结。
4. 远端沙箱准备对应 Linux CLI，并按 `everyline-cli` 公共 Skill 提供 Device Grant 的会话变量；合同通过附件字节流、沙箱内路径或完整 URL 交付。
5. 首次验证时发送「刚首次导入 EveryLine 的三项 Skill，请验证 CLI 和 Skill 已就绪并展示安装完成提示，先不要发起授权」。CLI 尚未安装时在同一请求中补充安装要求；确认 CLI 可执行后，豆包应在回复正文展示上述统一文案，即使 `version` 没有首次安装字段。

豆包的 Skill 功能和入口仍在快速更新；本手册以客户端存在“上传技能”为前提。当前客户端只有“对话新建技能”时，应等待或启用本地 Skill 上传入口，避免把 `SKILL.md` 正文复制成缺少 references 的不完整技能。

## 4. 确认宿主已识别 Skill

### 4.1 Codex

重新打开 Codex 会话，执行：

```text
/skills
```

预期列表中出现 `everyline-cli`、`everyline-review` 和 `everyline-review-config`。

如果目录已经正确但列表尚未刷新，重启 Codex 后再检查。官方文档说明 Codex 支持自动检测 Skill 变化，未出现时可通过重启刷新。

先运行一个不上传合同的冒烟验证：

```text
$everyline-cli 使用当前 prod-user Profile 和 user 身份检查 CLI 版本和授权状态，先不要上传合同。
```

预期行为：Codex 检查 `everyline-cli` 路径、版本、两条任务命令的帮助信息和授权状态，解析当前 `prod-user` Profile，并在后续调用中固定传入该 Profile 与 user 身份；不要求填写 `businessId`、`fileHash` 等内部字段。

### 4.2 WorkBuddy

重新打开 WorkBuddy 任务，在「专家·技能·连接器」中确认三项 EveryLine Skill 已启用。先发送：

```text
使用 everyline-cli 技能，检查 CLI 路径、版本和当前授权状态，先不要上传合同。
```

预期 WorkBuddy 执行真实 CLI 命令并返回结构化检查结果，而不是只复述安装文档。WorkBuddy 中不需要前往 Codex 执行 `/skills`。

### 4.3 豆包电脑版

在豆包「我的技能」中确认三项 EveryLine Skill 已启用，再新建工作任务并发送：

```text
使用 everyline-cli 技能，执行 command -v everyline-cli 和 everyline-cli version --output json；再检查当前授权状态，先不要上传合同。
```

继续条件：豆包实际返回命令路径和 CLI JSON 版本，不只是描述应该执行哪些命令。找不到命令时，先让豆包本地任务环境包含 npm 全局 bin 或 `$HOME/.local/bin`，然后重新打开任务。

## 5. 配置身份与授权

所有 Agent 在发起授权前都先让用户选择身份。同次授权已有用户明确的 `user` 或 `app` 选择时直接复用；尚未明确时，WorkBuddy 使用 `AskUserQuestion` 单选并设置 `multiSelect=false`，Codex 在 `request_user_input` 可用时使用互斥单选，豆包在原生单选组件可用时使用该组件。没有原生组件时统一显示 `1. user（个人账号授权）` 和 `2. app（应用授权）`，等待用户回复 `1/2` 或 `user/app`。收到选择前不匹配或创建 Profile，也不执行身份相关的状态查询或授权命令。Profile 名称、默认身份、唯一候选、历史 token 和 CLI `nextAction` 都不作为用户选择的依据；笼统的“开始授权”或“继续授权”也不等于选择了身份。

身份明确后，用户本轮没有指定 Profile 或环境时，Codex、WorkBuddy 和豆包统一默认 test：user 复用或创建 `test-user`，app 复用 `test-app`，缺少时在取得非敏感 app ID 后创建。当前 dev、blue、prod Profile 不会被默认继承；用户本轮显式指定的 Profile 或环境优先。宿主差异只影响 user 的授权协议：Codex 本地使用 OAuth/PKCE，豆包和 WorkBuddy 使用 Device Grant。

user 流程取得 CLI 返回的完整授权 URL 后，优先生成宿主原生链接按钮，按钮文字固定为“点击授权”；没有链接按钮时显示 `[点击授权](<FULL_AUTHORIZATION_URL>)`。链接目标逐字保留 CLI 返回值及全部 query 参数，由用户主动点击，Agent 不自动打开浏览器，也不为改变展示方式创建第二笔授权事务。app 流程没有此链接，继续使用下方隐藏输入 app secret 的方式。

豆包与 WorkBuddy 在 `auth init` 返回 `status=pending` 时，使用 CLI 返回的 `verification_link_text`（固定为“点击授权”）作为链接文字；旧版 CLI 缺少字段时仍使用“点击授权”，不沿用历史会话、旧卡片或网页标题。已授权或终态结果不携带该展示字段。

### user 身份：推荐用于人工交互验证

默认 test Profile 可由 Agent 自动创建；手工等价命令为：

```bash
everyline-cli config add test-user \
  --env test \
  --default-identity user

everyline-cli config use test-user
```

Codex 本地交互可完成浏览器 OAuth 登录：

```bash
everyline-cli auth login \
  --profile test-user \
  --as user \
  --no-open-browser \
  --timeout 3m
```

保持该 CLI 进程运行，从输出中取出完整授权地址并展示：

```text
[点击授权](<FULL_AUTHORIZATION_URL>)
```

用户点击并完成浏览器授权后，由原 CLI 进程接收 callback；不要自动打开页面或重新执行 `auth login`。

检查结构化授权状态：

```bash
everyline-cli auth status \
  --profile test-user \
  --as user \
  --output json
```

只有输出中的 `authenticated=true` 表示授权完成。

豆包（含“本地电脑”模式）和 WorkBuddy 统一使用 Device Grant。豆包在首次 `auth status` 前固定 `SESSION_ID` 和初始工作目录；宿主未提供时只生成一次 UUID，所有命令显式注入相同值，并由执行工具设置相同工作目录。下面的豆包命令应带 `SESSION_ID=<same-session-id>` 前缀；AgentKit 仍使用平台工作区与注入密钥。完成这些准备后执行：

```bash
everyline-cli auth init --profile test-user --as user --output json
```

把 `verification_uri_complete` 从 `https://` 到最后一个 query 参数逐字保留为链接目标，并展示 `[点击授权](<verification_uri_complete>)`。用户主动点击并完成授权后只检查一次：

```bash
everyline-cli auth complete --profile test-user --as user --output json
```

WorkBuddy 在第一次 `auth init` 前固定一个非敏感的 `CODEBUDDY_SESSION_ID`，上面两条命令和随后的 `auth status` 都添加相同的 `CODEBUDDY_SESSION_ID=<same-session-id>` 前缀。若 `auth complete` 提示本地没有待完成事务，先恢复首次命令使用的原值并重试 `auth complete`；此时不要执行 `auth init --restart`。只有 CLI 明确返回 `denied`、`expired` 或 `invalid_grant`，并经用户同意后，才创建新的授权事务。

豆包的恢复流程相同：先恢复原 `SESSION_ID` 和初始工作目录，再检查已有事务。豆包本地电脑也不执行 `auth login --as user`，Device 状态查询、业务取 token 和刷新均只使用当前 Device 会话，不回退到本机旧 OAuth 缓存。

user Token 过期且未刷新成功后，保持原 Profile、user 身份和宿主会话，重新生成一次授权链接供用户手动登录：Codex 重新执行上面的 `auth login --no-open-browser` 并保持进程等待回调；豆包/WorkBuddy 执行一次 `auth init`，过期 Token 会生成新的完整 Device 链接。若已有待完成事务则复用；只有该事务明确过期、拒绝或失效时才执行一次 `auth init --restart`。用户已要求过期后重新生成链接时直接执行该策略，不重复确认意愿，也不复用历史链接中的授权码。

用户完成手动登录后，Device 流程执行一次 `auth complete`，再用 `auth status` 确认 `authenticated=true`，只恢复原业务步骤一次。进入到期前五分钟窗口后，缺少 refresh token 或刷新失败时，按 CLI 提示手动重新授权。

只有 `status=succeeded` 才继续。dev/test/prod 环境预设提供 `contract-review` metadata、回调地址和 `contract-review:full` scope。Codex 每次显式 `auth login --as user` 会读取 metadata 的 `registration_endpoint`，动态注册浏览器 client 并执行 OAuth Authorization Code + PKCE。豆包/WorkBuddy 的 `auth init` 只走 Device Grant：dev/test 使用独立 Device client `zscli_c77221e810ce3977`，不动态注册也不复用浏览器 client；prod 或自定义环境需显式配置平台确认的 Device client。metadata 未声明 `device_authorization_endpoint` 时由认证服务补齐对应业务的 Device Grant；远端沙箱不回退到 `auth login`。豆包 AgentKit 还需注入 base64 编码的 32 字节 `EVERYLINE_CLI_CREDENTIAL_KEY_V1`。

### app 身份：适合无浏览器设备

三个宿主统一按两阶段交互：先单独输入并固定非敏感的 app ID，再输入一次 app secret。当前消息和 Profile 都没有 app ID 时，Agent 只询问 app ID，不在同一轮索取 secret；Profile 创建或校验完成并确认 app 未授权后，才发起一笔登录事务。该事务只读取一次 secret，结束后只用 `auth status` 验证，不自动重跑登录。失败时保留 Profile 和 app ID，等待用户明确要求新的重试。

macOS 或 Linux：

```bash
export EVERYLINE_APP_ID='<APP_ID>'
export EVERYLINE_APP_SECRET='<APP_SECRET>'

everyline-cli config add test-app \
  --env test \
  --default-identity app \
  --app-id "$EVERYLINE_APP_ID"

everyline-cli config use test-app

printf '%s' "$EVERYLINE_APP_SECRET" | \
  everyline-cli auth login \
    --profile test-app \
    --as app \
    --app-secret-stdin \
    --output json
```

不要在 Agent 对话、命令参数、截图或日志中粘贴 app secret。测试完成后清理当前终端中的临时 secret 环境变量。

Agent 需要 app secret 时，只引导用户在自己的终端或平台密钥入口完成安全输入，不得在对话中索取 secret。user 与 app 凭据独立保存，发起 app 授权不先退出 user。app token 过期不等于凭据失效；服务端返回 `http=200 code=10003 msg=invalid param` 时只报告通用参数错误，CLI 未明确指出字段时不推断 app ID/secret 已变更或轮换，也不自动切换 Profile 或身份。

WorkBuddy app 登录不使用对话输入组件。Skill 展示一行已填入真实 Node `bin` 路径、Profile 和 app ID 的命令，由用户在自己的 WorkBuddy 终端执行：

```bash
export PATH=<WORKBUDDY_NODE_BIN>:$PATH && everyline-cli auth login --profile <profile> --as app --app-id <app-id> --app-secret-stdin
```

CLI 显示 `App secret:` 后关闭终端回显，用户输入并按回车即可；secret 不进入 shell 历史。用户回到对话确认完成后，Skill 通过 `auth status --profile <profile> --as app --output json` 验证，不要求粘贴登录输出。`<WORKBUDDY_NODE_BIN>` 必须来自当前 WorkBuddy 安装，例如 `/Users/<user>/.workbuddy/binaries/node/versions/<version>/bin`，不固定到某个用户或版本。

## 6. 检查 CLI 版本能力

执行：

```bash
everyline-cli review task start --help
everyline-cli review task result --help
```

目标版本应明确说明：

- `reviewStrength` 接受“弱势 / 中立 / 强势”。
- `selectedCheckListIds` 与 `matchContractTypeRulePackage=true` 可以组合执行。
- `review task result` 会等待任务终态并获取最终结果。

Skill 也会在上传合同前重复这项就绪检查。缺少任一能力时，它会停在上传前并提示升级 CLI，不会猜测字段映射。

## 7. 验证完整交互

宿主支持聊天附件时，先附加一份 DOC、DOCX 或 PDF 合同，再发送：

```text
$everyline-review 使用 prod-user Profile 和 user 身份审查这个附件；强度中立
```

WorkBuddy 或豆包未提供 `$skill-name` 显式语法时，选择已安装的 `everyline-review` 并发送“使用 EveryLine CLI 审查这个附件；强度中立”。

预期 Skill 直接采用这份合同，不再要求用户提供绝对路径。宿主没有暴露可读取附件时，也可以输入以下提示，并替换为验证设备可读取的绝对文件路径：

```text
$everyline-review 使用 prod-user Profile 和 user 身份审查 /absolute/path/采购合同.pdf
```

正常交互顺序如下：

1. Skill 解析并固定 `prod-user` Profile 与 user 身份，检查 CLI 版本、`review task start/result` 帮助和授权状态。
2. CLI 上传合同并返回内部文件身份。
3. 三端先提取主体，按真实 `name（role）` 展示候选并等待单选；主体确定后独立收集弱势、中立或强势。WorkBuddy 使用正文编号列表；Codex 在原生选项工具可用且候选符合容量时使用单选卡，豆包按宿主选项能力或编号协议选择。
4. 三端的主体和强度确定后，读取全部清单分页，完整展示 `0. 通用审查清单（系统内置）` 和从 1 连续编号的所有真实清单；不分页、不用选项组件，等待用户一次回复全部所选编号（逗号或空格分隔）。全部编号有效后直接进入任务校验。
5. 内置项与真实清单组成统一候选；即使超过 4 项也完整展示所有候选，不提供翻页或搜索导航。6 个真实清单加内置项时，完整展示编号 0、1、2、3、4、5、6。各宿主均复用提前明确且能唯一匹配的选择；编号选择中遇到无效输入时保留主体、强度和完整候选，重新提示编号格式，不只采用有效部分，不追加完成或开始确认。
6. 四项业务输入完整后，Skill 使用同一输入执行 dry-run；失败时不发送正式请求。
7. dry-run 通过后，Skill 使用同一 Profile 和身份自动发起一次任务并记录 task ID。
8. Skill 只查询该 task ID；等待成功后统一返回基础信息表、审查概览、横排的风险等级饼图与风险类别条形图，以及“查看详情”链接和两小时有效期提示，不展示 task ID、终态字段或其他服务端参数。

示例对话：

```text
用户：$everyline-review 使用 prod-user Profile 和 user 身份审查 /Users/me/Documents/采购合同.pdf

Agent：第 1 步，请选择审查主体，回复一个编号：
1. 示例采购有限公司（甲方）
2. 示例供应有限公司（乙方）

用户：1

Agent：第 2 步，请选择审查强度，回复一个编号：
1. 弱势
2. 中立
3. 强势

用户：2

Agent：第 3 步，以下为全部可用审查清单，请回复编号；多选请用逗号或空格分隔：
0. 通用审查清单（系统内置）
1. 实际查询到的自定义清单 A
2. 实际查询到的自定义清单 B
3. 实际查询到的自定义清单 C
4. 实际查询到的自定义清单 D
5. 实际查询到的自定义清单 E
6. 实际查询到的自定义清单 F

用户：0, 1

Agent：参数 dry-run 已通过，任务已创建，正在等待审查结果……

Agent：**基础信息**

| 合同文件名称 | 立场 | 强度 | 审查清单 |
| --- | --- | --- | --- |
| <实际文件名> | <实际立场> | <实际强度> | <全部已选清单名称> |

**审查概览**

共发现<总数>处风险，红线风险：<红线数>项、高风险：<高风险数>项、中风险：<中风险数>项，低风险：<低风险数>项。
问题主要集中在<基于真实结果简洁概括>。

<同一行展示风险等级分布饼图（数量及占比）和风险类别分布条形图（每类数量），不显示图表编号>

审查结果：[查看详情](<REVIEW_DETAIL_URL>)
有效期提示：审查结果详情链接默认有效期为两小时，请及时查看。
```

候选主体、清单名称和最终链接必须来自当前身份下的真实 CLI 响应。示例名称仅用于说明对话形式。

如果用户已经知道全部输入，可以在第一句话一次提供：

```text
$everyline-review 使用 prod-user Profile 和 user 身份审查 /absolute/path/采购合同.pdf；立场方是示例采购有限公司；使用内置规则包和自定义清单 A；强度中立。
```

信息能够唯一匹配时，Skill 会减少追问。

## 8. 验收标准

| 检查项 | 通过标准 |
| --- | --- |
| Skill 发现 | 三个宿主中均出现 `everyline-cli`、`everyline-review`、`everyline-review-config` |
| CLI 发现 | `everyline-cli version --output json` 成功，版本符合预期 |
| 调用上下文 | 所有授权和业务命令显式使用同一 `--profile/--as` |
| 授权 | `auth status` 返回 `authenticated=true` |
| 输入交互 | 用户只需提供合同附件、路径或 URL 形式的合同来源，以及立场方、清单和强度；单附件不再追问路径；WorkBuddy 按主体、强度、清单顺序使用正文编号列表，Codex 按当前工具能力使用选项卡 |
| 内部参数 | Skill 不向用户索要 `businessId/fileId/fileHash/selectedAuditRole/wait` |
| 候选数据 | 主体、清单、规则均来自实时 CLI 查询 |
| 清单展示 | 三端完整展示内置项与全部真实清单，使用同一稳定编号映射，等待用户回复一个或多个编号，不分页、不提供搜索导航 |
| 阶段隔离 | Codex、豆包和 WorkBuddy 统一按主体 → 强度 → 清单逐步等待回复；每次只收集一个维度 |
| Codex 选项卡 | 当前回合提供原生结构化选项工具时，立场方和强度使用互斥单选选项卡；清单完整展示所有候选，等待用户回复一个或多个编号，不做对话分页 |
| 主体映射 | 主体按 `name（role）` 展示；用户回复完整展示项或唯一名称时，所选候选的 `name` 写入 `selectedPosition`，同一候选的 `role` 写入 `selectedAuditRole` |
| 规则编号 | `0` 固定表示内置规则包，自定义清单从 `1` 开始，展示与解析使用同一映射 |
| 正式请求门 | 同一输入的 dry-run 成功后才发起真实任务 |
| 任务幂等 | 取得 task ID 后只查询该任务，不重复创建 |
| 最终结果 | 统一返回基础信息表、审查概览、横排双图，以及指向完整签名 `reviewDetailUrl` 的“查看详情”链接和有效期提示；不展示 task ID、终态字段或其他服务端参数 |
| CLI 兼容 | 原有命令、参数和 JSON 接口保持不变 |

## 9. 常见问题

### `/skills` 中缺少拆分后的 EveryLine Skill

- 检查 `$HOME/.agents/skills/everyline-cli/SKILL.md`、`everyline-review/SKILL.md` 和 `everyline-review-config/SKILL.md`。
- 检查是否误生成同名双层目录。
- 重启 Codex 后重新执行 `/skills`。

### WorkBuddy 提示已安装，但要求前往 Codex 验证

- 检查 Skill 是否实际位于 `$HOME/.workbuddy/skills/`，而不是只位于 `$HOME/.agents/skills/`。
- 在「专家·技能·连接器」中确认技能已启用。
- 新建 WorkBuddy 任务后直接执行冒烟验证，不使用 Codex 的 `/skills` 作为 WorkBuddy 验收结果。

### 豆包上传技能后只解释文档，没有执行 CLI

- 确认三个 ZIP 都通过上传入口安装成技能，而不是作为普通聊天附件。
- 让豆包实际执行 `command -v everyline-cli` 和 `everyline-cli version --output json`。
- 检查沙箱 Linux CLI、命令执行权限、Device Grant 会话变量和 PATH；通过后再上传合同。

### Codex 提示找不到 `everyline-cli`

- 在同一个用户和终端环境中执行 `everyline-cli version --output json`。
- 检查 npm 全局 bin 目录是否已经加入 `PATH`。
- Windows 可使用 `Get-Command everyline-cli` 查看实际命令路径。

### Skill 在上传前提示 CLI 版本不满足要求

解析 `version --output json`。`updateRequired=true` 时先记住 `updateCommand`，继续完成当前完整业务流程；CLI 同时会在 stderr 输出 `code=UPDATE_PENDING`、当前版本、最新版本、更新命令和 `updateAfter=current_business_workflow`。全部业务 API 结束并保留结果后原样执行一次更新命令，再次检查版本；下一条新业务重新加载新版 Skill。

CLI 与三项 Skill 更新并校验成功后展示“EveryLine CLI 已更新完成。目前支持合同审查，以及审查清单、规则和规则分组配置。”，复用同次授权中用户已选的身份；尚未选择时先单选 user/app，再对匹配的 Profile 和身份执行一次 `auth status`。只有 `authenticated=true` 时追加“当前已存在生效授权，可直接调用cli能力；”；`authenticated=false` 时追加“使用前需要先完成账号授权，我现在可以为你打开授权页面或生成授权链接。”，已有继续授权请求时直接按已选身份继续，否则等待用户确认。状态查询报错时保留未知状态并报告原始错误，不从 token 文件或安装成功推断授权有效。

### 提示尚未选择 Profile

执行 `everyline-cli config list` 检查配置，再通过 `everyline-cli config use PROFILE_NAME` 选中本次验证使用的 Profile。

### user OAuth 启动失败

Codex 本地登录检查 Profile 是否具有 OAuth metadata、business type 和 loopback redirect，metadata 是否声明可达的 `registration_endpoint`；每次显式登录都会替换历史浏览器 client ID。豆包/WorkBuddy 还要检查 `contract-review` metadata 的 `device_authorization_endpoint`、独立 Device client 和沙箱安全存储环境变量。记录 CLI 原始提示并联系平台配置负责人。

### 返回 `code=20000019` 或“租户应用不匹配”

该错误表示租户与 AppID 的组织应用或计费绑定校验失败。检查当前身份、Profile 中的 AppID、网关传入 AppID，以及租户应用绑定；重新登录通常不会改变该配置结果。

### 发起任务超时

- 已取得 task ID：继续查询该 task ID。
- 尚未取得 task ID：保留 request ID 和原始错误并停止，避免重复创建或重复扣点。

### 成功状态没有预览链接

Skill 仅在“审查结果”一项说明链接缺失，不自行拼接 `reviewDetailUrl`，也不向用户展开原始终态。

### 预览链接包含 token 参数

`reviewDetailUrl` 是后端签发的用户可访问免登录链接。Skill 把字段值作为不可拆分字符串，从 `https://` 到最后一个查询参数逐字写入“查看详情”的 Markdown 链接目标，保留完整 `token`；不解析、脱敏、重新编码或改写。URL 中的参数不在链接之外单独展示，最终回复遵循基础信息表、审查概览、横排双图和末尾两行的统一格式。

## 10. 更新与卸载

### 两种更新方式

**按客户提供的 tgz 更新**（路径和版本替换为实际文件）：

```bash
npm install -g --foreground-scripts --allow-scripts=@qfeius/everyline-cli ./qfeius-everyline-cli-<版本>.tgz
everyline-cli version --output json
```

也可以将 tgz 作为工作任务附件提供，然后告诉 Agent：“使用 everyline-cli 技能，按附件 tgz 更新 CLI 和三项 Skill，不从 npm 查询最新版。”即使包版本相同，也按指定包安装并同步可管理的 Skill 内容；无需该包先发布到 npm。

**从 npm 更新到最新版**：

```bash
npm install -g --foreground-scripts --allow-scripts=@qfeius/everyline-cli @qfeius/everyline-cli@latest --registry https://registry.npmjs.org
everyline-cli version --output json
```

或告诉 Agent：“使用 everyline-cli 技能，从 npm 更新 CLI 和三项 Skill。”此方式要求对应版本已经发布到 npm。两种方式都保留原安装 prefix 和账号配置，并在更新后核对 CLI 与三项 Skill 版本；自定义 prefix 复用下文命令。

Codex、WorkBuddy 和已发现的豆包本地技能目录随 npm 安装同步，更新后新建任务加载。豆包云端手动导入版需重新导入三项 Skill ZIP；客户端提供的附件路径必须能被执行环境读取。


使用 npm 标准全局目录时更新 CLI：

```bash
npm install -g --allow-scripts=@qfeius/everyline-cli @qfeius/everyline-cli@NEW_RELEASE_VERSION
```

使用自定义 prefix 时，必须复用安装时的原始 prefix：

```bash
export EVERYLINE_NPM_PREFIX="$HOME/.local"
export EVERYLINE_NPM_ROOT="$(npm root -g --prefix "$EVERYLINE_NPM_PREFIX")"
npm install -g --allow-scripts=@qfeius/everyline-cli --prefix "$EVERYLINE_NPM_PREFIX" @qfeius/everyline-cli@NEW_RELEASE_VERSION
```

Windows PowerShell 使用自定义 prefix 时：

```powershell
$EverylineNpmPrefix = Join-Path $HOME ".local"
npm install -g --allow-scripts=@qfeius/everyline-cli --prefix $EverylineNpmPrefix @qfeius/everyline-cli@NEW_RELEASE_VERSION
```

macOS/Linux 使用符号链接、Windows 使用目录联接时，Codex 与 WorkBuddy 的三项 Skill 都指向新 npm 包中的同名目录。已发现或显式配置的豆包本地目录同步完整文件夹，三项技能的引用文件也会更新。云端导入版仍通过 `make skill-assets` 生成并上传三个新 ZIP。

豆包更新后应区分“本地文件已同步”和“当前任务已重新加载”。以 `skills_updated` 事件的目标路径核对三项实际文件，再重新读取或新建任务；只查看 npm 包内文件或 CLI 版本不算验证加载成功。未发现豆包目录、云端导入版或内容尚未核实时，应报告仍待更新/验证，使用实际目录重装或通过三个独立 ZIP 更新，外层完整发布 ZIP 和 npm `.tgz` 不作为豆包 Skill 导入包。

### 只从某一个宿主移除 Skill

只移除某个宿主的 Skill 时，不卸载共享的 npm 包或 CLI。卸载前先确认目标路径确实是本 Skill。

Codex 的 macOS/Linux 符号链接：

```bash
for skill_name in everyline-cli everyline-review everyline-review-config; do
  unlink "$HOME/.agents/skills/$skill_name"
done
```

WorkBuddy 的符号链接卸载：

```bash
for skill_name in everyline-cli everyline-review everyline-review-config; do
  unlink "$HOME/.workbuddy/skills/$skill_name"
done
```

豆包在「我的技能」中分别移除三项 Skill；不手工删除豆包客户端内部数据目录，也不因此卸载其他宿主仍在使用的 CLI。

Windows 目录联接在确认路径后，分别对两个宿主下的 `everyline-cli`、`everyline-review`、`everyline-review-config` 执行 `Remove-Item`。WorkBuddy 通过界面安装时，也只在其「专家·技能·连接器」中移除对应技能。

### 全局卸载 CLI 和 npm 包

确认 Codex、WorkBuddy 和豆包都不再使用 `everyline-cli`，并已清理各自的 Skill 登记后，才全局卸载 npm 包。

标准全局目录：

```bash
npm uninstall -g @qfeius/everyline-cli
```

macOS/Linux 使用自定义 prefix 时复用原安装位置：

```bash
export EVERYLINE_NPM_PREFIX="$HOME/.local"
npm uninstall -g --prefix "$EVERYLINE_NPM_PREFIX" @qfeius/everyline-cli
```

Windows PowerShell 使用自定义 prefix 时：

```powershell
$EverylineNpmPrefix = Join-Path $HOME ".local"
npm uninstall -g --prefix $EverylineNpmPrefix @qfeius/everyline-cli
```
