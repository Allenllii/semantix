# Spec：切片库条目上限 + 自适应价值淘汰（评分器④ / 淘汰器⑤ 落地）

> 对应：`docs/Agent-Infra-架构设计.md` §3.2 流水线 ④评分器/⑤淘汰器、§8「越用越准而非
> 越用越大」、§10 库膨胀兜底；`docs/Security-安全设计.md` :126 容量有界。现状断链：
> `Weight` 恒 1.0（`gc --min-weight` 是死判据）、`UpdateStats` 生产代码零调用者、
> `SliceStats` 缺 `LastUsed`（时效衰减无法实现）。
> 真源约束：检索排序（BM25 打分、zone 分类）**不读 Weight**——本 spec 全程维持该不变量。
> 姊妹 spec：[slice-store-append-journal.md](slice-store-append-journal.md)（存储引擎；
> 本 spec 消费其 §6 共享契约：LastUsed max-merge、StatsBatcher/ApplyStats、CompactWith）。
>
> **状态（2026-08-16）**：本文档为实现规格，先审后写。判级：Spec-Required（`SliceStats`
> 落盘字段 + 新配置键 ×4 + gc 契约变更）。

## 1. 目标与范围

**目标**：库有条目上限（默认 **5000**，用户拍板），超限按价值评分升序**归档**（非硬删）；
价值随使用自适应——命中/注入回写原料，评分离线批算，实现文档承诺的
`价值 = f(命中率, 时效衰减 exp(-λt), 用户反馈, 注入成功率)`。

**范围内**：`kernel/slice/score.go`（新）、`maintenance.go` GC 扩展、四个命中回写挂点
（CLI lookup/inject + gateway L3/L2）、`kernel/config` 三键、gateway config 一键、
gc 新 flag、dashboard 水位、example.toml/init 模板、相关文档同步。

**不在范围**：`Rejected` 的生产者（需 harness「用户回滚注入块」信号，公式已定义惩罚形态，
信号一来即生效）；`kernel/event` 的 SliceHit/SliceInject 发射（契约已有、零消费者，发了是
死代码——`docs/events.md` §4 加现状注记）；kernel/evolve 的任何改动（见 §6 边界）；
「意图相关度」项（无数据源，v1 移除不留假接口）。

## 2. 评分器（kernel/slice/score.go，纯函数）

```
active  = max(Stats.LastUsed, CreatedAt)
decay   = active>0 ? exp(-ln2 · max(0, now-active) / (halfLifeDays·86400)) : 0.5   // legacy 中性先验
use     = Hits + Injected
freq    = (use + 0.5) / (use + 3)                        // Laplace 平滑；冷启动 ≈ 0.14 而非 0
success = (Injected + 1) / (Injected + 1 + 2·Rejected)   // 一次拒绝抵两次成功注入
fb      = clamp(1 + 0.5·UserFeedback, 0.25, 2.0)         // NaN/Inf 先归 0 再算（防御）
Weight  = clamp(decay · freq · success · fb, 0.001, 1.0) // 下限 0.001 避开 Weight==0 哨兵
```

- `ScoreParams{HalfLifeDays: 30, GraceDays: 7, ...}`；`ComputeWeight(s, now, p)`；
  `Rescore(all, now, p)` —— **幂等纯函数**：同库同 now → 逐字节相同 Weight 集（确定性可测）。
- **设计取舍**：不用 EWMA（需逐事件持久化平滑态；decay-at-read 用已有计数 + 时间戳等效）；
  「命中率」降级为「使用频次饱和归一」（分母/曝光数无生产者）；`FirstSeen ≡ Slice.CreatedAt`
  （不加冗余字段，架构文档 §3.3 注明偏差）。
- **反自我强化不变量（安全）**：Weight 的消费者只有 cap 淘汰排序与 `gc --min-weight`，
  **检索与注入取舍永不读它**。L3 命中一个错误答案只会让它活得更久，不会让它被更多命中——
  Security :110 的增益回路结构上不存在；fb 上限 2.0 与 Rejected 通道是第二三道限幅。
