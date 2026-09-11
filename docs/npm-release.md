# EveryLine Cli npm 发布

npm 包名为 `@qfeius/everyline-cli`，终端命令和两项 Skill 名称保持不变。公共源固定为 `https://registry.npmjs.org/`。生成 `.tgz`、推送源码与发布 npm 是三个独立步骤，只有发布成功后才能通过包名安装。

本分支固定使用 blue 业务环境。blue 与 release 的安装、检查更新和发布统一使用 `latest` dist-tag，共用同一套递增编号；版本使用 `主版本.次版本.补丁版本`，例如 `0.1.11`，不再添加环境后缀。npm 渠道和版本号不表示业务环境，实际业务仍按 Profile 连接地址校验。npm 版本在整个包名下唯一，发布流程应依次执行，避免两次并行打包选中同一个尚未发布的版本。`latest` 渠道尚未发布或查询失败时，检查结果为未知，保留现有安装。

## 一次性配置

1. 确认 npm 账号有 `@qfeius` 下此包的发布权限。首次发布前先检查组织权限和包名归属。
2. npm 使用已配置的 GitHub OIDC trusted publisher，绑定 `qfeius/everyline-cli` 的 `npm-publish.yml`；发布工作流保留 `id-token: write` 权限。
3. 将两条发布工作流推送到 GitHub。`release.yml` 使用 GitHub 自动提供的 token 创建 Release 并上传制品；`npm-publish.yml` 单独发布 npm，避免两个任务争抢同一版本。

发布授权说明：[npm trusted publishing 文档](https://docs.npmjs.com/trusted-publishers/)。

## 发布步骤

1. 在 blue 分支执行 `node scripts/package-version.js --bump`，或使用包含升版步骤的 `make package`。脚本查询 npm 全部已发布版本，与本地版本比较后递增补丁号；保留 `publishConfig.tag=latest`。已发布版本不能覆盖。
2. 执行 `make release-check`。构建脚本会同步两项 Skill 的版本；提交版本及相关改动。
3. 创建并推送与包版本完全一致的标签，例如：

```bash
git tag v0.1.11
git push github HEAD
git push github v0.1.11
```

上例中的版本须替换为本次版本。`github` 为本仓库指向 GitHub 的远端名称。

同一标签触发两条工作流；两者都校验已提交版本、`latest` 渠道、Windows 打包和完整发布检查。`release.yml` 发布 GitHub Release 制品、npm 安装包和两项 Skill ZIP；`npm-publish.yml` 通过 OIDC 发布 npm 包并回查 `latest` 渠道。blue 与 release 的 npm 发布始终使用 `--tag latest`，发布成功后 `latest` 指向本次版本。

GitLab 流水线继续负责测试、构建和保存 `.tgz`，不重复发布 npm。

## 安装与验证

发布完成后安装 `latest` 渠道最新版：

```bash
npm install -g --foreground-scripts --allow-scripts=@qfeius/everyline-cli @qfeius/everyline-cli@latest --registry https://registry.npmjs.org
everyline-cli version --output json
```

安装包内含六个平台的二进制，不需要再次从 GitHub 下载。新版本检查更新查询 `latest` 渠道，发布目标使用不带后缀的正式版本。历史客户端若仍查询 `blue`、返回 `@blue` 更新目标或因旧版本校验显示未知，使用上述 `@latest` 安装命令升级，并保留原 npm prefix。安装完成后仍按实际连接地址核对 blue Profile。

尚未发布到 npm 时，使用构建后的本地包：

```bash
make package
npm install -g --foreground-scripts --allow-scripts=@qfeius/everyline-cli "./dist/everyline-cli-<版本>.tgz"
```

`make package` 取本地及 npm 全部已发布版本中最高的基础版本，再将 patch 加一；历史预发布版本参与比较时去掉后缀。例如 blue 已发布 `0.1.11`，即使 release 本地仍为 `0.1.8`，下次也会生成 `0.1.12`。查询失败时停止，保留本地版本。升版后同步两项 Skill、重新构建六个平台的二进制并生成 `.tgz`；不会提交代码、创建 Git 标签或发布到 npm。文件名中的版本以实际产物为准。日常交付使用 `make package`；`npm pack` 仅用于 CI 已固定版本的发布和内部校验，不自动升版。两个 `*-skill.zip` 是 Skill 导入包，不替代 CLI 安装包。

## 从旧包名迁移

仅当 `npm ls -g --depth=0` 确认已安装旧 npm 包 `everyline-cli` 时使用以下步骤。新用户直接安装 scoped 包，无需 `--force`。所有命令使用同一个 npm 全局目录；旧包使用自定义 prefix 时，每条命令均追加相同的 `--prefix`。

```bash
npm install -g --force --foreground-scripts --allow-scripts=@qfeius/everyline-cli @qfeius/everyline-cli@latest --registry https://registry.npmjs.org
everyline-cli --help
npm uninstall -g everyline-cli
npm rebuild -g --foreground-scripts --allow-scripts=@qfeius/everyline-cli @qfeius/everyline-cli
everyline-cli version --output json
```

第一步的 `--force` 仅用于接替旧包占用的同名命令。先确认新包安装和 Skill 迁移成功，再卸载旧包；卸载会移除同名命令入口，因此必须随后 rebuild 新包以恢复入口。保留原配置目录和凭据。任一步失败先处理该错误，不继续卸载或报告成功。尚未发布时，第一步的包名可替换为实际 `.tgz` 路径。

## 发布失败

- OIDC 授权失败：核对 npm trusted publisher 绑定的仓库、工作流文件和 GitHub `id-token: write` 权限。
- npm 403：核对 scope、包写权限、token 有效期及非交互发布权限。
- 版本已存在：不要覆盖；核对已发布结果，需要变更时提升版本并创建新标签。
- GitHub Release 已生成但 npm 发布失败：npm 包尚未发布成功；解决错误后可安装 Release 中的 `.tgz`，再处理 npm 发布。不要仅凭 Release 存在宣称 npm 已可安装。

交付文件名统一为 `everyline-cli-<版本>.tgz`，包内名称仍为 `@qfeius/everyline-cli`。固定版本打包使用 `node scripts/pack-release.js dist`；该脚本将 npm 自动生成的 scoped 文件名重命名，GitHub、GitLab 与本地打包均使用同一入口。
