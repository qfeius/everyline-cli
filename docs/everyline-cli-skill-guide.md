# EveryLine CLI 交互 Skill 安装与验证

本文用于在一台全新设备上安装 `everyline-cli` 及配套交互 Skill，并验证合同审查的多轮对话流程。Skill 负责对话和流程编排，CLI 继续负责授权、文件上传、主体提取、清单查询、任务发起和结果查询；安装 Skill 不会改变 CLI 的命令、参数或 JSON 接口。

需要逐项评审或测试当前 Skill 的全部分支时，请结合 [EveryLine CLI Skill 全量交互场景](everyline-cli-skill-interaction-scenarios.md)。

OpenAI 官方文档将 Skill 定义为包含 `SKILL.md` 及可选 references、scripts、assets 的目录。Codex 支持显式 `$skill-name` 调用和按 description 自动触发，并会扫描用户级 `$HOME/.agents/skills`；发现结果未刷新时可以重启 Codex。详见 [OpenAI Build skills](https://learn.chatgpt.com/docs/build-skills)。

## 1. 验证前准备

准备以下内容：

- 已安装 Codex 桌面端、Codex CLI 或 IDE 扩展，并支持 `/skills`。
- Node.js 18 或更高版本。
- `everyline-cli` 的 npm 版本号，或待验证的 `.tgz` 发布包。
- 一个可访问智审开放平台的 user 或 app 身份。
- 一份用于验证的 DOC、DOCX 或 PDF 合同；建议使用测试文件。

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

## 3. 安装 Skill

npm 包内的 Skill 位于：

```text
<npm-global-root>/everyline-cli/skills/everyline-cli/
```

安装过程只把该目录暴露给 Codex，不执行远端业务请求。

### macOS 或 Linux

使用符号链接可以让后续 npm 升级自动切换到包内的新 Skill：

```bash
skill_source="$(npm root -g)/everyline-cli/skills/everyline-cli"
skill_target="$HOME/.agents/skills/everyline-cli"

test -f "$skill_source/SKILL.md"
mkdir -p "$HOME/.agents/skills"
test ! -e "$skill_target"
ln -s "$skill_source" "$skill_target"
```

最后两个 `test` 会在源文件缺失或目标已经存在时停止，避免生成错误的嵌套目录或覆盖已有 Skill。

### Windows PowerShell

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

### 从源码目录验证

如果验证设备可以访问源码，也可以把仓库中的 `skills/everyline-cli/` 复制或链接到：

```text
$HOME/.agents/skills/everyline-cli/
```

目标目录下应直接包含 `SKILL.md` 和 `references/`，不要再多嵌套一层 `everyline-cli/`。

## 4. 确认 Codex 已识别 Skill

重新打开 Codex 会话，执行：

```text
/skills
```

预期列表中出现 `everyline-cli`。也可以输入 `$` 后搜索 `everyline-cli`。

如果目录已经正确但列表尚未刷新，重启 Codex 后再检查。官方文档说明 Codex 支持自动检测 Skill 变化，未出现时可通过重启刷新。

先运行一个不上传合同的冒烟验证：

```text
$everyline-cli 使用 user 身份检查 CLI 版本、审查能力和授权状态，先不要上传合同。
```

预期行为：Codex 检查 `everyline-cli` 路径、版本、帮助信息和授权状态，不要求填写 `businessId`、`fileHash` 等内部字段。

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

不要在 Codex 对话、命令参数、截图或日志中粘贴 app secret。测试完成后清理当前终端中的临时 secret 环境变量。

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

在 Codex 中输入以下提示，并替换为验证设备可读取的绝对文件路径：

```text
$everyline-cli 使用 user 身份审查 /absolute/path/采购合同.pdf
```

正常交互顺序如下：

1. Skill 检查 CLI 版本、目标命令和 user 授权状态。
2. CLI 上传合同并返回内部文件身份。
3. CLI 提取合同主体；Skill 展示真实主体名称，让用户选择审查立场方。
4. Skill 查询真实自定义清单和内置规则包选项，让用户选择审查规则来源。
5. Skill 询问审查强度：弱势、中立或强势。
6. 四项业务输入完整后，Skill 自动发起任务并记录 task ID。
7. Skill 只查询该 task ID，等待终态后返回实际状态及 `reviewDetailUrl`（服务端提供时）。

示例对话：

```text
用户：$everyline-cli 使用 user 身份审查 /Users/me/Documents/采购合同.pdf

Codex：检测到以下合同主体，请选择审查立场方：
1. 示例采购有限公司
2. 示例供应有限公司

用户：选择 1

Codex：请选择审查规则来源：
1. 按合同类型自动匹配内置规则包
2. 实际查询到的自定义清单 A
3. 实际查询到的自定义清单 B

用户：选择 1 和 2

Codex：请选择审查强度：弱势、中立、强势。

用户：中立

Codex：任务已创建，正在等待审查结果……
```

候选主体、清单名称和最终链接必须来自当前身份下的真实 CLI 响应。示例名称仅用于说明对话形式。

如果用户已经知道全部输入，可以在第一句话一次提供：

```text
$everyline-cli 使用 user 身份审查 /absolute/path/采购合同.pdf；立场方是示例采购有限公司；使用内置规则包和自定义清单 A；强度中立。
```

信息能够唯一匹配时，Skill 会减少追问。

## 8. 验收标准

| 检查项 | 通过标准 |
| --- | --- |
| Skill 发现 | `/skills` 中出现 `everyline-cli` |
| CLI 发现 | `everyline-cli version --output json` 成功，版本符合预期 |
| 授权 | `auth status` 返回 `authenticated=true` |
| 输入交互 | 用户只需提供合同来源、立场方、清单和强度 |
| 内部参数 | Skill 不向用户索要 `businessId/fileId/fileHash/selectedAuditRole/wait` |
| 候选数据 | 主体、清单、规则均来自实时 CLI 查询 |
| 任务幂等 | 取得 task ID 后只查询该任务，不重复创建 |
| 最终结果 | 返回真实终态；服务端提供链接时返回 `reviewDetailUrl` |
| CLI 兼容 | 原有命令、参数和 JSON 接口保持不变 |

## 9. 常见问题

### `/skills` 中没有 `everyline-cli`

- 检查 `$HOME/.agents/skills/everyline-cli/SKILL.md` 是否直接存在。
- 检查是否误生成了 `everyline-cli/everyline-cli/SKILL.md` 双层目录。
- 重启 Codex 后重新执行 `/skills`。

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

卸载前先确认目标路径确实是本 Skill。macOS/Linux 的符号链接可以使用：

```bash
unlink "$HOME/.agents/skills/everyline-cli"
npm uninstall -g everyline-cli
```

Windows 复制安装可以在确认路径后删除 `$HOME\.agents\skills\everyline-cli`，再执行：

```powershell
npm uninstall -g everyline-cli
```
