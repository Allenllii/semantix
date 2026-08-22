# Spec：切片库存储引擎——追加日志 + 几何 compaction（性能重构）

> 对应：性能实测报告（2026-08-15 bench：10000 切片 lookup p50 567ms、50000 切片 2.9s 越过
> agent-skill 3s 子进程超时后静默降级；`fileStore.Put` 全量重写致连续 n 次写 O(n²)，
> 50000 切片单次 extract 2.08s）。
> 真源约束：`kernel/slice/store.go` 的 `Store` 六方法接口**不变**（Put/Get/List/UpdateStats/
> ListAll/Delete）；`NewFileStore` 签名不变；base 文件保持 v1 纯 JSONL（一行一条 Slice JSON）。
> 姊妹 spec：[slice-value-eviction.md](slice-value-eviction.md)（上限与价值淘汰，依赖本 spec
> 的 §6 共享契约）。
>
> **状态（2026-08-16）**：本文档为实现规格，先审后写。判级：Spec-Required（落盘新增
> sidecar journal 文件 + `SliceStats` 新字段）。

## 1. 目标与范围

**目标**：

1. 写路径：n 次连续写从 O(n²) 总量降到 **O(n) 总量（均摊 O(1)/次）**——追加日志 + 墓碑 +
   几何触发 compaction；
2. 读路径：`lookup`/`inject` 打开库从「3 遍全量解析 + 全部 Embedding 浮点解码」降到
   「1 遍解析 + 零浮点解码」（实测 567ms 中浮点解码占 284ms、多读字节占 119ms）；
3. 调用方零改动：六方法接口不变，新能力全部走可选接口 + 类型断言（仓库既有惯例，
   如 `closeStore` 的 `interface{ Close() error }` 断言）。

**范围内**：`kernel/slice/file_store.go` 内部重写；`kernel/slice/maintenance.go` 的
Export 断言点替换；`kernel/slice/slice.go` 加 `SliceStats.LastUsed` 与 `Embedding` omitempty；
`cmd/semantix/lookup.go` indexFromStore 一遍化；`cmd/semantix/embedder.go` hash 向量不再落盘；
`gateway/gateway.go` metaStore 批量转发。

**不在范围**：条目上限与价值淘汰（姊妹 spec）；跨进程文件锁（见 §7 单写者声明）；
bbolt 等后端替换（`store.go:3-4` 注释预留的路，接口不变时可后换）；
`serve/watch` 常驻服务（Agile 3）。

## 2. 文件布局与 journal 格式

```
<db>                     base：纯 v1 JSONL（格式零变更，human-readable，旧二进制可读）
<db>.journal             追加日志（本 spec 新增，新二进制专属）
<db>.journal.stash-<ts>  孤儿 journal 的隔离存档（可人工取证/恢复）
<db>.tmp-* 等            临时文件（沿用既有 CreateTemp + rename 原子模式）
```

journal 一行一条 JSON 对象，首行是 header：

```json
{"j":1,"bsize":1234,"bmtime":1723852800123456789,"bsha":"<hex>"}
{"op":"put","s":{ ...完整 Slice JSON，与 base 行同构... }}
{"op":"del","id":"<id>"}
{"op":"stat","id":"<id>","d":{ ...SliceStats JSON... }}
```

- `j` 是 journal 格式版本号（v1 base 没有版本号的教训在此补上）。重放遇到未知 `j` 或未知
  `op`：计入 skipped、跳过、**绝不失败**（前向兼容护栏——未来版本的记录不能让旧版本变砖）。
- **为什么是 sidecar 而不是单文件追加**：v1 读取器对一行只有两种结局——合法对象进库
  （含未知字段的对象会变成「幻影空 ID Slice」，喂给 bm25.Insert 直接报错 → lookup/gateway
  起不来）、非法行静默 skip 且被下一次全量重写永久抹掉（已删条目复活）。枚举所有单文件
  墓碑编码，旧二进制的结局只有「变砖 / 复活 / 污染检索」三选一，无安全格。sidecar 让旧
  二进制看到一个「陈旧但自洽」的纯 v1 base，降级路径干净（§8）。

## 3. 打开、内存模型与世代绑定

- **打开**：无 journal → 纯 v1 快速路径，行为与今天一致；**journal 惰性创建**，首个写操作
  才建——保证只读命令（lookup/search/dashboard）零写副作用（`dashboard.go` 的
  Stat-before-open 分支继续成立）。
- **世代绑定**：header 记录 base 的 `(bsize, bmtime, bsha)`。打开时先比 `bsize+bmtime`
  （热路径零 hash 成本）；失配 = base 被外部写者（旧二进制的全量重写 = rename，size/mtime
  必变）动过 → **孤儿处理：base 为准，journal 整体 rename 为 stash + stderr 一行警告 +
  重建**。不盲目重放：孤儿场景下写入时序未知，盲重放会让 journal 里较旧的 put 覆盖 base
  里较新的版本（stale-wins 真损坏）。compaction 崩溃窗口产生的孤儿（新 base + 旧 journal）
  内容已折入新 base，stash 恰好零丢失——同一策略覆盖两种场景。`bsha` 只用于 stash 警告
  信息与人工取证。