- `--min-weight` 复活后的操作指引：0.03~0.08；0.05 ≈ 闲置约两个半衰期且从未被使用。

## 3. 命中回写（原料采集，全部 best-effort）

| 挂点 | 回写 | 说明 |
|---|---|---|
| CLI lookup（输出前） | zone==**hit** 的结果：Hits+1, LastUsed=now | grey/miss 不计——不奖励弱相关；一次 ApplyStats；失败仅 stderr，不改退出码 |
| CLI inject（Build 后，调用侧） | inj.Slices：Injected+1, LastUsed=now | 回写在 cmd 层，`Injector.Build` 保持只读（kernel 决策链无副作用） |
| gateway L3 命中（响应后） | res.SliceID：Hits+1, LastUsed=now | 走 `g.store`，失败仅 log |
| gateway L2 注入（Build 成功后、forward 前） | inj.Slices：Injected+1, LastUsed=now | 记「注入尝试」——上游可用性不是切片属性 |
| verify / search | **不回写** | 评测幂等性 / 人工浏览非复用意图 |

依赖姊妹 spec：新存储下回写 = O(1) append，不拖慢读路径；旧存储下 ApplyStats 批量
也只付一次重写（合并顺序解耦）。

## 4. 上限与淘汰器（maintenance.go 扩展）

- `GCOptions` 追加：`MaxSlices int; Archive bool(默认 true); Rescore bool; Now int64(0→time.Now)`。
  `GCResult` 追加：`Capacity int; OverCap []string; Archived int; RescoredWeights int`。
- **流程**：① Rescore 全库并批量持久化 Weight（dry-run 不持久化）→ ② 既有
  retention/min-weight 判据（**豁免规则原样保留**：CreatedAt==0 免 retention、Weight==0
  免 min-weight）→ ③ 剩余 > MaxSlices 时按四元组淘汰到恰好等于上限：

```
淘汰序（先淘汰者在前）：
  ① graceProtected 升序   // now-CreatedAt < grace_days 的排最后
  ② Weight 升序
  ③ CreatedAt 升序        // 0（unknown）视为最老 → legacy 平局先走
  ④ ID 升序               // 终极平局，逐字节可复现
```

- **cap 无豁免**（与阈值类判据的威胁模型不同：阈值是绝对差判断，legacy 没资格被判差；
  cap 是相对挤出，豁免会让上限失效）——legacy 经中性先验评分公平竞争，平局按 CreatedAt
  兜底。grace 只后置不豁免：全 grace 库仍可强制达标，上限永远可达。
- **归档**：Delete 前追加写 `<db>.archive.jsonl`（Export 同行格式 → `slice.Import`
  可整体还原，Stats/Weight 保留；0600；先归档后删，崩溃最坏产生重复行，Import 幂等无害）。
  `--no-archive` 退化硬删。**默认归档 ON 覆盖全部三条淘汰路径**（既有 gc 从硬删变
  「移动」，朝安全方向的契约变更，此处明示）。归档文件自身不设上限，v1 用户自行清理。
  「降级注入」不做——注入块本就是低权威区，以「归档 + 可还原」替代。
- **触发点与冻结期**：`semantix gc`（强制）；gateway `New()` 启动 loadIndex 前
  `CompactWith(Rescore + evict)`（强制）；**运行中/ingest 后明确不做**（架构 §4.2：库增量
  变更延迟至冻结期结束——淘汰只在进程边界生效，运行中内存索引不动）；extract 后仅
  >90% 水位 stderr 提示「run semantix gc」。

## 5. 配置与 CLI 面

