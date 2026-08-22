# Spec: 全仓品牌替换 reasonix → semantix（彻底版）

- 状态：已批准（2026-08-21，会话内批准，口径=按 spec 执行）
- 判级：Spec-Required（R5 落盘格式变更 · R6 CLI/配置键变更 · R7 >300 行跨 ≥3 包）
- 背景：v0.5.0 发布版实测中间文件与运行过程仍大量显示 reasonix。
  h4-branding spec §3.1 的「不改清单」（文件系统名/配置路径/env/二进制名保留 reasonix）
  由本 spec **作废并取代**：除下述例外，全仓所有 reasonix 替换为 semantix。

## 1. 现状（2026-08-21 origin/main 1626d90）

- 出现量：reasonix 3578 / Reasonix 2231 / REASONIX 1033，约 660 个文件，96% 在 harness/。
- harness 主入口已是 `cmd/semantix-agent`（vendor 时改好），但 `scripts/release/build-full.sh`
  仍从旧 fork 路径构建 `./cmd/reasonix` 并打包 `reasonix` 二进制——这是发布版满是
  reasonix 的直接原因。
- 根模块 `semantix`，harness 同模块；仅 `harness/sdk/go` 独立模块且路径指向上游
  （`github.com/esengine/DeepSeek-Reasonix/sdk/go`）。

## 2. 替换规则（按序执行，先专名后通名）

| # | 原 | 新 | 说明 |
|---|---|---|---|
| 1 | `reasonix.toml` | `semantix-agent.toml` | harness 项目级配置。**不能**用 `semantix.toml`：与 kernel CLI 的项目配置同名冲突 |
| 2 | `reasonix-plugin.json` | `semantix-plugin.json` | 插件原生清单名；加载处保留旧名回退读取（存量插件不破） |
| 3 | `.reasonix-theme` | `.semantix-theme` | 主题扩展名；加载处保留旧扩展名兼容 |
| 4 | `.reasonix/`（项目与用户目录） | `.semantix/` | 与 kernel 共用目录；两边文件名互不冲突（harness: commands/ sessions/ 等；kernel: project.db usage.jsonl 等） |
| 5 | `REASONIX_*` env（114 个） | `SEMANTIX_*` | 已核对与 kernel 现有 SEMANTIX_* 零后缀重叠 |
| 6 | 命令调用语境 `reasonix ...`（help/文档中的 `reasonix doctor`、`$ reasonix` 等） | `semantix-agent ...` | 品牌词直改会误导用户敲成 kernel CLI 的 `semantix` |
| 7 | `github.com/esengine/DeepSeek-Reasonix/sdk/go` | `github.com/Gnosil/semantix/harness/sdk/go` | SDK 模块路径对齐本仓真实布局，go get 可达 |
| 8 | 其余 `reasonix`/`Reasonix`/`REASONIX` | `semantix`/`Semantix`/`SEMANTIX` | 大小写三联通替，含标识符、注释、测试、i18n、site |
| 9 | 文件/目录名含 reasonix（guide、plugin-example 等 6 处） | 对应改名 | git mv |

`build-full.sh` 重写：从本仓构建 `./cmd/semantix-agent` 与 `./cmd/semantix`，
打包 `semantix-agent` + `semantix` 两个二进制，移除 FORK_ROOT 依赖；
release 说明文本同步（fork 的 `reasonix.example.toml` 未 vendor 进来，
本次新增最小 `semantix-agent.example.toml` 随包分发）。

### 2.1 执行中追加的决策（实施时发现，按本 spec 精神裁定）