- **内存模型**：打开时一次加载 `entries map[string]*entry` + `order`（保持 v1 文件序语义：
  更新条目移尾）；六方法在内存上服务；**返回值一律深拷贝**（含 `Meta.Deps`/`Meta.Mtimes`
  map——v1 每次全量重解析等价于每次全新对象，共享指针会让调用方改动腐蚀缓存）；变更顺序
  统一「先 journal append + flush 成功，再改内存」。「打开即快照、进程内不见外部变更」
  正是架构文档 §4.2 冻结期要求的语义（v1 每次操作重读文件反而是弱语义）。

## 4. 崩溃安全与 fsync 策略

- **尾部撕裂**：单写者 + O_APPEND，撕裂只发生在最后一条记录；JSON 对象的任意真前缀都是
  非法 JSON → 现有逐行 skip 机制天然容忍，无需长度前缀/CRC。截尾 = 精确丢掉最后一条
  未完成记录。
- **中部损坏**（位翻转）：skip + 计数，与 v1「skip 一行 = 丢一条」同级暴露，Export 的
  skipped 计数会暴露它。
- **fsync 取舍（明示）**：每个公开写操作结束 `bufio.Flush`（数据进内核，**进程崩溃零丢失**）；
  `fsync` 只在 Close / compaction / 每累计 128 条或 1MiB 时执行。理由：darwin 的
  `File.Sync` 是 F_FULLFSYNC（10ms+ 级），逐条 fsync 会让 5 万行 Import 退化到分钟级。
  代价：**掉电**（非进程崩溃）最多丢最后 ≤128 条——切片库是可从会话 transcript 重提取的
  派生数据，且有 Export 备份通道，可接受。CLI 短进程由既有 `defer closeStore` 保证退出前
  fsync。

## 5. compaction

- **触发**：`journalOps ≥ max(1024, liveCount)` 或 `journalBytes ≥ max(4MiB, baseBytes)`，
  取先到者。**只在写操作路径、持锁内联**执行；读操作永不触发写；**无后台 goroutine/定时器**
  （冻结期契约：库的可见状态只在进程边界变化，纯折叠不改变可见状态所以允许服务期执行）。
  常量硬编码不进 config（避免再造 `retrieval.vector_dim` 式死键）。
- **步骤**：活 entry 按 order 经 DTO marshal → tmp + Chmod 0600 + Sync + rename base →
  写新 header journal（同 tmp+rename）→ 换 fd。全程复用 writeAll 既有原子模式。
  崩溃窗口：base rename 前崩 = 旧态完好；base 与 journal rename 之间崩 = 孤儿 stash
  零丢失（§3）。
- **两个入口**：`Compact()`（identity 折叠，逻辑状态不变）与
  `CompactWith(rescore func([]*Slice) []*Slice)`（淘汰钩子，姊妹 spec 消费；只在进程边界
  调用：gc 命令 / gateway 启动；本 spec 先接 identity）。
- **复杂度**：几何触发下两次 compaction 之间至少 liveCount 次 append，单次 compaction
  成本 O(live+journal) ≤ O(2·journalOps) → 均摊 O(1)/条；从空库连插 n 条总折叠成本
  Σ 2^k·J₀ ≤ 2n = **O(n)**，写放大 ≈ 2 倍。单次最坏延迟 = 终规模一次折叠（50000 条
  ≈ 0.5-1.2s，免读免解析，仅 marshal + 顺序写 + fsync），对数次且发生在写路径。
  **不做分层 LSM**：本库 ≤100MB 全量常驻内存、读放大恒 0、单层折叠停顿可接受；LSM 的
  多层级/manifest/bloom filter 换来的是破坏 base human-readable 与零依赖铁律的复杂度。
  未来体量超内存 → 接口不变整体换 bbolt。

## 6. 共享契约（姊妹 spec 依赖，先落于本 spec）

1. `SliceStats` 新增 `LastUsed int64`（json `last_used,omitempty`，unix 秒，0 = 从未使用
   或 legacy）。**合并语义 max-merge**：`cur.LastUsed = max(cur, delta)`；其余四计数仍
   delta 累加。在线 `UpdateStats` 与 journal `stat` 记录重放**共用同一个 `mergeStats`
   helper**（单一事实源）。
2. 新增可选接口 `StatsBatcher{ UpdateStatsBatch(map[string]SliceStats) error }` +
   包级 `ApplyStats(s Store, deltas)`（断言走批接口，否则逐 ID 回退 `UpdateStats`）。
   新存储实现 = k 次 O(1) append + 一次 flush；batch 内缺失 ID 跳过不报错（与单条
   UpdateStats 的 not-found error 有意分歧，因批量回写是 best-effort 原料采集）。
   gateway `metaStore` 补 `UpdateStatsBatch` 转发（复用 ApplyStats）。
