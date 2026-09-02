# EveryLine CLI 交互 Skill 安装与验证（Codex / WorkBuddy / 豆包）

本文用于在一台全新设备上安装 `everyline-cli` 及配套交互 Skill，并验证合同审查的多轮对话流程。Skill 负责对话和流程编排，CLI 继续负责授权、文件上传、主体提取、清单查询、任务发起和结果查询；全局安装 npm 包默认同时登记 Codex Skill，但不会改变 CLI 的命令、参数或 JSON 接口。

需要逐项评审或测试当前 Skill 的全部分支时，请结合 [EveryLine CLI Skill 全量交互场景](everyline-cli-skill-interaction-scenarios.md)。

Skill 的安装位置和验证入口由 Agent 宿主决定，不要跨宿主复用目录：

| 宿主 | 安装入口 | 用户级位置或管理方式 |
| --- | --- | --- |
| Codex | 本地 Skill 目录 | `$HOME/.agents/skills/everyline-cli` |
| WorkBuddy | 「专家·技能·连接器」或本地 Skill 目录 | `$HOME/.workbuddy/skills/everyline-cli` |
| 豆包电脑版 | 「工作任务 → 技能·连接器·伙伴 → 上传技能」 | 由豆包管理，不手工猜测内部目录 |

OpenAI 官方文档将 Skill 定义为包含 `SKILL.md` 及可选 references、scripts、assets 的目录。Codex 支持显式 `$skill-name` 调用和按 description 自动触发，并扫描用户级 `$HOME/.agents/skills`。详见 [OpenAI Build skills](https://learn.chatgpt.com/docs/build-skills)。WorkBuddy 使用自己的 `$HOME/.workbuddy/skills`，可参考 [WorkBuddy 技能系统说明](https://cloud.tencent.com/developer/article/2693324)；豆包电脑版当前通过界面上传本地 Skill，可参考 [豆包工作任务自定义 Skill 说明](https://www.beating.news/flash/362784)，最终入口以客户端实际展示为准。

## 1. 验证前准备

准备以下内容：

- 已安装准备验证的 Agent 宿主：Codex、WorkBuddy，或带有“工作任务”和“技能·连接器·伙伴”的豆包电脑版。
- Node.js 18 或更高版本。
- `everyline-cli` 的 npm 版本号，或待验证的 `.tgz` 发布包。
- 一个可访问 EveryLine 的 user 或 app 身份。
- 一份用于验证的 DOC、DOCX 或 PDF 合同；建议使用测试文件。

WorkBuddy 和豆包必须在安装 CLI 的同一台电脑、同一系统用户下执行本地任务。豆包选择“本地电脑”模式，并按客户端提示授予读取合同文件和执行本地命令所需的权限；云电脑不复用本机的 CLI、Profile 或授权缓存。

CLI 和 Skill 应来自同一个发布版本，避免 Skill 使用了新流程而 CLI 仍是旧命令。

## 2. 全局安装包默认同时安装 CLI 和 Codex Skill

全局安装 `everyline-cli` npm 包时，`postinstall` 会校验原生 CLI，并将包内 Skill 链接到 `$HOME/.agents/skills/everyline-cli`：

- 只有全局安装登记 Codex Skill；项目局部安装和 `npx` 临时执行不会写入用户 Skill 目录。
- 已存在并指向同一 npm 包的 Skill 链接会直接复用，重复安装不会嵌套目录。
- 已存在普通目录或指向其他来源的同名链接时停止安装，不覆盖用户文件。
- 显式设置 `EVERYLINE_SKIP_SKILL_INSTALL=1` 时只安装 CLI。
- 测试或自定义宿主可以通过 `EVERYLINE_CODEX_SKILLS_DIR` 改写 Skill 根目录。

新版 npm 可能要求显式批准依赖包的安装脚本，因此推荐使用以下命令。该批准只针对 `everyline-cli`，用于执行包内的 CLI 校验和 Skill 登记。

### 从 npm 仓库安装

```bash
npm install -g --allow-scripts=everyline-cli everyline-cli@RELEASE_VERSION
```

### 从待发布的 tgz 安装

```bash
npm install -g --allow-scripts=everyline-cli /absolute/path/everyline-cli-RELEASE_VERSION.tgz
```

Windows PowerShell 同样可以使用：

```powershell
npm install -g --allow-scripts=everyline-cli C:\absolute\path\everyline-cli-RELEASE_VERSION.tgz
```

检查安装结果：

```bash
everyline-cli version --output json
everyline-cli --help
```

预期结果：命令可以执行，版本与待验证版本一致，stdout 返回 JSON 版本信息。

同时检查 Codex Skill：

```bash
test -f "$HOME/.agents/skills/everyline-cli/SKILL.md"
```

## 3. 按宿主安装 Skill

npm 包内的 Skill 位于：

```text
<npm-global-root>/everyline-cli/skills/everyline-cli/
```

全局 npm 安装会自动登记 Codex Skill。WorkBuddy 和豆包仍需要按本节使用各自的目录或界面导入；安装过程不会创建 Profile、发起授权或执行远端业务请求。

以下命令默认使用 npm 的标准全局目录。CLI 若通过 `npm install -g --prefix "$HOME/.local"` 安装，先在当前终端设置：

```bash
export EVERYLINE_NPM_PREFIX="$HOME/.local"
export EVERYLINE_NPM_ROOT="$(npm root -g --prefix "$EVERYLINE_NPM_PREFIX")"
```

`EVERYLINE_NPM_PREFIX` 表示执行安装、更新和卸载时必须复用的 npm prefix；`EVERYLINE_NPM_ROOT` 表示该 prefix 下的 package root。后续命令会优先使用 `EVERYLINE_NPM_ROOT`，从而取得同一份 npm 包中的 CLI 和 Skill。重新打开终端后，应先按原安装位置重新设置这两个变量。

### 3.1 Codex

#### macOS 或 Linux

正常情况下上一节的全局安装已经创建符号链接。若使用旧版 npm、跳过了生命周期脚本或需要手工修复，可执行等价命令：

```bash
npm_global_root="${EVERYLINE_NPM_ROOT:-$(npm root -g)}"
skill_source="$npm_global_root/everyline-cli/skills/everyline-cli"
skill_target="$HOME/.agents/skills/everyline-cli"

test -f "$skill_source/SKILL.md"
mkdir -p "$HOME/.agents/skills"
test ! -e "$skill_target"
ln -s "$skill_source" "$skill_target"
```

最后两个 `test` 会在源文件缺失或目标已经存在时停止，避免生成错误的嵌套目录或覆盖已有 Skill。

#### Windows PowerShell

正常安装使用目录联接指向 npm 包内 Skill，后续原地升级会自动使用新内容。需要手工修复时执行：

```powershell
$NpmRoot = (npm root -g).Trim()
$SkillSource = Join-Path $NpmRoot "everyline-cli\skills\everyline-cli"
$SkillTargetRoot = Join-Path $HOME ".agents\skills"
$SkillTarget = Join-Path $SkillTargetRoot "everyline-cli"

if (-not (Test-Path (Join-Path $SkillSource "SKILL.md"))) {
    throw "npm 包中缺少 everyline-cli Skill"
}
if (Test-Path $SkillTarget) {
    throw "目标 Skill 已存在，请先确认现有版本"
}

New-Item -ItemType Directory -Force -Path $SkillTargetRoot | Out-Null
New-Item -ItemType Junction -Path $SkillTarget -Target $SkillSource | Out-Null
```

CLI 通过自定义 prefix 安装时，先把第一行替换为以下两行，并使用原安装位置：

```powershell
$EverylineNpmPrefix = Join-Path $HOME ".local"
$NpmRoot = (npm root -g --prefix $EverylineNpmPrefix).Trim()
```

Windows 目录联接与 macOS/Linux 符号链接一样，会持续指向 npm 包内的同一路径。

#### 从源码目录验证

如果验证设备可以访问源码，也可以把仓库中的 `skills/everyline-cli/` 复制或链接到：

```text
$HOME/.agents/skills/everyline-cli/
```

目标目录下应直接包含 `SKILL.md` 和 `references/`，不要再多嵌套一层 `everyline-cli/`。

### 3.2 WorkBuddy

WorkBuddy 使用自己的 Skill 目录；只安装到 `$HOME/.agents/skills` 属于 Codex 安装，不代表 WorkBuddy 已识别。

macOS 或 Linux：

```bash
npm_global_root="${EVERYLINE_NPM_ROOT:-$(npm root -g)}"
skill_source="$npm_global_root/everyline-cli/skills/everyline-cli"
skill_target="$HOME/.workbuddy/skills/everyline-cli"

test -f "$skill_source/SKILL.md"
mkdir -p "$HOME/.workbuddy/skills"
test ! -e "$skill_target"
ln -s "$skill_source" "$skill_target"
```

也可以在 WorkBuddy 左侧打开「专家·技能·连接器」，使用当前版本提供的添加或导入入口安装完整 Skill。通过界面安装时，WorkBuddy 可能把技能保存为 `skill_<id>` 并登记内部元数据，不要再额外创建同名手工副本。

### 3.3 豆包电脑版

豆包电脑版的“工作任务”支持上传本地 Skill，但不使用 Codex 或 WorkBuddy 的目录。先把 npm 包内 Skill 制作成 ZIP；ZIP 根目录必须直接包含 `SKILL.md`，不能再包一层 `everyline-cli/`。

macOS 或 Linux：

```bash
npm_global_root="${EVERYLINE_NPM_ROOT:-$(npm root -g)}"
skill_source="$npm_global_root/everyline-cli/skills/everyline-cli"
skill_archive="$HOME/Downloads/everyline-cli-skill.zip"

test -f "$skill_source/SKILL.md"
test ! -e "$skill_archive"
(
  cd "$skill_source"
  zip -qr "$skill_archive" SKILL.md references
)
unzip -l "$skill_archive"
```

Windows PowerShell：

```powershell
$NpmRoot = (npm root -g).Trim()
$SkillSource = Join-Path $NpmRoot "everyline-cli\skills\everyline-cli"
$SkillArchive = Join-Path $HOME "Downloads\everyline-cli-skill.zip"

if (-not (Test-Path (Join-Path $SkillSource "SKILL.md"))) {
    throw "npm 包中缺少 everyline-cli Skill"
}
if (Test-Path $SkillArchive) {
    throw "目标 ZIP 已存在，请先确认现有文件"
}

Compress-Archive `
    -Path (Join-Path $SkillSource "SKILL.md"), (Join-Path $SkillSource "references") `
    -DestinationPath $SkillArchive
```

在豆包中安装：

1. 更新并打开豆包电脑版，进入「工作任务」。
2. 选择「本地电脑」执行环境。
3. 打开侧边栏「技能·连接器·伙伴」。
4. 进入「我的技能」，点击「+ 新建」或当前版本对应入口，选择「上传技能」。
5. 上传 `everyline-cli-skill.zip`，检查解析出的名称为 `everyline-cli`，并启用该技能。
6. 新建一个本地工作任务进行验证；不要把 ZIP 当成普通聊天附件只做内容总结。

豆包的 Skill 功能和入口仍在快速更新；本手册以客户端存在“上传技能”为前提。当前客户端只有“对话新建技能”时，应等待或启用本地 Skill 上传入口，避免把 `SKILL.md` 正文复制成缺少 references 的不完整技能。

## 4. 确认宿主已识别 Skill

### 4.1 Codex

重新打开 Codex 会话，执行：

```text
/skills
```

预期列表中出现 `everyline-cli`。也可以输入 `$` 后搜索 `everyline-cli`。

如果目录已经正确但列表尚未刷新，重启 Codex 后再检查。官方文档说明 Codex 支持自动检测 Skill 变化，未出现时可通过重启刷新。

先运行一个不上传合同的冒烟验证：

```text
$everyline-cli 使用当前 prod-user Profile 和 user 身份检查 CLI 版本、审查能力和授权状态，先不要上传合同。
```

预期行为：Codex 检查 `everyline-cli` 路径、版本、两条任务命令的帮助信息和授权状态，解析当前 `prod-user` Profile，并在后续调用中固定传入该 Profile 与 user 身份；不要求填写 `businessId`、`fileHash` 等内部字段。

### 4.2 WorkBuddy

重新打开 WorkBuddy 任务，在「专家·技能·连接器」中确认 `everyline-cli` 已启用。先发送：

```text
使用 everyline-cli 技能，检查 CLI 路径、版本、审查能力和当前授权状态，先不要上传合同。
```

预期 WorkBuddy 执行真实 CLI 命令并返回结构化检查结果，而不是只复述安装文档。WorkBuddy 中不需要前往 Codex 执行 `/skills`。

### 4.3 豆包电脑版

在豆包「我的技能」中确认 `everyline-cli` 已启用，再新建“本地电脑”工作任务并发送：

```text
使用 everyline-cli 技能，执行 command -v everyline-cli 和 everyline-cli version --output json；再检查审查能力和当前授权状态，先不要上传合同。
```

继续条件：豆包实际返回命令路径和 CLI JSON 版本，不只是描述应该执行哪些命令。找不到命令时，先让豆包本地任务环境包含 npm 全局 bin 或 `$HOME/.local/bin`，然后重新打开任务。

## 5. 配置身份与授权

### user 身份：推荐用于人工交互验证

创建并选中 prod Profile：

```bash
everyline-cli config add prod-user \
  --env prod \
  --default-identity user

everyline-cli config use prod-user
```

完成浏览器 OAuth 登录：

```bash
everyline-cli auth login \
  --profile prod-user \
  --as user \
  --timeout 3m
```

检查结构化授权状态：

```bash
everyline-cli auth status \
  --profile prod-user \
  --as user \
  --output json
```

只有输出中的 `authenticated=true` 表示授权完成。如果发布包中的 prod 预设尚未包含正式 OAuth metadata、client ID 和 loopback redirect，应先由平台补齐配置，再继续 user 流程。

### app 身份：适合无浏览器设备

macOS 或 Linux：

```bash
export EVERYLINE_APP_ID='<APP_ID>'
export EVERYLINE_APP_SECRET='<APP_SECRET>'

everyline-cli config add prod-app \
  --env prod \
  --default-identity app \
  --app-id "$EVERYLINE_APP_ID"

everyline-cli config use prod-app

printf '%s' "$EVERYLINE_APP_SECRET" | \
  everyline-cli auth login \
    --profile prod-app \
    --as app \
    --app-secret-stdin \
    --output json
```

不要在 Agent 对话、命令参数、截图或日志中粘贴 app secret。测试完成后清理当前终端中的临时 secret 环境变量。

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
$everyline-cli 使用 prod-user Profile 和 user 身份审查这个附件；强度中立
```

WorkBuddy 或豆包未提供 `$skill-name` 显式语法时，选择已安装的 `everyline-cli` 技能并发送“使用 EveryLine CLI 审查这个附件；强度中立”即可。

预期 Skill 直接采用这份合同，不再要求用户提供绝对路径。宿主没有暴露可读取附件时，也可以输入以下提示，并替换为验证设备可读取的绝对文件路径：

```text
$everyline-cli 使用 prod-user Profile 和 user 身份审查 /absolute/path/采购合同.pdf
```

正常交互顺序如下：

1. Skill 解析并固定 `prod-user` Profile 与 user 身份，检查 CLI 版本、`review task start/result` 帮助和授权状态。
2. CLI 上传合同并返回内部文件身份。
3. Skill 查询并保存全部真实清单；每页展示 4 个真实清单，跨页累计选择，直到用户选择“完成选择”。
4. 清单确定后，CLI 提取合同主体；Skill 在下一次独立交互中展示真实主体名称，让用户选择审查立场方。
5. Skill 询问审查强度：弱势、中立或强势。
6. 四项业务输入完整后，Skill 使用同一输入执行 dry-run；失败时不发送正式请求。
7. dry-run 通过后，Skill 使用同一 Profile 和身份自动发起一次任务并记录 task ID。
8. Skill 只查询该 task ID，等待终态后完整原样返回 `reviewDetailUrl`（服务端提供时），并提示免登录链接默认两小时有效。

示例对话：

```text
用户：$everyline-cli 使用 prod-user Profile 和 user 身份审查 /Users/me/Documents/采购合同.pdf

Agent：请选择审查规则来源。第 1/1 页，已选择 0 项：
0. 按合同类型自动匹配内置规则包
1. 实际查询到的自定义清单 A
2. 实际查询到的自定义清单 B
操作：完成选择 / 按名称搜索

用户：选择 0 和 1，完成选择

Agent：检测到以下合同主体，请选择审查立场方：
1. 示例采购有限公司（甲方）
2. 示例供应有限公司（乙方）

用户：选择 1

Agent：请选择审查强度：弱势、中立、强势。

用户：中立

Agent：参数 dry-run 已通过，任务已创建，正在等待审查结果……
```

候选主体、清单名称和最终链接必须来自当前身份下的真实 CLI 响应。示例名称仅用于说明对话形式。

如果用户已经知道全部输入，可以在第一句话一次提供：

```text
$everyline-cli 使用 prod-user Profile 和 user 身份审查 /absolute/path/采购合同.pdf；立场方是示例采购有限公司；使用内置规则包和自定义清单 A；强度中立。
```

信息能够唯一匹配时，Skill 会减少追问。

## 8. 验收标准

| 检查项 | 通过标准 |
| --- | --- |
| Skill 发现 | Codex 的 `/skills`、WorkBuddy 的「专家·技能·连接器」或豆包的「我的技能」中出现 `everyline-cli` |
| CLI 发现 | `everyline-cli version --output json` 成功，版本符合预期 |
| 调用上下文 | 所有授权和业务命令显式使用同一 `--profile/--as` |
| 授权 | `auth status` 返回 `authenticated=true` |
| 输入交互 | 用户只需提供合同附件、路径或 URL 形式的合同来源，以及立场方、清单和强度；单附件不再追问路径 |
| 内部参数 | Skill 不向用户索要 `businessId/fileId/fileHash/selectedAuditRole/wait` |
| 候选数据 | 主体、清单、规则均来自实时 CLI 查询 |
| 清单分页 | 先读取全部候选，每页展示 4 个真实清单；稳定全局编号、跨页累计、全局名称搜索均可用 |
| 阶段隔离 | 先用独立交互完成清单选择，再用下一次独立交互选择立场方；同一选择卡片不混合两类问题 |
| 主体映射 | 主体按 `name（role）` 展示；用户回复完整展示项或唯一名称时，所选候选的 `name` 写入 `selectedPosition`，同一候选的 `role` 写入 `selectedAuditRole` |
| 规则编号 | `0` 固定表示内置规则包，自定义清单从 `1` 开始，展示与解析使用同一映射 |
| 正式请求门 | 同一输入的 dry-run 成功后才发起真实任务 |
| 任务幂等 | 取得 task ID 后只查询该任务，不重复创建 |
| 最终结果 | 返回真实终态；服务端提供链接时完整原样返回签名 `reviewDetailUrl`，不脱敏或重新拼接 |
| CLI 兼容 | 原有命令、参数和 JSON 接口保持不变 |

## 9. 常见问题

### `/skills` 中没有 `everyline-cli`

- 检查 `$HOME/.agents/skills/everyline-cli/SKILL.md` 是否直接存在。
- 检查是否误生成了 `everyline-cli/everyline-cli/SKILL.md` 双层目录。
- 重启 Codex 后重新执行 `/skills`。

### WorkBuddy 提示已安装，但要求前往 Codex 验证

- 检查 Skill 是否实际位于 `$HOME/.workbuddy/skills/`，而不是只位于 `$HOME/.agents/skills/`。
- 在「专家·技能·连接器」中确认技能已启用。
- 新建 WorkBuddy 任务后直接执行冒烟验证，不使用 Codex 的 `/skills` 作为 WorkBuddy 验收结果。

### 豆包上传技能后只解释文档，没有执行 CLI

- 确认当前任务使用“本地电脑”，不是云电脑。
- 确认上传入口将 ZIP 安装成技能，而不是把 ZIP 作为普通聊天附件。
- 让豆包实际执行 `command -v everyline-cli` 和 `everyline-cli version --output json`。
- 检查本地任务的命令执行、文件读取权限和 PATH；三项通过后再上传合同。

### Codex 提示找不到 `everyline-cli`

- 在同一个用户和终端环境中执行 `everyline-cli version --output json`。
- 检查 npm 全局 bin 目录是否已经加入 `PATH`。
- Windows 可使用 `Get-Command everyline-cli` 查看实际命令路径。

### Skill 在上传前提示 CLI 版本不满足要求

安装与 Skill 同版本的最新 CLI 发布包，再重新开始验证。该提示属于版本就绪门，不是合同或授权失败。

### 提示尚未选择 Profile

执行 `everyline-cli config list` 检查配置，再通过 `everyline-cli config use PROFILE_NAME` 选中本次验证使用的 Profile。

### user OAuth 启动失败

检查当前发布包的 prod Profile 是否具有平台确认过的 OAuth metadata、client ID 和 loopback redirect。相关配置缺失时记录 CLI 原始提示并联系平台配置负责人。

### 返回 `code=20000019` 或“租户应用不匹配”

该错误表示租户与 AppID 的组织应用或计费绑定校验失败。检查当前身份、Profile 中的 AppID、网关传入 AppID，以及租户应用绑定；重新登录通常不会改变该配置结果。

### 发起任务超时

- 已取得 task ID：继续查询该 task ID。
- 尚未取得 task ID：保留 request ID 和原始错误并停止，避免重复创建或重复扣点。

### 成功状态没有预览链接

记录任务成功及原始结果。Skill 不会自行拼接 `reviewDetailUrl`。

### 预览链接包含 token 参数

`reviewDetailUrl` 是后端签发的用户可访问免登录链接。Skill 完整原样展示该链接，不解析、脱敏、改写或单独输出其中的 token，并提示链接默认两小时有效。

## 10. 更新与卸载

使用 npm 标准全局目录时更新 CLI：

```bash
npm install -g --allow-scripts=everyline-cli everyline-cli@NEW_RELEASE_VERSION
```

使用自定义 prefix 时，必须复用安装时的原始 prefix：

```bash
export EVERYLINE_NPM_PREFIX="$HOME/.local"
export EVERYLINE_NPM_ROOT="$(npm root -g --prefix "$EVERYLINE_NPM_PREFIX")"
npm install -g --allow-scripts=everyline-cli --prefix "$EVERYLINE_NPM_PREFIX" everyline-cli@NEW_RELEASE_VERSION
```

Windows PowerShell 使用自定义 prefix 时：

```powershell
$EverylineNpmPrefix = Join-Path $HOME ".local"
npm install -g --allow-scripts=everyline-cli --prefix $EverylineNpmPrefix everyline-cli@NEW_RELEASE_VERSION
```

macOS/Linux 使用符号链接、Windows 使用目录联接时，Skill 都会指向新 npm 包中的同名目录。

WorkBuddy 使用符号链接安装时，同样由 npm 包更新 Skill 内容；其目标路径是 `$HOME/.workbuddy/skills/everyline-cli`。豆包使用上传 ZIP 安装，更新 npm 包后需要重新生成 ZIP，并在豆包「我的技能」中更新或重新上传。

### 只从某一个宿主移除 Skill

只移除某个宿主的 Skill 时，不卸载共享的 npm 包或 CLI。卸载前先确认目标路径确实是本 Skill。

Codex 的 macOS/Linux 符号链接：

```bash
unlink "$HOME/.agents/skills/everyline-cli"
```

WorkBuddy 的符号链接卸载：

```bash
unlink "$HOME/.workbuddy/skills/everyline-cli"
```

豆包在「我的技能」中移除 `everyline-cli`；不要手工删除豆包客户端的内部数据目录，也不要因此卸载其他宿主仍在使用的 CLI。

Windows 目录联接可以在确认路径后使用 `Remove-Item "$HOME\.agents\skills\everyline-cli"` 删除。WorkBuddy 通过界面安装时，也只在其「专家·技能·连接器」中移除对应技能。

### 全局卸载 CLI 和 npm 包

确认 Codex、WorkBuddy 和豆包都不再使用 `everyline-cli`，并已清理各自的 Skill 登记后，才全局卸载 npm 包。

标准全局目录：

```bash
npm uninstall -g everyline-cli
```

macOS/Linux 使用自定义 prefix 时复用原安装位置：

```bash
export EVERYLINE_NPM_PREFIX="$HOME/.local"
npm uninstall -g --prefix "$EVERYLINE_NPM_PREFIX" everyline-cli
```

Windows PowerShell 使用自定义 prefix 时：

```powershell
$EverylineNpmPrefix = Join-Path $HOME ".local"
npm uninstall -g --prefix $EverylineNpmPrefix everyline-cli
```
