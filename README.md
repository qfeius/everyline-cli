# everyline-cli

智审开放平台的命令行客户端，支持合同审查工作流、审查清单和审查规则管理。

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

检查授权状态：

~~~bash
everyline-cli auth status \
  --profile prod-user \
  --as user \
  --output json
~~~

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

Agent 执行 CLI 时建议遵循固定流程：

1. 检查版本和命令路径。
2. 使用显式 --profile 和 --as，不要依赖当前默认 Profile。
3. 先执行 auth status --output json。
4. 正式请求前先使用 --dry-run 或 --print-input。
5. 使用 --output json，不要解析 table 输出。
6. 将 stdout 作为业务结果，将 stderr 作为进度和诊断。
7. 已有 task-id 时使用 review task result，不要重复发起任务。
8. 遇到超时不要盲目重试 review task start，优先查询已有任务状态。

无浏览器 Agent 应使用 app 身份。user OAuth 需要用户在浏览器中完成首次授权，适合在 Codex 所在的交互式电脑上由用户完成一次授权后复用。

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
    "selectedPosition": "甲方",
    "selectedAuditRole": "甲方",
    "reviewStrength": 1
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

config.selectedPosition、config.selectedAuditRole 和 config.reviewStrength 是发起审查必填字段。文件来源通常由上传接口补充文件身份；URL 来源还需要提供 businessId 和上传接口返回的 fileHash。

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

使用上传结果中的 businessId、fileId 和 fileHash 发起任务：

~~~bash
everyline-cli review task start \
  --profile prod-user \
  --as user \
  --data '{"businessId":"biz-001","fileId":123456,"fileHash":"<upload.fileHash>","config":{"selectedPosition":"甲方","selectedAuditRole":"甲方","reviewStrength":1}}' \
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

review task result 会在 stderr 输出 running 等任务状态，最终审查结果仍输出到 stdout。

CLI 已移除 --app-type 参数；文件上传也不再接受 --business-id。发起审查输入只接受当前契约声明的字段，未知字段会被拒绝。

## 自更新

独立二进制可以通过显式 HTTPS manifest 检查和更新当前平台制品：

~~~bash
everyline-cli update \
  --manifest-url https://example.com/everyline-cli/manifest.json \
  --dry-run

everyline-cli update \
  --manifest-url https://example.com/everyline-cli/manifest.json
~~~

manifest 需要声明版本、当前平台的制品 URL 和 SHA-256。CLI 只有在新版本、平台匹配且摘要校验通过时才替换二进制；下载失败或校验失败会保留原文件。通过 npm/npx 薄包装启动时不会修改包内二进制，请使用 npm 更新包。

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