3. `Close() error`：flush + fsync + 关 fd，幂等。`cmd/semantix/main.go` 与
   `gateway/gateway.go` 的既有 `interface{ Close() error }` 断言自动生效，零改动。
4. **不加 PutBatch**：journal 化后单次 Put ≈ 数十 µs（marshal 一条 + append + flush），
   extract/verify/ingest/prefetch 的逐条循环成本进入噪声区；Import 5 万条 = 5 万次
   append + 几何 compaction ≈ 秒级。扩大接口面无收益。

## 7. Embedding 处理与单写者声明

- **读侧零浮点解码**：内部 DTO `sliceDTO{...; Embedding json.RawMessage}`；`Store` 返回的
  `Slice.Embedding` 为 **nil**。安全依据：全仓库零语义读者——`search --retriever
  vector/hybrid` 每次查询从 Content 现算（`search.go:103-119`），从不读落盘向量；
  lookup/inject/verify/gateway 全走 BM25；`bm25.cloneSlice` 对 nil 天然正确，字段类型不变。
- **字节保真**：写侧与 compaction 把 `RawMessage` 原样回填——已落盘向量（含 ModelEmbedder
  产物）任意次 compaction 后逐字节不变。
- **hash 向量不再落盘**：`extract --embedder hash`（默认）不再写 `Embedding` 与
  `Meta.EmbedModel/EmbedDim`（HashEmbedder 可从 Content 确定性重算，且这三个字段只写不读）；
  仅 `--embedder model` 时持久化。`Slice.Embedding` 加 `json:",omitempty"`（nil 从
  `"Embedding":null` 变为省略，旧读兼容——缺省字段 Unmarshal 同样得 nil）。存量旧向量
  不清洗（raw 透传保留）；`gc --strip-hash-embeddings` 留作 future 项。
- **Export 无损契约**：`skippedLister` 断言点（同包未导出）替换为
  `rawExporter{ exportLines() ([][]byte, int, error) }`——Export 输出仍是**含向量的纯 v1
  JSONL**（无损备份 + 版本无关降级通道）；非 fileStore 实现回退 ListAll，行为不变。
  skipped = baseSkipped + journalSkipped。
- **单写者声明**：不引入 flock（`syscall.Flock` 仅 unix，Windows 需分叉实现，仓库已有
  Windows 权限断言分平台先例，不添新平台断裂面）。跨进程并发写的结果未定义——与 v1
  同级（v1 是 rename 竞态整写丢失）；新方案最坏是丢更新 + stash 可诊断，不更糟。
  advisory flock 列为 future hardening。

## 8. 降级路径

- `semantix gc`（触发 compaction）跑完后：journal 为空 header、base 是全量 v1 —— 旧二进制
  直接可读且零丢失（其首次写会孤儿化空 journal，stash 空文件无损）。
- `slice.Export` 行格式 → 旧二进制的 `slice.Import`：第二条通道，任何时刻可用。

## 9. 测试计划

会破需改：`TestFileStoreToleratesOversizedLine` 注释更新（坏行到 compaction 才被抹）+
新增「compaction 后坏行消失」测试接管原语义；`embedder_test` hash 路径断言（不再落向量）。
不得破坏：0600 双测试、corrupt skipped 计数、Export/Import 往返、gc 零值语义、gateway e2e。

新增：journal 重放正确性（put 覆盖 / del / stat 含 LastUsed max-merge）；compaction 等价性
（前后 ListAll 逐字段相等 + base 每行可独立按 v1 解出）；崩溃模拟（journal 任意字节截断
表驱动；新 base + 旧 journal → stash 且状态等于新 base）；旧格式兼容（大写/小写 key、
`"Embedding":null`、含向量 → Export 字节保真）；幻影/未知 op/未知 j 防御；journal 0600；
只读零副作用（不建 journal、不改 base mtime）；性能护栏（白盒 compactions 计数：5000 次
Put 的 base 重写次数 ≤ 几何界，确定性断言不用计时）；StatsBatcher 批量=逐条等价；
metaStore 转发。

## 10. 验收标准

- `go test ./... -race` + `go vet` 全绿；
- bench 复测：10000 切片 lookup p50 **≤200ms**（基线 567ms）；写操作本身均摊 **O(1)**
  ——以 Import 50000 条 **≤10s** 为实测佐证（基线按 O(n²) 推算小时级）；CLI 单次 extract
  在 50000 库上 **≤600ms**（成本已由「每条全量重写」变为「一次开库加载」，基线 2081ms）；
  hash extract 新库体积 ≈ 旧库 40%；
- 兼容冒烟：改造前二进制生成的 v1 库 → 新二进制 打开/lookup/gc/export 全通，Export 输出
  与 v1 逐条语义一致。
