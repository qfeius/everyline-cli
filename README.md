# everyline-cli

EveryLine 命令行工具，支持合同审查工作流、审查清单和审查规则管理。

公开版本只提供 prod 环境预设。其他部署环境应通过自定义 Profile 配置，不在公开文档和安装包中暴露内部环境地址。

## 安装

### 本地构建

构建要求：Go 1.24 或更高版本、Node.js 18 或更高版本。进入 everyline-cli 源码目录后执行：

~~~bash
cd everyline-cli
make test
make build

mkdir -p ~/.local/bin
install -m 755 bin/everyline-cli ~/.local/bin/everyline-cli
export PATH="$HOME/.local/bin:$PATH"

everyline-cli version
~~~

## 认证方式选择

| 使用场景 | 推荐身份 | 授权方式 |
|---|---|---|
| Codex、人工用户、本地交互 | user | 浏览器 OAuth 授权 |
| 豆包、WorkBuddy 远端沙箱 | user | `auth init` / `auth complete` Device Grant |
| CI、定时任务、无浏览器 Agent | app | app-id + app secret |

user 身份不需要 app-id；app 身份必须配置 app-id。建议为不同身份创建不同 Profile，并在每次调用时显式指定 --profile 和 --as。

## user 用户授权

### 创建 Profile

~~~bash
everyline-cli config add prod-user \
  --env prod \
  --default-identity user
~~~

### 浏览器登录

~~~bash
everyline-cli auth login \
  --profile prod-user \
  --as user \
  --timeout 3m
~~~

CLI 会打开浏览器完成用户登录，通过本机 loopback 回调接收授权结果，并缓存 user token。auth login 不使用 --env；环境在 config add 时指定。

豆包或 WorkBuddy 沙箱与宿主浏览器不共享 loopback 端口时，使用 Device Grant：

~~~bash
everyline-cli auth init --profile prod-user --as user --output json
# 用户打开 verification_uri_complete 并完成授权后：
everyline-cli auth complete --profile prod-user --as user --output json
~~~

`auth init` 不监听 `127.0.0.1`。dev/test 的 `contract-review` metadata 使用独立 EveryLine Device client `zscli_c77221e810ce3977`，scope 固定为 `contract-review:full`；已有旧 Profile 会按标准 metadata URL 自动选择该 client。其他环境也可把平台确认的 endpoint/client ID 写入 Profile。豆包 AgentKit 的安全凭证存储要求注入 base64 编码的 32 字节 `EVERYLINE_CLI_CREDENTIAL_KEY_V1`，WorkBuddy 使用系统凭证库。

检查授权状态：

~~~bash
everyline-cli auth status \
  --profile prod-user \
  --as user \
  --output json
~~~

`auth status` 默认读取当前身份对应的安全缓存。OAuth metadata 声明 refresh grant 时，CLI 会在过期前五分钟尝试刷新；业务请求收到可信 `code=110004` 时只刷新并重放一次。服务端不支持刷新或返回 `invalid_grant` 时清理被拒绝的旧 token；并发写入的新 token 会保留。

如果 prod 预设尚未包含正式的 OAuth metadata、client ID 和 loopback redirect，需要由平台提供确认后的配置，再通过 --oauth-* 参数补充。CLI 不猜测 OAuth 地址，也不把 AuthURL 直接当作 OAuth authorization endpoint。

## app 应用授权

### 创建 Profile

~~~bash
export EVERYLINE_APP_ID='cli_prod_xxx'

everyline-cli config add prod-app \
  --env prod \
  --default-identity app \
  --app-id "$EVERYLINE_APP_ID"
~~~

### 登录

本地或 CI 环境都建议通过 stdin 传入 app secret，避免 secret 出现在命令参数中：

~~~bash
export EVERYLINE_APP_SECRET='从 Secret Manager 注入的值'

printf '%s' "$EVERYLINE_APP_SECRET" | \
  everyline-cli auth login \
    --profile prod-app \
    --as app \
    --app-secret-stdin \
    --output json
~~~

本地开发机如果希望保存 app secret，可以显式使用 --save-app-secret。macOS 优先保存到 Keychain，其他系统使用配置目录下的 secrets.json。CI 和定时任务不应持久化 app secret。

app-id 的来源优先级为：--app-id > Profile 专用环境变量 > EVERYLINE_APP_ID > Profile。app secret 的来源优先级为：--app-secret > --app-secret-stdin > Profile 专用环境变量 > EVERYLINE_APP_SECRET > 本地安全存储。

## Codex/Agent 最佳实践

