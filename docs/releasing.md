# 发版流程

这份文档面向维护者，描述 memo 从 `main` 到 GitHub Release 的完整流程。

核心约定：

- `main` 只接受 Pull Request，使用 **Squash merge**，保持线性历史。
- 版本号以 **tag** 为唯一来源；tag 必须打在 `main` 的提交上。
- 发布由 tag 触发的 GitHub Actions 自动完成，不设人工审批环境。
- 版本号由 `tools/release` 统一写入，构建时从 tag 读取。

## 1. 一次性仓库设置

这些设置不在代码里，需要在 GitHub 网页上完成。只需要配置一次。

### 1.1 `main` 分支规则

**Settings → Rules → Rulesets → New branch ruleset**

- Target branches：`main`
- 勾选：
  - Require a pull request before merging
  - Require status checks to pass：选择 PR 上实际出现的 CI 检查（当前 job 名是
    `Tests`；reusable workflow 在部分界面会显示成 `verify / Tests`）
  - Require branches to be up to date before merging
  - Block force pushes
  - Restrict deletions
- Bypass list 留空。维护者也走 PR，避免绕过检查。
- 同时只允许 Squash merge：**Settings → General → Pull Requests** 中关闭
  “Allow merge commits”和“Allow rebase merging”，只保留
  “Allow squash merging”。
- 打开 “Automatically delete head branches”，合并后自动删除
  `release/vX.Y.Z` 分支。

第一次配置 required check 时，`Tests` 可能还不在下拉列表里；先让一个 PR
跑一次 CI，再回来选择。

### 1.2 tag 规则

**Settings → Rules → Rulesets → New tag ruleset**

- Target tags：`v*`
- 勾选：
  - Restrict creations
  - Restrict updates
  - Restrict deletions
- Bypass list 加入维护者团队（个人仓库可加入 Repository admin）。这样只有
  维护者能创建 tag，且 tag 一旦创建就不能改指向或删除。
- 如果误操作需要删除 tag，维护者通过 bypass 操作；流程见第 7 节。

### 1.3 Actions 权限

**Settings → Actions → General → Workflow permissions** 选择
“Read and write permissions”。`release.yml` 需要 `contents: write` 才能创建
Release。

不需要配置 Environment，也不需要在 workflow 里等待人工批准：合并 release PR
并推送 tag 后直接发布。

## 2. 版本号工具

版本号分布在 15 个位置、8 个文件里：

| 文件 | 位置 |
| --- | --- |
| `build/config.yml` | `info.version` |
| `build/windows/info.json` | `file_version`、`product_version`、`FileVersion`、`ProductVersion` |
| `build/darwin/Info.plist` | `CFBundleShortVersionString`、`CFBundleVersion` |
| `build/darwin/Info.dev.plist` | `CFBundleShortVersionString`、`CFBundleVersion` |
| `build/ios/Info.plist` | `CFBundleShortVersionString`、`CFBundleVersion` |
| `internal/services/version.go` | `Version` |
| `frontend/package.json` | `version` |
| `frontend/package-lock.json` | 根 `version`、`packages[""].version` |

不要手工改这些值，使用工具：

```powershell
go run ./tools/release print               # 当前 build/config.yml 里的版本
go run ./tools/release check               # 15 个位置必须与 config 一致
go run ./tools/release set 1.4.0           # 写入全部 15 个位置
go run ./tools/release check 1.4.0 -notes  # 同时要求 docs/releases/v1.4.0.md 存在
```

`set` 只替换版本值，保留注释、缩进和键顺序，并且可以重复执行。不要为了改版本
去运行 `wails3 update build-assets`，它会重写构建资源，可能覆盖手工维护的内容。

版本格式为 `X.Y.Z`。工具允许 `-rc.1` 这类预发布后缀，但 MSI 只接受纯
`X.Y.Z`，因此当前发布流程只发布稳定版。

## 3. 发版步骤

下面的例子发布 `v1.4.0`。

### 3.1 同步并创建 release 分支

```powershell
git switch main
git pull --ff-only
git switch -c release/v1.4.0
```

### 3.2 统一版本号

```powershell
go run ./tools/release set 1.4.0
go run ./tools/release check 1.4.0
```

### 3.3 写发布说明

新建 `docs/releases/v1.4.0.md`，用中文、面向用户，建议包含：

```markdown
## 重要变更

- ...

## 新增

- ...

## 优化

- ...

## 修复

- ...
```

发布说明会被 workflow 原样放进 Release 正文，开头自动加一行项目介绍和版本号，
因此文件里不需要重复标题和版本号。缺少这个文件时，tag 发布会在 preflight
阶段直接失败，没有兜底文案。

### 3.4 本地验证

```powershell
go run ./tools/release check 1.4.0 -notes
go test ./internal/... ./tools/...
go vet ./...
gofmt -l internal tools main.go

cd frontend
npm ci
npx tsc --noEmit
npm test
npm run build
cd ..
```

### 3.5 提交并开 PR

```powershell
git add -A
git commit -m "Release v1.4.0"
git push -u origin release/v1.4.0
```

在 GitHub 上创建到 `main` 的 PR，等待 `Tests` 通过，然后 **Squash and merge**。
合并后 `release/v1.4.0` 会被自动删除。

### 3.6 在 main 上打 tag

确认 release PR 已经 squash 合并，然后：