| 键 | 默认 | 位置 |
|---|---|---|
| `store.max_slices` | **5000**（≥0 校验，0=不限；env `SEMANTIX_MAX_SLICES`） | kernel/config |
| `score.half_life_days` | 30（>0） | kernel/config |
| `score.grace_days` | 7（≥0） | kernel/config |
| `[store] max_slices` | 5000 | gateway/config（score 参数 gateway v1 用内置默认） |

默认 5000 依据：改造后 lookup ≈80-100ms 档（用户拍板，激进性能保护）；README 给调优表。
上限按 store 文件全局一个，不按 scope 分（project/user 本就是不同 db 文件）。
字节上限 v1 不做（`maxResultLen` 已截断内容，条目数是字节数的合理代理）。

- **gc 新 flag**：`--max-slices`（覆盖 config，0=禁用）/`--no-archive`/`--no-rescore`；
  **`--db` 默认改读 `cfgString(resolved, "store.db", "")`**（gc 需读 config 拿 max_slices，
  顺带对齐 lookup 既有模式；export/import 的同类不一致单列 issue）。JSON envelope 加
  `capacity/evicted/archived/weights_updated`（空数组非 nil 惯例）。completion 元数据同步。
- **dashboard**：📦 块加水位行 `slices N / 5000 (x%)`；JSON payload 加 `capacity`；
  0=不限时只显示计数。
- **`semantix usage` 不动**：`usage.Event` 无 slice ID，与 per-slice 评分无数据交集——
  评分原料只走 store 回写，防止未来有人想从 usage 日志反推。

## 6. 与 kernel/evolve 的边界

评分器/淘汰器落在 `kernel/slice`（架构 §3.2 的流水线 ④⑤ 属库自身维护）；
**kernel/slice 与 kernel/evolve 互不 import**（架构 §6：进化引擎只读写参数存储、不回调
决策链）。给 evolve 留的接口 = `ScoreParams` 普通 struct（未来 evolve 离线优化产物填充它，
参数单向流动），不留回调、不留信号接口。v1 不向 evolve 喂任何信号。

既有 bug 单列 issue（不在本 spec）：`usage --evolve-db` 喂的 "cost"/"latency" 不在
evolve 识别列表（`evolve.go:154-162` default 丢弃），EWMA 恒零且 params.json 零消费者。

## 7. 测试计划

新增：score_test（公式边界、legacy 中性、确定性 DeepEqual、NaN/Inf 防御、Weight 值域）；
LastUsed max-merge 与 ApplyStats 批量=逐条等价；cap 淘汰确定性（同权重平局
CreatedAt→ID 序两次运行同序）；grace 后置但 cap 仍达标；legacy 参与 cap 而
retention/min-weight 豁免仍有效；归档往返无损；--no-archive；dry-run 不写 archive
不持久化 Weight；gc CLI 新 flag + envelope + config 生效 + --db 默认；gateway config
解析 + 启动收敛 + e2e 断言 L3 命中后 Hits/LastUsed 落库、L2 后 Injected 落库；
lookup 仅 hit 区增长 + 写失败不改退出码；dashboard 水位。

会破需改：TestGCCLI/TestGCJSONEnvelope（--no-rescore 或按 stats 构造 + 新字段）；
config 全键清单（+3 键）；gateway DefaultConfig 比较（+MaxSlices）；dashboard 黄金输出。
kernel 层 gc 零值语义测试（`GC(store, GCOptions{})` 什么都不删）不改而绿。

## 8. 验收标准

- 全量测试绿；淘汰冒烟：6000 条库 → `gc --dry-run --json` 报 capacity/evicted →
  实跑归档 1000 条 → `import <db>.archive.jsonl` 可还原；
- 确定性：同库两次 gc（注入同 Now）产出逐字节相同的 archive 与 Weight；
- 文档同步落地：README 状态行（评分器/淘汰器 shipped，顺带修正超实现宣称）、架构 §3.3、
  events.md §4、Security 容量有界注、QUICKSTART gc 参数。
