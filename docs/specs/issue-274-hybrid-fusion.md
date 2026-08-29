# Spec v1 — hybrid 检索融合:等权分数平均 → RRF/可配权重(Issue #274)

> 判级:Spec-Required。本 spec 细化 Issue #274「hybrid 检索融合:等权分数平均 →
> RRF/可配权重」:融合策略单一真源下沉、weighted 权重可配、RRF 分数标度化以保持
> zone 三段分类语义、配置契约与验收标准。基线:当前 main(55240b1 + #262 rebase)。
> 待团队评审;实现前本 spec 需通过 review。

## 1. 现状审计(真源,2026-08-21)

| 事实 | 证据 |
|---|---|
| gateway hybrid 融合是**等权分数平均**,`/2` 为字面量 | `gateway/retriever.go` `fuseHits`:两路各自按本路 top-1 归一化到 [0,1](`norm` 闭包),再 `score[id] = s / 2`。无配置键、无变量 |
| 融合设计理由:归一化使 zone 绝对下限对 hybrid 成立 | `gateway/retriever.go:20-25` Score scale contract 注释 |
| **CLI search 的 hybrid 是另一条路径**:RRF(k=60 硬编码),与 gateway 不等价 | `cmd/semantix/search.go` `rrfFuse`:`1.0/(60+rank)` 求和;`--retriever hybrid` 帮助文本自称 "RRF fusion" |
| **search hybrid 的 zone 分类恒为 miss(现存缺陷)**:RRF 原始分上界 2/(60+1)≈0.033 < `AbsLow=0.45`,`zones.Classify` 的 `top1 < AbsLow → Miss` 全量命中 | `cmd/semantix/search.go:120-135`(rrfFuse 后 `zones.Classify(hit.Score, top1)`);`kernel/zone/zone.go` `Classify` |
| search hybrid 的 Hit **无 Lexical 字段**(`LexicalValid=false`),#260 词法门在该路径不生效 | `cmd/semantix/search.go` `rrfFuse` 构造 `slice.Hit{Slice, Score}`;`kernel/slice/slice.go:148-158` Hit 注释 |
| 全仓无共享 RRF 实现(`grep RRF` 仅 search 内 rrfFuse 与注释) | `git grep -i rrf` |
| 融合逻辑在 gateway 包内未导出(`fuseHits`/`hybridIndex`),kernel 侧(命令层)不可复用 | `gateway/retriever.go:113-117`;`kernel/lookup.Execute` 只接受外部注入的 `slice.Index` |
| `kernel/config` 的 `retrieval.retriever` 默认 "hybrid":search 命令消费(flag 默认值),lookup/inject 不消费(用 `deps.newIndex()` = bm25) | `kernel/config/config.go:212`;`cmd/semantix/search.go:37`;`cmd/semantix/lookup.go:55,134` |
| 词法门依赖 fused 分数中的 BM25 路贡献 | `kernel/slice/slice.go:151-158`(Lexical 语义);`kernel/cache/l3.go` `lexicalSupported`(#260) |

## 2. 目标与非目标

### 2.1 目标

```text
融合策略单一真源(kernel/fuse 共享包)
  → gateway hybridIndex + cmd search hybrid 统一走共享包
  → fusion 策略可配:weighted(现状语义 + bm25_weight) / rrf(rrf_k)
  → RRF 分数标度化到 [0,1],zone 三段分类语义保持(主要设计风险,§4)
  → Lexical 语义按策略定义,#260 词法门在两条路径均生效
  → 配置契约 + 校验 + 回归
```

### 2.2 非目标(列为后续,不进入本期)

- **百分位绝对门 / 逐条目自适应阈值**:RRF 与 zone 绝对门彻底解耦需与
  「逐条目自适应阈值」RFC 联动(issue 原文建议 3 的备选),本期用标度化保持
  现有绝对门语义(§4.3),不新造门形态;
- **kernel/lookup 命令的 hybrid 支持**:lookup 保持 bm25(harness 注入路径
  不变);共享包下沉后能力面已对称(命令层可按需接线),产品面后续 issue;
- **三路及以上融合**(语义+BM25+实体,Memo/HippoRAG 形态):本期只有两路;
- **fusion 在线自适应**(按负载/分布自动切换策略):本期只做显式配置。

## 3. 融合单一真源:`kernel/fuse`

新包 `kernel/fuse`(零第三方依赖,与 kernel 其他包同纪律):

```go
// Strategy 是 hybrid 融合策略。
type Strategy int

const (
    // Weighted 等权/加权分数平均:两路各按本路 top-1 归一化到 [0,1] 后
    // 加权平均(现状 fuseHits 语义,权重可配)。分数 ∈ [0,1]。
    Weighted Strategy = iota
    // RRF 倒数排名融合(SIGIR'09):分数 = Σ 1/(rrfK+rank),输出前按
    // rrfK 标度化到 [0,1](§4.3 数学推导)。
    RRF
)

// Config 是融合配置(零值 = 默认)。
type Config struct {
    Strategy   Strategy  // 默认 Weighted(现状行为不变)
    RrfK       int       // RRF 常数,默认 60(RRF 惯例值)
    BM25Weight *float64  // weighted 模式的 BM25 路权重;nil=默认 0.5,显式 0=纯向量
}

// Fuse 融合两路命中列表(k = 输出上限)。
// Lexical 语义按策略:Weighted → BM25 路归一化贡献(现状不变);
// RRF → QueryCoverage(词法覆盖度,与排名解耦,§4.4)。
func Fuse(bm, vec []slice.Hit, k int, cfg Config) []slice.Hit
```

- gateway `hybridIndex.Search` 改为调用 `fuse.Fuse`(构造时持有 `fuse.Config`,
  由 `newRetriever` 注入);
- `cmd/semantix/search.go` 删除 `rrfFuse`,hybrid 分支改走 `fuse.Fuse`
  (items 由 bm25.Search 结果直接提供,不再需要 byID 重建);
- `fuse.Fuse` 的 Hit 恒设置 `LexicalValid=true`(两条路径都评估词法支持)。

## 4. 分数尺度与 zone 兼容性(主要设计风险)

### 4.1 问题

`kernel/zone` 分类器是双轴:
`relative = score/top1`(相对置信度,`TauHigh/TauLow`)与
`absolute = top1`(绝对下限,`AbsHigh/AbsLow`,#259 后 per-type 表只覆盖
相对轴)。绝对门按 cosine/加权融合的有界 [0,1] 尺度调校(默认
`AbsHigh=0.7, AbsLow=0.45`)。

RRF 原始分 `Σ 1/(k+rank)` 上界 `2/(k+1)`(k=60 时 ≈0.033)——直接喂
`Classify` 则 `top1 < AbsLow` 恒成立,所有候选判 miss(§1 已确认 search
路径现存此缺陷)。

### 4.2 方案:线性标度化到 [0,1]

融合层输出前对 RRF 分数做固定线性标度:

```text
s' = s · (k+1)/2,   k = rrfK
```

性质(推导):

| 情形 | RRF 原始分 | 标度化 s' |
|---|---|---|
| 双路 rank 1 | 2/(k+1) | 1.0 |
| 单路 rank 1 | 1/(k+1) | 0.5 |
| 单路 rank r(≥1) | 1/(k+r) | (k+1)/(2(k+r)) |

> rank 从 1 起(RRF 惯例,rank = 该路内位置+1);标度化锚点由此成立:
> 双路 top → 1.0、单路 top → 0.5。

- **排序不变**:线性缩放不改变序;
- **相对置信度轴不变**:`s'/top1' = s/top1`(#259 per-type 相对表不受影响);
- **有界稳定**:s' ∈ [0,1],无 top-1 归一化的离群值扭曲问题
  (现状 `norm` 对单路 top-1 异常值敏感,issue 原文指出);
- 绝对门语义重解释(k=60,数值复核):`AbsHigh=0.7` ⇔ `Σ1/(60+r_i) ≥ 0.02295`
  ≈ 双路都进前 ~25,或一路 top-1 且另一路前 ~90;`AbsLow=0.45` ⇔
  `Σ ≥ 0.01475` ≈ 至少一路 top-8 内,或双路都 top-60 内。§1 中 search
  缺陷在此方案下消失(`0.5=单路 top1` 进入 grey 带而非 miss)。

### 4.3 边界与代价(必须知悉)

- **k 漂移**:rrfK 改变时中间排名候选的 s' 微漂移(`s'=(k+1)/(2(k+r))`),
  zone 门语义随之微变。这是可配 k 的固有代价;本期接受,文档注明;
- **权重对称**:RRF 无权重轴(排名天然加权),`bm25_weight` 在 rrf 模式下
  不生效(配置校验允许共存,文档注明);
- **绝对门不重标**:不引入百分位门;若后续需要彻底解耦,走
  「逐条目自适应阈值」RFC(非目标 §2.2)。

### 4.4 Lexical 语义(按策略,#260 兼容)

| 策略 | Lexical | 语义 | 词法门行为 |
|---|---|---|---|
| Weighted | BM25 路归一化贡献 `bmNorm`(不乘权重) | "BM25 路对该候选的支持强度",与融合权重解耦 | 现状不变 |
| RRF | `bm25.QueryCoverage(query, content)` | 词法覆盖度,与排名无关 | 纯向量候选(无 BM25 词项重合)Lexical=0 → 门拦截 ✓;BM25 路候选按覆盖度评估,不再被 RRF 的慢衰减(1/(k+r))架空 |
| bm25 单路 / vector 单路 | 现状(1 / QueryCoverage) | 不变 | 不变 |

> 现状 RRF(search 路径)无 Lexical 字段,词法门不生效——本期修复为
> `LexicalValid=true` + QueryCoverage。

## 5. 配置契约

### 5.1 gateway `[retrieval]`(toml)

```toml
[retrieval]
retriever = "hybrid"   # bm25 | vector | hybrid(现状)
fusion    = "weighted" # weighted(默认,现状行为) | rrf
rrf_k     = 60         # RRF 常数,>= 1,默认 60
bm25_weight = 0.5      # weighted 模式 BM25 路权重 ∈ [0,1],默认 0.5
```

- `RetrievalConfig` 新增 `Fusion string` / `RRfK int` / `BM25Weight float64`
  (toml 键 `fusion` / `rrf_k` / `bm25_weight`);
- `validate()` 校验:`fusion ∈ {weighted, rrf}`(空 = 默认 weighted)、
  `rrf_k >= 1`、`bm25_weight ∈ [0,1]`、NaN/Inf 拒绝(对齐 `false_hit_sim`
  校验模式);
- 零值语义:`fusion` 空 → weighted;`rrf_k` 0 → 60;`bm25_weight` 未配置
  (nil)→ 0.5。`bm25_weight` 用 `*float64`(gateway 配置中与 `LexicalFloor`
  同先例):**显式 0 是合法语义「纯向量路」,必须与「未配置」区分**,指针
  nil 即未配置。

### 5.2 CLI `search` flags

```text
--retriever bm25|vector|hybrid   (现状)
--fusion weighted|rrf            默认 weighted
--rrf-k N                        默认 60
--bm25-weight W                  默认 0.5
```

- 默认值来源:`cfgString(deps.resolved, "retrieval.fusion", "weighted")`
  等,与 `retrieval.retriever` 同模式;
- `--retriever hybrid` 与 `--fusion rrf` 组合下 zone 分类恢复正常(§4.2),
  现存"hybrid zone 恒 miss"缺陷随本 spec 修复。

### 5.3 kernel `[retrieval]` 配置面澄清

- `retrieval.retriever` 默认 "hybrid":search 命令消费(flag 默认),lookup/
  inject 不消费(注入 bm25)——**非死键,但消费面只有 search**;spec 不改
  该键默认值;
- kernel/config 同步加 `retrieval.fusion / rrf_k / bm25_weight` 三个可选键
  (mergePtr,默认值同上),供 search 命令 flag 默认值;gateway 读自己的
  toml 键,两套配置面独立(现状即如此,retriever 键同理)。

## 6. 验收标准

- [ ] **c1 兼容回归**:`fusion` 未配置/`weighted` 时,gateway hybrid 输出
  与现状 `fuseHits` 逐位一致(分数、排序、Lexical、截断 k);
- [ ] **c2 RRF 生效**:`fusion=rrf` 时 gateway hybrid 分数 ∈ [0,1],
  双路 top1 候选 = 1.0、单路 top1 = 0.5;排序 = 1/(k+r) 之和序;
- [ ] **c3 zone 语义保持**:rrf 模式下 `zones.Classify` 出现 hit/grey/miss
  三态(不再恒 miss);相对置信度轴与 per-type 表行为不变;
- [ ] **c4 权重生效**:`bm25_weight=1` → 排序 = BM25 路序;`=0` → 向量路序;
  中间值插值序;
- [ ] **c5 search 统一**:`search --retriever hybrid` 走共享 `fuse.Fuse`,
  输出 zone 三态(缺陷修复)、Lexical 字段按策略填充(`LexicalValid=true`);
  `--fusion/--rrf-k/--bm25-weight` 生效;
- [ ] **c6 配置校验**:`fusion=xxx`(非法)、`rrf_k=0/-1`、`bm25_weight=-0.1/1.5`
  、NaN/Inf 均被 `validate()` 拒绝;零值回落默认;
- [ ] **c7 verify 对照**:`verify`(或 spec 附录脚本)输出 hybrid weighted vs
  rrf 的 clear-hit/grey/miss 三段占比与命中率对照(issue 验收思路);
- [ ] **c8 词法门回归**:weighted 路径 Lexical 不变;rrf 路径纯向量候选
  (无词项重合)被 L3 词法门拦截;单路模式不受影响;
- [ ] **c9 质量门**:`go vet ./...` 干净、`go test ./...` 全绿(新增测试
  覆盖 c1-c8);Windows 平台既有 `.journal` 失败按惯例对照基线。

## 7. 测试计划(按风险放置)

| 层 | 测试 | 覆盖 |
|---|---|---|
| kernel/fuse | `TestWeightedFuse`(与现状 fuseHits 输出逐位一致)、`TestRRFFuse`(分数/排序/标度化上界 1.0 与 0.5 语义)、`TestRRFScaleInvariance`(排序不变)、`TestBM25Weight`(0/1/0.5 边界)、`TestFuseLexical`(两策略 Lexical 语义)、`TestFuseConfigZeroValue`(默认回落)、`TestFuseKTruncation` | c1-c5,c8 |
| gateway | `TestHybridFusionConfig`(配置注入与 validate 校验矩阵)、`TestHybridRRFZoneTriState`(L2 决策在 rrf 下 zone 三态)、gateway e2e(hybrid+rrf 检索链路) | c2,c3,c6 |
| cmd/semantix | `TestSearchHybridFusionFlags`(--fusion/--rrf-k/--bm25-weight 接线与 zone 三态)、`TestSearchHybridLexical` | c5,c8 |
| 对照 | `TestFuseWeightedMatchesLegacy`(gateway 现状 fuseHits 的逐位回归基线,保留旧实现为测试参照或 golden 值) | c1 |

## 8. 实施顺序(建议)

1. `kernel/fuse` 包 + 单测(weighted 先落地,与现状逐位对齐);
2. gateway `hybridIndex` 换用共享包 + `[retrieval]` 新键 + validate;
3. `search` 删除 `rrfFuse` 改走共享包 + flags;
4. rrf 标度化与 zone 三态回归(c2/c3);
5. verify 对照脚本(c7)与文档(QUICKSTART 命令表、README)。

## 9. 参考

- RRF (SIGIR'09): https://dl.acm.org/doi/10.1145/1571941.1572114
- Mem0: https://arxiv.org/abs/2504.19413 · HippoRAG: https://arxiv.org/abs/2405.14831
- `gateway/retriever.go`、`cmd/semantix/search.go`、`kernel/zone/zone.go`、
  `kernel/slice/slice.go`(当前真源)
- Issue #260(词法门)、#259(zone per-type 阈值)、#186(GW6 retriever 接线)
