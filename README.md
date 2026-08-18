# everyline-cli

智审开放平台 V3 的 Go 命令行客户端。实现技术方案中的 27 个远端 operation：Profile/tenant token、完整合同审查工作流、飞书字段捷径发起、审查清单以及规则分组/规则管理。

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
  --base-url https://example.com \
  --token-url https://example.com/open-apis/auth/v3/tenant_access_token/internal \
  --app-id cli_xxx

export EVERYLINE_APP_SECRET='replace-me'
everyline-cli auth login

everyline-cli review run --input review-run.json --output json
```

`app_secret` 不写入 Profile，也不提供明文命令行参数。可使用 `EVERYLINE_APP_SECRET`、`EVERYLINE_APP_SECRET_<PROFILE>` 或 `auth login --app-secret-stdin`。token 缓存在权限为 `0600` 的 `~/.everyline-cli/tokens.json`；CI 可用 `EVERYLINE_ACCESS_TOKEN` 覆盖。

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

完整命令和 27 项接口映射见 [命令参考](docs/command-reference.md) 与 [API 映射](docs/api-mapping.md)。清单、规则分组和规则的所有删除操作都要求显式 `--yes`；`--dry-run` 不调用远端。

## npm/npx 包装

Node 包只负责启动对应平台的 Go 二进制，CLI 本体没有 Node 运行时依赖。发布前运行 `scripts/build-release-assets.sh` 生成六个平台架构的 `bin/<platform>-<arch>/everyline-cli`，随后再打包 npm 产物。
