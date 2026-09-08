# everyline-cli

智审开放平台 V3 的 Go 命令行客户端。覆盖接口文档中的 25 个远端 operation 映射：Profile/tenant token、合同审查工作流、审查清单以及规则分组/规则管理；其中 21 项已开放真实调用，4 项缺字段级详情的批量写操作失败关闭。文件快照和字段捷径接口按当前 CLI 范围暂不纳入同步。

## 构建

需要 Go 1.24 或更高版本。

```bash
make test
make build
./bin/everyline-cli version
```

## 快速开始

```bash
everyline-cli config add dev \
  --env dev \
  --app-id cli_xxx

export EVERYLINE_APP_SECRET='replace-me'
everyline-cli auth login

everyline-cli review run --input review-run.json --output json
```

URL 来源的一键审查输入还需提供 `businessId` 与对应文件内容的 SHA-256 `fileHash`，因为标准 URL 上传接口的响应只返回 `fileId`。

`appType=CLM` 时，本地文件上传也必须提供 `businessId`；CLI 会在上传前校验这一条件。

也可以使用内置环境地址创建 `test`、`blue` 或线上 `prod` Profile（`app-id` 仍需替换为该环境的应用 ID）：

```bash
everyline-cli config add test --env test --app-id cli_test_xxx
everyline-cli config add blue --env blue --app-id cli_blue_xxx
everyline-cli config add prod --env prod --app-id cli_prod_xxx
```

`app_secret` 不写入 Profile，也不提供明文命令行参数。可使用 `EVERYLINE_APP_SECRET`、Profile 专用变量或 `auth login --app-secret-stdin`。专用变量以 `EVERYLINE_APP_SECRET_` 开头；常见的小写字母映射为大写，数字保持不变，其他 UTF-8 字节（包括原始大写字母）编码为 `_XX`，因此后缀可逆且不会因大小写或符号碰撞。例如 `prod-eu`、`prod.eu`、`prod_eu` 分别对应 `EVERYLINE_APP_SECRET_PROD_2DEU`、`EVERYLINE_APP_SECRET_PROD_2EEU`、`EVERYLINE_APP_SECRET_PROD_5FEU`。token 缓存在权限为 `0600` 的 `~/.everyline-cli/tokens.json`；CI 可用 `EVERYLINE_ACCESS_TOKEN` 覆盖。

CLI 同时支持 `app` 和 `user` 两种身份。`--env` 会自动写入每个环境对应的智审自有认证页面：dev 为 `https://dev-contract-agent.qtech.cn`、test 为 `https://test-contract-agent.qtech.cn`、blue 为 `https://blue-contract-agent.qtech.cn`、prod 为 `https://contract-agent.qfei.cn`。用户在页面完成认证后，将页面交接的 access token 通过 stdin 或 `EVERYLINE_USER_ACCESS_TOKEN` 注入 CLI：

```bash
everyline-cli auth login --as user --access-token-stdin
everyline-cli auth use --as user
everyline-cli review run --as user --input review-run.json --output json
```

`auth login --as user` 缺少 token 时会输出当前 Profile 的自有认证页面地址；CLI 不猜测页面内部协议。app/user token 在本地缓存中隔离保存。

## 一键审查输入

```json
{
  "source": {
    "type": "file",
    "path": "./contract.pdf",
    "name": "采购合同.pdf"
  },
  "businessId": "biz-001",
  "appType": "THIRD_PARTY",
  "config": {},
  "extractSubjects": true,
  "wait": true
}
```

完整命令和 25 项接口映射见 [命令参考](docs/command-reference.md) 与 [API 映射](docs/api-mapping.md)。清单、规则分组和规则的所有删除操作都要求显式 `--yes`；`--dry-run` 不调用远端。

可通过 `everyline-cli completion bash|fish|powershell|zsh` 生成 shell 补全脚本。四个尚缺字段级详情页的批量创建/更新命令仅开放 `--dry-run`，真实请求会失败关闭，具体清单见 API 映射。

## npm/npx 包装

Node 包只负责启动对应平台的 Go 二进制，CLI 本体没有 Node 运行时依赖。发布前运行 `scripts/build-release-assets.sh` 生成六个平台架构的 `bin/<platform>-<arch>/everyline-cli`。CI 会把 tag 或提交版本规范化为 SemVer、同步到 npm manifest，并产出带正确版本号的 `.tgz` 制品。

安装 scoped npm 包后，执行文件名仍为 `everyline-cli`：

```bash
npx @yuwengueen/everyline-test version
```