```powershell
git switch main
git pull --ff-only
git log -1 --oneline   # 确认这确实是 release 提交

git tag -a v1.4.0 -m "memo v1.4.0"
git push origin v1.4.0
```

tag 必须打在 `main` 当前的提交上。`release.yml` 的 preflight 会检查 tag 提交
是否为 `origin/main` 的祖先；不在 `main` 上的 tag 会被拒绝，不会开始构建。

### 3.7 观察发布

打开 **Actions → Release**，会依次运行：

1. `Preflight`：解析 tag、检查是否在 `main`、检查 15 个版本位置和发布说明。
2. `Tests`：复用 `ci.yml`。
3. `Windows (MSI + NSIS)`：构建 NSIS 安装包和 MSI，版本来自 preflight。
4. `macOS (universal DMG)`：构建 Intel + Apple Silicon 通用 DMG。
5. `Publish release`：生成 `SHA256SUMS.txt`，创建 GitHub Release，并设为
   latest。

发布成功后确认：

- Release 里有 4 个资产：
  `memo-windows-amd64.msi`、`memo-windows-amd64-installer.exe`、
  `memo-macos-universal.dmg`、`SHA256SUMS.txt`。
- Release 正文包含 `docs/releases/v1.4.0.md` 的内容。
- `https://github.com/olianate/memo/releases/latest` 指向 `v1.4.0`。
- 应用内“软件更新”能看到新版本。

## 4. Workflow 的校验规则

`release.yml` 只由 `v[0-9]*` tag 或手动触发。tag 发布时：

- `VERSION` 从 tag 名去掉开头的 `v` 得到，例如 `v1.4.0` → `1.4.0`。
- tag 提交必须是 `origin/main` 的祖先。
- `go run ./tools/release check "$VERSION" -notes` 必须通过。
- MSI 使用 preflight 输出的 `VERSION`，不再从 `ref_name` 推导，也不会在版本
  不合法时静默回退到 `0.0.0`。
- `Publish release` 只在 tag 触发时运行，`make_latest: true`。

手动触发（**Actions → Release → Run workflow**）只构建安装包，不发布。可选
`version` 输入用于指定要校验的版本；留空则使用 `build/config.yml` 的版本。
手动运行不会检查 tag 是否在 `main` 上，也不会要求发布说明。

## 5. 回滚与失败处理

### 5.1 Preflight 失败

常见原因和处理：

- **tag 不在 main 上**：删除 tag，合并缺失的提交，再在 `main` 上重新打 tag。
- **版本号不一致**：在一个新 PR 里执行 `go run ./tools/release set 1.4.0`，
  合并后删除并重打 tag。
- **缺少发布说明**：补 `docs/releases/v1.4.0.md`，合并后删除并重打 tag。

删除 tag（维护者通过 tag ruleset 的 bypass）：

```powershell
git tag -d v1.4.0
git push origin :refs/tags/v1.4.0
```

不要移动已存在的 tag。删除后重新创建，指向新的 `main` 提交。

### 5.2 构建失败

先看失败 job 的日志。如果是偶发的基础设施问题，直接 **Re-run failed jobs**。
如果是代码或 workflow 问题：

1. 在 `main` 上通过 PR 修复。
2. 删除 tag，在修复后的 `main` 提交上重新打 tag。

注意：GitHub Actions 使用的是 **tag 指向的提交里** 的 workflow 文件。只在
`main` 上改了 workflow 但没有重新打 tag，重跑旧的 tag 仍然会用旧 workflow。

### 5.3 已经发布的版本有问题

不要删除或移动 tag。按严重程度处理：

- **安装包有问题但版本号可用**：重新构建并替换 Release 资产，同时更新
  `SHA256SUMS.txt`；或者直接发补丁版本。
- **版本本身有问题**：在 GitHub 上编辑该 Release，勾选 “Set as a pre-release”。
  `/releases/latest` 会忽略预发布版本，应用内更新会回退到上一个稳定版。然后
  在 `main` 上修复，发布 `v1.4.1`。
- **发布说明写错**：直接编辑 Release 正文即可，不需要动 tag；下次发版时再修正
  `docs/releases/` 里的源文件。

不要为了“覆盖”一个有问题的版本而把 tag 指向新提交；tag 规则会阻止，而且已经
下载过旧版本的用户无法被可靠覆盖。

### 5.4 发布顺序

`make_latest: true` 会把本次发布设为 latest。不要补发比当前 latest 更旧的版本，
否则 `releases/latest` 会指向旧版本，应用内更新也会跟着回退。如果确实需要，
发布后在 Release 页面手动调整 “Set as latest”。

## 6. 历史 tag

`v1.3.0` 及更早的 tag 不追溯修改。新的“tag 必须在 main 上”校验从下一个版本
开始生效。历史 tag 如果不符合新规则，不要移动它们；需要重建时按新流程发布一个
新版本。

## 7. 发布检查清单

- [ ] `main` 已同步，release 分支从最新 `main` 创建
- [ ] `go run ./tools/release set X.Y.Z` 已执行
- [ ] `docs/releases/vX.Y.Z.md` 已提交
- [ ] `go run ./tools/release check X.Y.Z -notes` 通过
- [ ] 本地 Go 测试、vet、前端测试和构建通过
- [ ] PR 通过 `Tests`，使用 Squash merge
- [ ] tag 打在 `main` 的最新提交上，使用 annotated tag
- [ ] Release workflow 全部通过
- [ ] Release 资产、正文和 latest 指向正确
- [ ] 应用内更新检查正常
