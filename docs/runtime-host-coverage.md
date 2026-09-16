# 客户端识别增量与样本回放

本页记录 `process-ancestry-v4` 引入的场景，当前探测器已升级到 `process-ancestry-v6`。EveryLine 与 contract-cli 使用相同规则；当前产品编码分别为 contract-review、contract。

## 原有能力

保留已在 Windows/macOS 验证的 Codex、豆包、豆包工作、WorkBuddy 国内版身份规则，以及 Windows 的环境路径/产品元数据兜底。macOS 签名验证跳过资源内容校验，继续校验代码签名与 Bundle ID/Team ID。新增兜底不覆盖已有命中或签名不匹配结果。

## 2026-09-11 样本与新增规则

来源：[环境检测脚本及结果](https://ysi13ckdb9.feishu.cn/wiki/Z4G7wmQg0iukzYkx45dcMyo0nLc)。采集脚本的场景标签由测试者填写，并非自动探测证据。

| 场景 | 证据与处理 |
|---|---|
| 独立豆包工作 Mac 本地 | 签名验证 exit_code=0，com.work.pc.doubao / 96L78H6LMH；回放可命中原有 high 规则 |
| 飞书内豆包工作 Mac 本地 | Lark.app 的 com.electron.lark / XY6NLV7YTS 有效签名，加四个 DOUBAO_OFFICE 运行时标记，新增 doubaoWork / low；飞书签名本身不代表豆包工作 |
| 豆包工作 Linux 云端（包括飞书入口） | 四个 DOUBAO_OFFICE 标记，加 DOUBAO_SANDBOX_TYPE、AIO_CLI_BIN_DIR，新增 doubaoWork / low；不声称能区分入口 |
| WorkBuddy Mac 国内版/国际版本地 | 样本 ps 报 process unavailable，无签名证据；WORKBUDDY_CONFIG_DIR + CODEBUDDY_BROKERED_SHELL_ENV + CODEBUDDY_NODE_BIN + BASH_ENV 组合新增 workbuddy / low，不区分发行区域 |
| WorkBuddy Web 云端 | v2 产品值 WorkBuddy_Web / web_agents / agent-server / sdk-go，加 AgentOS 标记，返回 workbuddy / low |
| 飞书豆包工作伙伴云端 | v2 的 aily_agent 标签、Aily 任务标记、同一 .aily 根目录下实际存在的 workdir/task_* 与 workspace，加 python-server / runtime-agent 祖先进程，返回 doubaoWorkmates / low |
| 飞书内豆包工作 Windows 本地 | Feishu.exe 有效签名指纹 491a3724…311c11c，加四个 Office 标记，新增 doubaoWork / low；不把飞书单独归类为豆包工作 |
| WorkBuddy 国际版 Windows 本地 | WorkBuddyAI.exe 有效签名指纹 a5260c88…81a0f45，独立登记程序路径和证书组合，返回 workbuddy / high |
| WorkBuddy 国际版 Web | v2 产品值与国内 Web 完全相同，复用同一条 workbuddy Web 规则，不推断发行区域 |

四个 Office 标记是 DOUBAO_OFFICE_AGENT_NAME、DOUBAO_OFFICE_EDITION、DOUBAO_OFFICE_MARKET、DOUBAO_OFFICE_PLATFORM_APP_ID。实现只检查非空存在性，不输出值，不读取 Token 或会话 ID。环境组合证据统一 low；即使宿主签名有效，也不把内嵌产品归因提升为 high。出现不同产品的组合标记冲突时保持 unknown。

Web 匹配只读取 CLIENT_INFO_IDE_TYPE、CLIENT_INFO_PLATFORM、CODEBUDDY_SESSION_BIZ_SOURCE、CODEBUDDY_CODE_ENTRYPOINT 的白名单产品值；会话变量仅检查存在性，不读取其值。工作伙伴只额外读取 LARKSUITE_CLI_AGENT_NAME、AILY_WORKDIR、AILY_WORKSPACE。返回原因与业务 Header 不包含标签值或目录。

工作伙伴规则是对本次观测运行时组合的低置信度归因，不是厂商保证的唯一产品身份；仅有 AILY 前缀或 aily_agent 标签不够。其他 Aily 产品若复用完整组合，仍需更细的产品标识。两个 Web 版本的相同字段也不能用来区分地区。

新增 evidence_type：macos_signed_host_runtime、windows_signed_host_runtime、macos_runtime_environment、linux_runtime_environment。新增来源值 doubaoWorkmates；国内/国际 WorkBuddy 都保持 workbuddy。来源 Header 不用于鉴权。

## 验证

`environment_runtime_test.go` 是脱敏字段投影的规则回放，覆盖飞书组合身份、独立豆包工作原规则、WorkBuddy 中外版本地环境、Office 云端、缺字段、冲突、通用变量误判防护与原决策优先级。不是在实际 Mac 上运行 CLI 的证明；也不声称这些样本覆盖了未提供的 Codex/豆包 Mac 实机记录。

Windows 的飞书内豆包工作本地和 WorkBuddy 国际版本地样本已补齐并加入回放测试，无需重复采集。原有四类 Windows 国内版本地规则保持不变。云端无需仅因访问者使用 Windows 而重复全测。

工作伙伴和两个 WorkBuddy Web 的 v2 样本已补齐，environment_cloud_test.go 覆盖完整匹配、错误/缺失产品值、Aily 目录缺失/跨根、缺少祖先进程、冲突、已有决策保留及真实检测入口接线。无需继续重复采集上述场景；最终仍需用发布包做实际业务请求验收。

补充采集脚本 v2 保留 ps 的失败详情，并仅新增产品标签白名单值和运行时路径；不采集 Token、AILY_SIGNATURE、用户/会话 ID 的值。
