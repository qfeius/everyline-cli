# Windows WorkBuddy 升级兼容修复（2026-09-14）

后续统一策略已更新为 [process-ancestry-v6](attribution-stability.md)，以下为 v5 修复记录。

识别器版本：`process-ancestry-v5`。

- WorkBuddy 国内／海外 Windows 版本仍统一识别为 `workbuddy`。已登记的有效签名优先提供 high 置信度；签名未登记或验证不可用不再阻断进程／路径归因，降级为 medium（路径）或 low（名称），不作为鉴权依据。其他产品的签名不匹配处理不变。
- 补充已存在的 `WorkBuddy`／`WorkBuddyAI` 安装目录下 `resources/app.asar.unpacked/cli/vendor/shim/safe-bin/safe-delete-bash-env.sh` 运行环境证据，返回 low 置信度。原有 shell-runtime 入口的 product.json 检查保留。
- macOS 使用 Bundle ID + Team ID，没有绑定叶证书指纹，资源内容校验已跳过；本次不修改。Linux 云端使用运行时标记，没有证书指纹检查；本次不修改，也不改变普通豆包云端的分类。
- 海外样本中的主程序签名已匹配旧登记，重放应保持 high；此前请求 evidence=none 的具体原因仍需实际 CLI 版本、进程采集结果及错误信息定位。本次修复兜底缺口，不声称已复现该原请求。

验证覆盖证书变化、验签不可用、沙箱与主程序证书不同、已登记主程序优先、新旧 shell 路径及非 WorkBuddy 路径拒绝。Linux 单元测试不能替代 Windows 实机 WinTrust 与父进程采集验收。
