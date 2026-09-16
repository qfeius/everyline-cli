# 客户端来源识别稳定性（process-ancestry-v6）

EveryLine CLI 与 Contract CLI 使用相同采集和判定规则，仅产品编码不同。来源字段用于统计与诊断，不参与登录鉴权或业务权限判断。

- 先采集祖先进程路径、进程名和已有运行时组合证据，发布完整的初步报告，再执行签名检查。
- 已登记签名／包身份命中返回 high；签名变化、未登记或验证失败，不再否决明确产品路径（medium）或进程名（low）。此策略适用于 macOS 和 Windows 的所有已登记客户端。
- 多个不同产品的祖先进程路径同时命中时，返回 unknown / conflicting_process_evidence。国内与海外 WorkBuddy 仍属于同一产品。
- WorkBuddy macOS 同时登记 com.workbuddy.workbuddy、com.tencent.workbuddy.mac 与国际版 com.workbuddy.workbuddy-ai，Team ID 均为 FN2V63AD2J；路径同时覆盖 WorkBuddy.app 和 WorkBuddy AI.app。资源校验仍通过 --ignore-resources 跳过。
- 探测仍限时 5 秒，辅助进程在签名检查前后各输出一份 JSON。若后续检查卡住或失败，父进程保留已收到的完整报告，并记录诊断 warning；没有收到有效报告才返回 unknown。业务 Context 取消规则不变，辅助进程仍被终止和回收。
- 云端 Linux 产品标记和分类不变；没有证据时不根据操作系统或本机安装的软件猜测平台。

验证覆盖：新旧 Bundle ID、身份变化与验签失败、Windows 证书变化、签名阶段超时后保留路径结果、成功后提升置信度、冲突路径保持 unknown，以及无报告超时、非法输出和进程回收。

单元测试与交叉编译不能代替 macOS / Windows 实机验收。需在 WorkBuddy 中分别使用两个 CLI 发请求，检查 detector_version=v6、来源 workbuddy 及相应置信度。

2026-09-14 官方安装包静态样本位于 `internal/invocation/testdata/installer-samples-20260914.json`，由 `TestOfficialInstallerSampleAttribution` 回放。14 份样本涵盖 9 份 Mac 包与 5 份 Windows 包／在线安装器；其中豆包 Windows 只提取了安装器声明的目标程序名。静态签名字段不等于原生验签成功。

飞书内豆包工作本地额外支持宿主父进程路径与 Office 产品标记的组合兜底，签名失效或轮换不再单独阻断识别；仅有飞书进程或仅有产品标记仍不足以识别。

2026-09-15 Windows WorkBuddy sh 断链修复：仅当已有规则仍为 unknown / none 且没有冲突标记时，使用实际祖先进程中 `.workbuddy` 或 `.workbuddy-ai` 下 `binaries/PortableGit/versions/<任意版本>/(usr/)?bin/sh.exe|bash.exe` 的完整结构作为低置信度证据。规则为 `client.workbuddy.bundled-git-ancestor`，依据为 `process_executable_path`。不以当前 CLI 或 Node 的安装目录推断来源，不扫描已安装软件，不修改 macOS / Linux 行为；已有匹配和冲突结果保持不变。样本回放覆盖两款 CLI 的 v6 断链记录；仍需 Windows sh 与 .cmd 实机业务请求验收。
