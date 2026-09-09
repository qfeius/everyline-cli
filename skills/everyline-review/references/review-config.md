# EveryLine 审查配置管理入口

查询、创建、修改或删除审查清单、规则和规则分组时，通过宿主技能加载能力读取 `everyline-review-config`，并由它负责配置管理流程。不要假定跨 Skill 相对路径可用；尚未安装时先安装或导入该 Skill。npm 全局安装会登记两个 Skill；豆包云端需分别导入两个 ZIP。

- “用清单 A 审查合同”仍由主文件的[合同审查流程](../SKILL.md#review)处理。
- “新建或修改清单后审查合同”先完成 config 中的具体写入确认、写入与回读，再携带真实清单 ID、已确认的合同、主体和强度返回审查。审查请求本身不代表用户确认配置写入。
- 两个 Skill 复用主文件的[执行前检查](../SKILL.md#preflight)、[Profile 与身份](../SKILL.md#identity)、[授权恢复](../SKILL.md#auth-recovery)和[通用边界](../SKILL.md#boundaries)。配置管理复用公共接入时不进入合同审查；同一会话不重复检查或询问已确认输入。
- 组合任务保持同一 Profile、身份与 Device 会话上下文，等整条业务流程结束后再更新 CLI。结果未知的写入先回读，不重复提交。
