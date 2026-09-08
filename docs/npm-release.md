# EveryLine Cli npm 发布

npm 包名为 `@qfeius/everyline-cli`，终端命令和三项 Skill 名称保持不变。公共源固定为 `https://registry.npmjs.org/`。生成 `.tgz`、推送源码与发布 npm 是三个独立步骤，只有发布成功后才能通过包名安装。

## 一次性配置

1. 确认 npm 账号有 `@qfeius` 下此包的发布权限。首次发布前先检查组织权限和包名归属。
2. 在 GitHub 仓库 `qfeius/everyline-cli` 的 **Settings → Secrets and variables → Actions** 中添加 `NPM_TOKEN`。使用具有包写权限、允许非交互发布的 granular access token；按 npm 当前规则配置 Bypass 2FA，不将 token 写入仓库。
3. 将 `.github/workflows/release.yml` 推送到 GitHub。工作流使用 GitHub 自动提供的 token 创建 Release，无需另配 GitHub token。

Token 权限说明：[npm CI/CD 文档](https://docs.npmjs.com/using-private-packages-in-a-ci-cd-workflow/)。

## 发布步骤

1. 在准备发布的分支更新 `package.json.version`。正式版如 `0.0.8`，预发布版如 `0.0.8-beta.1`。已发布版本不能覆盖。
2. 执行 `make release-check`。构建脚本会同步三项 Skill 的版本；提交版本及相关改动。
3. 创建并推送与包版本完全一致的标签，例如：

```bash
git tag v0.0.8
git push github HEAD
git push github v0.0.8
```

上例中的版本须替换为本次版本。`github` 为本仓库指向 GitHub 的远端名称。

标签触发的工作流依次校验版本与 npm 登录、运行测试和安装校验、发布 GitHub 原生二进制制品、上传 npm 安装包和三项 Skill ZIP、发布 npm 包，最后回查 npm 渠道版本。正式版发布到 `latest`；含预发布后缀的版本发布到 `beta`，不覆盖 `latest`。

GitLab 流水线继续负责测试、构建和保存 `.tgz`，不重复发布 npm。

## 安装与验证

发布完成后安装正式版：

```bash
npm install -g --foreground-scripts --allow-scripts=@qfeius/everyline-cli @qfeius/everyline-cli@latest --registry https://registry.npmjs.org
everyline-cli version --output json
```

预发布版将 `@latest` 替换为 `@beta`。安装包内含六个平台的二进制，不需要再次从 GitHub 下载。

尚未发布到 npm 时，使用构建后的本地包：

```bash
make package
npm install -g --foreground-scripts --allow-scripts=@qfeius/everyline-cli "./dist/everyline-cli-<版本>.tgz"
```

`make package` 每次先将补丁版本递增一位（例如 `0.0.7 → 0.0.8`），再同步三项 Skill、重新构建六个平台的二进制并生成 `.tgz`；不会提交代码、创建 Git 标签或发布到 npm。若当前是预发布版本，按 npm 的 patch 规则转为对应正式版本。文件名中的版本以实际产物为准。日常交付使用 `make package`；`npm pack` 仅用于 CI 已固定版本的发布和内部校验，不自动升版。三个 `*-skill.zip` 是 Skill 导入包，不替代 CLI 安装包。

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

- 缺少 `NPM_TOKEN`：工作流在发布前明确报错，配置 GitHub Secret 后重跑。
- npm 403：核对 scope、包写权限、token 有效期及非交互发布权限。
- 版本已存在：不要覆盖；核对已发布结果，需要变更时提升版本并创建新标签。
- GitHub Release 已生成但 npm 发布失败：npm 包尚未发布成功；解决错误后可安装 Release 中的 `.tgz`，再处理 npm 发布。不要仅凭 Release 存在宣称 npm 已可安装。

交付文件名统一为 `everyline-cli-<版本>.tgz`，包内名称仍为 `@qfeius/everyline-cli`。固定版本打包使用 `node scripts/pack-release.js dist`；该脚本将 npm 自动生成的 scoped 文件名重命名，GitHub、GitLab 与本地打包均使用同一入口。