仓库和 npm 发布包包含三项职责分离的交互式 Skill：`everyline-shared` 负责首次配置、身份和授权，`everyline-review` 负责单份合同审查，`everyline-review-config` 负责清单、规则和规则分组。三项 Skill 共用当前 CLI 的实时帮助和结构化输出约束；旧 `skills/everyline-cli/` 作为兼容内容继续随包发布。

全局安装 npm 包时，`postinstall` 会把三项 Skill 同步登记到 Codex 的 `$HOME/.agents/skills` 和 WorkBuddy 的 `$HOME/.workbuddy/skills`，并在首次安装建立授权门禁：旧 token 保留，但必须完成一次新的 user/app 授权后才能调用审查、清单或规则业务命令。项目局部安装和 `npx` 临时执行不登记用户级 Skill；豆包通过 `make skill-assets` 生成的三个独立 ZIP 从界面导入。Skill 不修改或替代 CLI 接口。

在 Codex、WorkBuddy 或豆包电脑版安装并验证完整交互流程，请参阅 [EveryLine CLI 交互 Skill 安装与验证](docs/everyline-cli-skill-guide.md)；评审全部对话分支，请参阅 [EveryLine CLI Skill 全量交互场景](docs/everyline-cli-skill-interaction-scenarios.md)。

Agent 执行 CLI 时建议遵循固定流程：

1. 检查版本和命令路径。
2. 使用显式 --profile 和 --as，不要依赖当前默认 Profile。
3. 先执行 auth status --output json。
4. 正式请求前先使用 --dry-run 或 --print-input。
5. 使用 --output json，不要解析 table 输出。
6. 将 stdout 作为业务结果，将 stderr 作为进度和诊断。
7. 已有 task-id 时使用 review task result，不要重复发起任务。
8. 遇到超时不要盲目重试 review task start，优先查询已有任务状态。

长期无人值守任务仍使用 app 身份。人工参与的豆包/WorkBuddy 沙箱可通过 Device Grant 使用 user 身份；Codex 本地交互继续使用 `auth login`。

Agent 调用示例：

~~~bash
everyline-cli auth status \
  --profile prod-app \
  --as app \
  --output json

everyline-cli review run \
  --profile prod-app \
  --as app \
  --input review-run.json \
  --output json \
  --timeout 30s \
  --deadline 10m \
  --interval 2s \
  > result.json 2> progress.log
~~~

## 执行合同审查

### 一键流程

准备 review-run.json：

~~~json
{
  "source": {
    "type": "file",
    "path": "./contract.pdf",
    "name": "采购合同.pdf"
  },
  "config": {
    "selectedPosition": "xxx公司",
    "selectedAuditRole": "甲方",
    "reviewStrength": "中立",
    "matchContractTypeRulePackage": true
  },
  "extractSubjects": true,
  "wait": true
}
~~~

先进行本地校验：

~~~bash
everyline-cli review run \
  --profile prod-user \
  --as user \
  --input review-run.json \
  --dry-run
~~~

确认无误后执行：

~~~bash
everyline-cli review run \
  --profile prod-user \
  --as user \
  --input review-run.json \
  --output json \
  > result.json 2> progress.log
~~~

config.selectedPosition、config.selectedAuditRole 和 config.reviewStrength 是发起审查必填字段。`selectedPosition` 填写合同主体的精确名称，`selectedAuditRole` 填写同一主体在主体提取响应中的角色，例如“甲方”或“乙方”。`reviewStrength` 推荐使用“弱势/中立/强势”，并兼容旧版 `0/1/2`；CLI 在 HTTP 边界统一转换为 `0/1/2`。规则来源必须满足非空 `selectedCheckListIds` 或 `matchContractTypeRulePackage=true` 至少一项；两项同时提供时组合执行，互不覆盖且没有优先级。文件来源通常由上传接口补充文件身份；URL 来源还需要提供 businessId 和上传接口返回的 fileHash。发起审查的实际 HTTP 请求会固定补入 `usageReportContext.reportBusinessCode=everyLine_100_openApi_cli`，调用方无需提供也不能覆盖。

### 分步流程

需要控制每个阶段时使用：

~~~text
review file upload
        ↓
review task start
        ↓
review task result
~~~

上传文件：

~~~bash
everyline-cli review file upload \
  --profile prod-user \
  --as user \
  --file ./contract.pdf \
  --name 采购合同.pdf \
  --output json > upload.json
~~~

沙箱宿主只提供原始附件字节流时，可保持相同上传契约并改用 stdin：

~~~bash
everyline-cli review file upload --profile prod-user --as user --stdin --name 采购合同.pdf --output json < attachment.pdf
~~~

使用上传结果中的 businessId、fileId 和 fileHash 发起任务：

