# 自更新行为契约

## 目标

提供独立二进制的安全自更新命令和非阻断版本检查。更新源由命令参数、环境变量或发布构建通过 HTTPS manifest 配置，不依赖 Profile 或业务鉴权。

命令形式：

```text
everyline-cli version [--manifest-url https://example.com/everyline-cli/manifest.json]
everyline-cli update [--manifest-url https://example.com/everyline-cli/manifest.json] [--dry-run]
```

## manifest 契约

```json
{
  "version": "1.2.3",
  "platforms": {
    "darwin-arm64": {
      "url": "https://example.com/everyline-cli/1.2.3/darwin-arm64/everyline-cli",
      "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    }
  }
}
```

约束：

- `version` 和当前构建版本都必须是可比较的 SemVer；当前开发构建不能直接执行自更新。
- 平台键使用 `GOOS-GOARCH`，例如 `darwin-arm64`、`linux-amd64`、`windows-amd64`。
- manifest 和制品地址必须使用 HTTPS。
- `sha256` 必须是 64 位十六进制字符串。
- manifest 必须包含当前平台制品；未提供时命令失败关闭。
- 当前版本大于或等于 manifest 版本时不下载、不替换。

SHA-256 用于确认下载内容未被传输过程修改；当前没有签名公钥或签名发布协议，因此不把签名校验伪装成已实现的安全边界。制品来源信任由 manifest 地址的管理方承担。

## 运行链路

```text
update command
  -> 校验 manifest URL、当前版本和 wrapper 边界
  -> GET manifest
  -> 选择当前平台制品
  -> 比较版本
  -> 下载到当前可执行文件同目录的临时文件
  -> 校验 SHA-256 和文件权限
  -> 原子替换当前独立二进制
  -> 输出更新结果
```

更新使用根命令的 `--timeout` 作为 manifest 与制品下载的总时间预算。临时文件位于目标文件同一目录，保证 Unix 系统上的替换保持同一文件系统；校验失败、取消、超时或替换失败均保留原二进制。

Windows 无法覆盖正在运行的可执行文件时，CLI 先复制出独立 helper，再由该副本在有限窗口内重试替换。helper 不占用目标文件，也不依赖可能已经退出的父进程句柄；最终成功或失败通过继承的 stderr 输出，helper 副本登记为系统重启时清理。助手不是公开命令。

通过 npm/npx 薄包装启动时，包装器设置 `EVERYLINE_CLI_WRAPPER=1`。此时 `update` 不下载、不修改 npm 包内二进制，直接提示使用 npm 更新包，避免把 npm 安装目录当作独立安装目标。

## 可观察行为

| 场景 | 结果 |
|---|---|
| manifest URL 缺失、非 HTTPS 或不可访问 | 返回错误，不访问业务 API，不修改本地二进制 |
| 当前版本不可比较 | 返回错误，不下载制品 |
| 当前版本已是最新或更高 | 返回 `updated=false`，不下载、不替换 |
| 当前平台没有制品 | 返回错误，原二进制不变 |
| 制品下载失败或 SHA-256 不匹配 | 返回错误，原二进制不变 |
| `--dry-run` | 完成 manifest、平台和版本校验，不下载制品、不替换 |
| Unix 独立二进制下载并同步替换成功 | 返回 `updated=true, scheduled=false` |
| Windows 制品已交给独立 helper | 返回 `updated=false, scheduled=true`；最终结果由 helper 写入 stderr |
| npm/npx 薄包装调用 | 返回安装方式提示，不执行更新 |

`version` 稳定输出当前构建字段以及 `latestVersion/isLatest/updateCommand`。manifest 未配置或检查失败时，命令仍返回成功，`isLatest=null` 并输出 `checkError`，不把未知状态误报为最新版。普通 `review/checklist/rule` 命令只在确认存在新版本时向 stderr 写提示；检查失败不改变业务请求和退出码。

## `review task result` 当前边界

命令默认轮询 `status`，每次响应向 stderr 输出状态；`success` 后调用一次 `info`，顶层 `url` 可用时规范化为 `reviewDetailUrl`，stdout 只输出最终业务结果。飞书用户 OAuth 响应没有预览链接时仍返回完整成功详情。CLI 当前确认的状态集合为：

- `running`：继续轮询；
- `success`：结束轮询并获取详情；
- `fail`：结束轮询并返回失败快照；
- 空状态：按任务不存在或无权访问处理；
- 其他状态：保留快照并报未知状态错误。

仓库没有后端完整状态枚举，因此 `queued`、`pending`、`processing` 等是否属于服务端合法中间态仍是待确认项。在后端契约确认前，CLI 不把未知值静默当作可等待状态，避免无限等待或误判终态。

## checklist/rule 批量写能力

四个批量创建/更新操作属于非必需能力。它们保留输入校验和 `--dry-run`/`--print-input`，真实请求在 HTTP 发送前失败关闭；该保护是当前能力边界，不阻塞单项清单、规则及批量删除链路。
