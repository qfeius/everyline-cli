# EveryLine CLI 交互 Skill 安装与验证（Codex / WorkBuddy / 豆包）

本文用于在一台全新设备上安装 `everyline-cli` 及配套交互 Skill，并验证合同审查的多轮对话流程。Skill 负责对话和流程编排，CLI 继续负责授权、文件上传、主体提取、清单查询、任务发起和结果查询；安装 Skill 不会改变 CLI 的命令、参数或 JSON 接口。

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
- 一个可访问智审开放平台的 user 或 app 身份。
- 一份用于验证的 DOC、DOCX 或 PDF 合同；建议使用测试文件。

WorkBuddy 和豆包必须在安装 CLI 的同一台电脑、同一系统用户下执行本地任务。豆包选择“本地电脑”模式，并按客户端提示授予读取合同文件和执行本地命令所需的权限；云电脑不复用本机的 CLI、Profile 或授权缓存。

CLI 和 Skill 应来自同一个发布版本，避免 Skill 使用了新流程而 CLI 仍是旧命令。

## 2. 安装 CLI

### 从 npm 仓库安装

```bash
npm install -g everyline-cli@RELEASE_VERSION
```

### 从待发布的 tgz 安装

```bash
npm install -g /absolute/path/everyline-cli-RELEASE_VERSION.tgz
```

Windows PowerShell 同样可以使用：

```powershell
npm install -g C:\absolute\path\everyline-cli-RELEASE_VERSION.tgz
```

检查安装结果：

```bash
everyline-cli version --output json
everyline-cli --help
```

预期结果：命令可以执行，版本与待验证版本一致，stdout 返回 JSON 版本信息。

## 3. 按宿主安装 Skill

npm 包内的 Skill 位于：

```text
<npm-global-root>/everyline-cli/skills/everyline-cli/
```

安装过程只向对应宿主登记 Skill，不执行远端业务请求。

以下命令默认使用 npm 的标准全局目录。CLI 若通过 `npm install -g --prefix "$HOME/.local"` 安装，先在当前终端设置：

```bash
export EVERYLINE_NPM_ROOT="$(npm root -g --prefix "$HOME/.local")"
```

后续命令会优先使用 `EVERYLINE_NPM_ROOT`，从而取得同一份 npm 包中的 CLI 和 Skill。

### 3.1 Codex

#### macOS 或 Linux

使用符号链接可以让后续 npm 升级自动切换到包内的新 Skill：

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
Copy-Item -Recurse -Path $SkillSource -Destination $SkillTarget
```

Windows 采用复制方式；升级 npm 包后，需要在确认旧目录内容后重新复制 Skill。

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
3. CLI 提取合同主体；Skill 展示真实主体名称，让用户选择审查立场方。
4. Skill 查询真实自定义清单和内置规则包选项，让用户选择审查规则来源。
5. Skill 询问审查强度：弱势、中立或强势。
6. 四项业务输入完整后，Skill 使用同一输入执行 dry-run；失败时不发送正式请求。
7. dry-run 通过后，Skill 使用同一 Profile 和身份自动发起一次任务并记录 task ID。
8. Skill 只查询该 task ID，等待终态后返回实际状态及 `reviewDetailUrl`（服务端提供时）。

示例对话：

```text
用户：$everyline-cli 使用 prod-user Profile 和 user 身份审查 /Users/me/Documents/采购合同.pdf

Agent：检测到以下合同主体，请选择审查立场方：
1. 示例采购有限公司
2. 示例供应有限公司

用户：选择 1

Agent：请选择审查规则来源：
1. 按合同类型自动匹配内置规则包
2. 实际查询到的自定义清单 A
3. 实际查询到的自定义清单 B

用户：选择 1 和 2

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
| 正式请求门 | 同一输入的 dry-run 成功后才发起真实任务 |
| 任务幂等 | 取得 task ID 后只查询该任务，不重复创建 |
| 最终结果 | 返回真实终态；服务端提供链接时返回 `reviewDetailUrl` |
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

## 10. 更新与卸载

更新 CLI：

```bash
npm install -g everyline-cli@NEW_RELEASE_VERSION
```

macOS/Linux 使用符号链接时，Skill 会指向新 npm 包中的同名目录。Windows 使用复制安装时，需要核对并替换 `$HOME/.agents/skills/everyline-cli` 中的旧副本。

WorkBuddy 使用符号链接安装时，同样由 npm 包更新 Skill 内容；其目标路径是 `$HOME/.workbuddy/skills/everyline-cli`。豆包使用上传 ZIP 安装，更新 npm 包后需要重新生成 ZIP，并在豆包「我的技能」中更新或重新上传。

卸载前先确认目标路径确实是本 Skill。macOS/Linux 的符号链接可以使用：

```bash
unlink "$HOME/.agents/skills/everyline-cli"
npm uninstall -g everyline-cli
```

WorkBuddy 的符号链接卸载：

```bash
unlink "$HOME/.workbuddy/skills/everyline-cli"
npm uninstall -g everyline-cli
```

豆包先在「我的技能」中移除 `everyline-cli`，再执行 `npm uninstall -g everyline-cli`；不要手工删除豆包客户端的内部数据目录。

Windows 复制安装可以在确认路径后删除 `$HOME\.agents\skills\everyline-cli`，再执行：

```powershell
npm uninstall -g everyline-cli
```