~~~bash
everyline-cli review task start \
  --profile prod-user \
  --as user \
  --data '{"businessId":"biz-001","fileId":123456,"fileHash":"<upload.fileHash>","config":{"selectedPosition":"xxx公司","selectedAuditRole":"甲方","reviewStrength":"中立","matchContractTypeRulePackage":true}}' \
  --output json
~~~

拿到 taskId 后获取最终结果：

~~~bash
everyline-cli review task result \
  --profile prod-user \
  --as user \
  --task-id 123456789 \
  --output json > result.json
~~~

review task result 会在 stderr 输出 running 等任务状态，并把最终审查结果输出到 stdout；后端提供顶层 `url` 时会规范化为 `reviewDetailUrl`。JSON/raw 不对 URL 中的 `&` 做 HTML 转义，Skill 必须逐字展示包含完整 `token` query 的字段值。飞书用户 OAuth 响应缺少预览链接时仍返回完整成功详情。

CLI 已移除 --app-type 参数；文件上传也不再接受 --business-id。发起审查输入只接受当前契约声明的字段，未知字段会被拒绝；`usageReportContext` 仅由 CLI 在 HTTP 边界固定生成。

## 自更新

`everyline-cli version` 输出 `latestVersion/isLatest/updateRequired/updateCommand`；检查失败时 `isLatest=null`，且不会把未知状态当成需要更新。普通 review/checklist/rule 命令确认存在新版本时，在 stderr 输出 `UPDATE_PENDING` 单行 JSON，但继续完成当前业务 API。Agent 在当前完整业务流程结束后执行其中的 `updateCommand`，下一条新业务再使用新版 CLI 与 Skill。

独立二进制可以通过 HTTPS manifest 检查和更新当前平台制品。地址按 `--manifest-url`、`EVERYLINE_CLI_UPDATE_MANIFEST_URL`、发布构建内置值的顺序选择：

~~~bash
everyline-cli update \
  --manifest-url https://example.com/everyline-cli/manifest.json \
  --dry-run

everyline-cli update \
  --manifest-url https://example.com/everyline-cli/manifest.json
~~~

manifest 需要声明版本、当前平台的制品 URL 和 SHA-256。CLI 只有在新版本、平台匹配且摘要校验通过时才替换二进制；下载失败或校验失败会保留原文件。Windows 需要延后替换时返回 `updated=false, scheduled=true`，独立 helper 的最终结果写入 stderr。通过 npm/npx 薄包装启动时不会修改包内二进制，请使用 npm 更新包。

Codex 与 WorkBuddy 的 CLI 和三项 Skill 来自同一个 npm 包，npm 更新成功后现有目录链接会直接使用新版 Skill。版本门禁以统一包版本为检测信号，因此每次 Skill 发布（包括纯文案调整）都必须提升 `package.json` 版本、发布同版本 npm 包并更新远端 manifest；只替换 ZIP 而不提升统一版本不会触发本地强制更新。豆包导入版仍按平台发布流程上传新 ZIP。

manifest 示例：

~~~json
{
  "version": "1.2.3",
  "platforms": {
    "darwin-arm64": {
      "url": "https://example.com/everyline-cli/1.2.3/darwin-arm64/everyline-cli",
      "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    }
  }
}
~~~

## 输出与安全

- 新 Profile 和未配置回退的默认输出为 JSON；显式配置的 table、yaml、raw 保持不变。
- 结构化结果使用 --output json 或 --raw，便于 Agent 和脚本解析。
- stdout 只承载业务结果；进度、浏览器授权提示和诊断信息写入 stderr。
- --dry-run 和 --print-input 只校验并输出规范化请求，不调用远端。
- 不要把 app secret、token 或 OAuth code 写入代码、日志、命令参数或提交记录。
- auth logout 删除 token 缓存；显式保存的 app secret 不会被自动删除。
- token、Profile 和 secret 文件由 CLI 使用受限权限保存；不要把配置目录加入公共仓库。

## 常用命令

~~~bash
everyline-cli config list
everyline-cli config show prod-user --output yaml
everyline-cli auth status --profile prod-user --as user --output json
everyline-cli auth logout --profile prod-user --as user
everyline-cli completion zsh
~~~

完整命令和 API 映射见命令参考与 API 映射。

## 开源贡献

提交代码前请确认：

- 不新增内部环境地址、内部域名或未确认的 OAuth 配置。
- 不提交 app secret、access token、OAuth code、Keychain 内容或本地配置文件。
- 使用 fake HTTP、临时 Profile 和测试 token 验证功能，不依赖真实 prod 凭证。
- 运行 go test ./...、go vet ./... 和 make build。