| 决策点 | 结论 | 理由 |
|---|---|---|
| ACP 扩展命名空间 `_reasonix.io/*`、`_meta["reasonix.io"]` | 改 `_semantix.io/*` / `"semantix.io"` | 纯协议标识不走网络，属产品自有 wire 面而非上游站点引用 |
| 插件清单 `apiVersion: reasonix.io/plugin/v2` | 规范值改 `semantix.io/plugin/v2`，解析器保留旧值兼容 | 与清单文件名回退同一策略，上游生态插件可继续安装 |
| 崩溃/遥测端点 `crash.reasonix.io` | 改 `crash.semantix.ensureok.ai`（org 自有域，暂未部署） | 产品数据不得上报上游；也绝不能指向未注册域名（semantix.io 未持有，指过去=把用户数据送给抢注者） |
| 自更新源 `esengine/DeepSeek-Reasonix` | 改 `Gnosil/semantix`；更新资产名 `semantix-agent-<os>-<arch>.tar.gz`；必需资产裁掉未发布的 windows zip | 自更新必须指向本仓 release；build-full.sh 同步产出平铺更新资产 + SHA256SUMS |
| 内嵌更新日志 `release-notes/releases.json`（上游 32 个版本史） | 整体替换为本产品自己的 v0.5.0/v0.5.1 两条记录 | 应用内「新版说明」展示的是上游历史，正是用户投诉的可见面 |
| keyring 服务名 `reasonix` | 随通替改 `semantix` | provider 密钥需重输一次，随 release notes 说明 |
| 遗留数据迁移 | 修正 §4 预想：harness 自带 legacy 迁移机制（`harness/migration/` 以 `~/.reasonix` 为源），其源路径字面量保护不改，改名后旧会话可被迁移读取 | 迁移代码的职责就是匹配旧产物 |
| 历史决策记录文档（h4-branding spec、旧报告/issue 规格） | 通替波及处不回滚，但 brand 包注释与守卫测试改为「不得泄漏上游名」语义 | 历史文件以 git 历史为准，活代码守卫必须语义正确 |

## 3. 例外清单（不替换，替换即出错）

| 类别 | 位置 | 原因 |
|---|---|---|
| 法律署名 | `harness/LICENSE.reasonix.md`（文件名+内容）、`harness/ATTRIBUTION.md` 中 "Reasonix Contributors" | MIT 版权行是法律文本，改了等于篡改署名 |
| 上游仓库标识 | 文档中 `DeepSeek-Reasonix` 仓库名/URL、`reasonix.io` 来源注释 | 指向外部真实存在物，改了链接就断 |
| 外部工具契约键 | `harness/config/ccswitch.go` 读取的 `enabled_reasonix` / `apps.reasonix` JSON 字面量 | CC Switch（第三方应用）落盘 schema，改了读不到 |
| legacy 迁移匹配串 | `harness/appidentity/` 中 `dev.reasonix.desktop`、`reasonix-launcher.exe` 等字面量 | 其职责就是识别老安装产物做清理/迁移 |
| fork 补丁 | `patches/`（2 文件） | diff 上下文对应 fork 仓文件，替换即损坏（fork 路线已弃，另行处理删除） |
| 历史报告/证据 | `docs/reports/reasonix-kvcache-mechanisms.md`（含文件名）、`artifacts/issue-194/` | 对上游的历史分析与实验证据，改名即失真 |

例外之外的文档散见提法（README、路线图、架构文档里指「我们的 harness」的 Reasonix）
一律替换；指「上游/fork 仓库」本体的保留仓库名。机械替换后逐 hunk 人工审计 docs/ 与 site/。

## 4. 落盘影响与迁移

- 新版 harness 数据目录变为 `~/.semantix`、项目目录 `.semantix/`、配置
  `semantix-agent.toml`。**不新写迁移代码**；harness 既有的 legacy 迁移机制
  （`harness/migration/`，源路径 `~/.reasonix` 字面量按例外保护）继续负责旧会话迁移。
- 存量提示：旧 `~/.reasonix` 数据（含 global-workspace）不会被新版读取；需要时手动
  `mv ~/.reasonix ~/.semantix`（注意：内含 git worktree 的话 mv 后需 `git worktree repair`；
  或不动旧目录，在新版里重新添加项目路径）。随 release notes 说明。

## 5. 验证

1. `go vet ./...` && 三个入口 `go build`（semantix / semantix-agent / semantix-gateway）
2. `go test ./... -race`（kernel/cmd/gateway 前台 + harness 后台分组）
3. 残留审计脚本：`grep -ri reasonix` 输出 == §3 例外清单，零计划外残留
4. `git diff --check`；改到 site/ → `cd site && npm run check`
5. 冒烟：隔离 HOME 下 `semantix-agent --version` + 启动写盘路径确认落 `~/.semantix`

## 6. 不做

- 不删 `patches/`、不动 `branding/`（未入 main 的本地工作）、不做数据自动迁移、
  不合并两套 toml schema、不改 kernel CLI 的任何对外名称。
