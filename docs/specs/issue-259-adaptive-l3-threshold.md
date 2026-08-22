# Spec v1 — L3 命中阈值:全局硬编码 → 按类型分化 → 逐条目自适应(Issue #259)

> 判级:Spec-Required。本 spec 细化 Issue #259「L3 命中阈值:全局硬编码 →
> 按类型分化 → 逐条目自适应(vCache)」,把 RFC 的三阶段拆成可独立验收的
> 实现边界。基线为当前 main(99a22d7)。阶段 3 的误命中观测通道已在
> Issue #262 落地(80b76bc/b16b9e1/335797f),本 spec 直接消费,不再重复造。

## 1. 目标与非目标

### 1.1 现状缺口(真源审计,2026-08-22)

| 缺口 | 证据 |
|---|---|
| 四参数全局硬编码 | `kernel/zone/zone.go:49-51` `Default()` 写死 TauHigh 0.8 / TauLow 0.55 / AbsHigh 0.7 / AbsLow 0.45;`Classify`/`ClassifyL3` 只消费这一个快照 |
| gateway 无任何阈值配置键 | `gateway/gateway.go:162` `z := zone.Default()`;`gateway/config.go` `RetrievalConfig` 只有 retriever/top_k/budget/vector_dim/fusion/rrf_k/bm25_weight,无 tau 键 |
| gateway 不接 evolve | `--evolve-db` 只接在 CLI(`cmd/semantix/flags.go:49` `applyEvolveParams`),gateway 启动路径无 evolve 状态读取 |
| CLI 配置层无 tau 键 | `kernel/config/config.go:51-58` `fileRetrieval` 无 tau 键;`semantix.example.toml` 无阈值节 |
| 切片类型不参与阈值选择 | P/C/T/R/M(`kernel/slice/slice.go:10-20`)共用一套常量;L3 判定在 `kernel/cache/l3.go:185` 单点 `z.ClassifyL3(...)` |
| evolve 只调 TauL2 一个参数 | `kernel/evolve/evolve.go:221-264` `maybeAdjustLocked` 只动 TauL2 与 PrefetchConf;`Params.TauL3`(evolve.go:52)有字段无调整 |
| 逐条目无自适应 | L3 决策无 per-slice 阈值状态;误命中反馈(#262)只做统计与绕过,不反哺阈值 |

### 1.2 本期范围

```text
阶段 1 可配化:四参数进 gateway [retrieval] 与 CLI semantix.toml;
              gateway 接 evolve tuned TauL2(显式配置优先)。
阶段 2 按类型分化:zone.Zones.ForType(typeName) 覆盖;P/C/T/R/M 可配独立阈值;
              verify --calibrate 增加 per-type 分布(校准依据)。
阶段 3 逐条目自适应:kernel/adapt 新包,每个高频复用切片维护正/负反馈,
              在线估计条目级 τ_low,全局参数作先验/冷启动;
              gateway 消费:复用→正,疑似误命中/judge 拒绝→负;
              verify --calibrate 报告 adapt 状态。
```

### 1.3 非目标(列为后续,不进入本期)

- TauHigh/AbsHigh/AbsLow 的逐条目学习(只学 TauLow 一个旋钮,与 evolve
  语义对齐;abs 下限是绝对尺度安全网,不该被单条目污染);
- harness/Reasonix 侧真实误命中信号(仍只有 #262 的疑似口径,报告必须标注);
- `kernel/event` 新增自适应事件 Kind(状态文件 + gateway 日志已够);
- CLI lookup/search 消费 adapt 状态(本期只做 gateway 生产路径 + CLI 只读报告);
- 分型初值的自动离线校准(verify --calibrate 输出 per-type 分布,由 operator
  决策;默认不分化,行为零变化)。

## 2. 语义设计(单一真源)

### 2.1 阈值解析顺序(阶段 1)

```text
gateway:  显式 tau_* 配置 > evolve tuned TauL2 > zone.Default()
CLI:      --tau-* flag > semantix.toml [retrieval] > zone.Default()
```

evolve 值只覆盖 TauLow,且 clamp 到 [evolve.DefaultMinTau, evolve.DefaultMaxTau]
(0.30–0.80),与 CLI `applyEvolveParams` 现有行为一致(flags.go:70-76)。

### 2.2 per-type 覆盖语义(阶段 2)

- `zone.Zones` 增加 `ByType map[string]Zones`,key 是 `slice.SliceType.String()`
  的稳定 wire 名(`prompt|context|tool_pattern|result|memory`)。zone 包不依赖
  slice 包,以 string 解耦。
- `ForType(typeName string) Zones`:命中 `ByType` → 该覆盖(完整四参数);
  未命中/空 → 返回全局基线自身。覆盖是**完整快照**,不支持部分覆盖——
  每个类型要么整组覆盖,要么用全局(避免"部分继承"的隐式语义)。
- 配置端(toml)是部分覆盖语法(只写要改的字段),组装为完整快照时未写字段
  继承全局值——语法宽松、语义严格。
- 默认 `ByType` 为空 → 全类型全局基线,行为与现状逐字节一致。

### 2.3 逐条目自适应语义(阶段 3,vCache 路线)

- **学什么**:每个条目的 `TauLow`(grey-zone floor,即"验证预算旋钮")。
  全局 TauLow 作先验/冷启动;条目学到的值只在该条目被判定时覆盖 TauLow。
- **信号**(复用 #262 通道):
  - 正:该 slice 被 L3 复用并服务(未触发重试);
  - 负:同 session 对已复用 query 的重试(#262 疑似误命中)→ 该 slice 负反馈;
  - 负:judge 判定拒绝(`JudgeDeclined`)→ 该 slice 负反馈(grey 区被拒说明
    该条目在相似邻域易误判)。
- **估计**:per-entry 维护负样本 EWMA(alpha=0.1,与 evolve 一致)。
- **调整规则**(对齐 evolve 冻结窗思想):
  - 样本 ≥ MinSamples 且负 EWMA > ErrorBound → TauLow += Step(收紧);
  - 样本 ≥ MinSamples 且负 EWMA < ErrorBound/2 且正样本 ≥ MinSamples →
    TauLow -= Step(放宽);
  - clamp 到 [MinTau, min(MaxTau, 全局 TauHigh − 0.05)](保证 grey 区非空);
  - 每次调整后冻结 FreezeEpochs 次观察(防抖 + 保护注入字节稳定)。
- **冷启动**:条目复用次数 < MinHits 或样本 < MinSamples → 返回全局 TauLow,
  零行为变化。
- **持久化**:`<slice store 同目录>/l3-adapt.json`,启动加载、变更即写;
  文件损坏/缺失 → WARN + 空状态(自适应状态可重建,不是数据真相)。
- **开关**:默认启用,`adaptive = false` 或 `error_bound = -1` 关闭(行为回退
  全局阈值)。

## 3. 阶段 1 接线:四参数可配化

### 3.1 gateway 配置与组装

`gateway/config.go` `RetrievalConfig` 新增:

```go
TauHigh   *float64 `toml:"tau_high"`   // nil → zone.Default()
TauLow    *float64 `toml:"tau_low"`
AbsHigh   *float64 `toml:"abs_high"`
AbsLow    *float64 `toml:"abs_low"`
EvolveDB  string   `toml:"evolve_db"`  // 可选 evolve state 目录;tuned TauL2 → TauLow
```

- `validate()`:tau 须 (0,1] 且 tau_high > tau_low;abs 须 ≥ 0;NaN/Inf 拒绝。
- 新增 `(*Config) zoneConfig() (zone.Zones, error)`:显式 tau_* > evolve
  (仅 TauLow) > Default。evolve 值 clamp 到 [0.30, 0.80]。
- `gateway/gateway.go:162` 改为调用 `cfg.zoneConfig()`。

### 3.2 CLI 配置层

`kernel/config/config.go` `fileRetrieval` 新增四 pointer 键
(`tau_high/tau_low/abs_high/abs_low`),走既有 `mergePtr` 机制
(flag > env > file > default),`validate()` 加值域规则(与 gateway 同规则)。

`cmd/semantix/flags.go` 新增 `applyConfigZones(c *config.Resolved)`:显式 flag
优先,否则 toml 值覆盖内置默认;调用点在 `eval.go` / `lookup.go`(×2) /
`search.go` / `verify.go` 的 parse 与 `applyEvolveParams` 之间(优先级:
flag > toml > evolve > default——与 issue-01 的 flag 语义保持兼容)。

### 3.3 文档

`semantix.example.toml` 增加 `[retrieval]` 四键注释示例;gateway 示例配置
(如有)同步。

## 4. 阶段 2 接线:按类型分化

### 4.1 kernel/zone

`Zones` 增加字段与方法:

```go
type Zones struct {
    TauHigh, TauLow, AbsHigh, AbsLow float64
    ByType map[string]Zones          // per-type overrides (wire-name keyed)
}

func (z Zones) ForType(typeName string) Zones
```

`Default()` 返回 `ByType: nil`。`Classify`/`ClassifyL3` 签名不变(仍是全局
语义);per-type 由判定点先 `ForType` 再判。

### 4.2 判定点接线

- `kernel/cache/l3.go` `DecideL3`:候选循环内
  `z.ForType(s.Type.String()).ClassifyL3(...)`(s 非 nil 已由候选过滤保证);
- `kernel/cache/l3.go` `DecideL2`:`h.Slice != nil` 时 ForType,否则全局;
- `kernel/inject/inject.go` `Build`:`in.Zones != nil` 且 `h.Slice != nil` 时
  ForType 后 Classify。

### 4.3 配置

- gateway `RetrievalConfig.ByType map[string]zoneOverride`,toml:

```toml
[retrieval.by_type.result]
tau_high = 0.85
tau_low  = 0.60
abs_high = 0.75
abs_low  = 0.50
```

`zoneOverride` 四字段皆 pointer(部分覆盖);未知 type key → validate 报错
(fail-closed);组装完整 `zone.Zones.ByType` 时未写字段继承全局。

- CLI `kernel/config`:`fileRetrieval.ByType map[string]fileZone`,平铺键
  `retrieval.by_type.<type>.tau_high` 等;`flags.go` 消费组装(优先级同 3.2)。

### 4.4 verify --calibrate 分型报告

`verify --calibrate`(cmd/semantix/verify.go:403 现有报告)增加 per-type
分解:命中/误命中/grey 占比按 `prompt|context|tool_pattern|result|memory`
分列——为 operator 决定分化值提供回放依据(对应 issue 的"用 verify 回放集
分型校准初值")。

## 5. 阶段 3 接线:逐条目自适应(vCache)

### 5.1 新包 kernel/adapt

```go
package adapt

type Config struct {
    ErrorBound    float64 // 用户指定误命中率上界;默认 0.05;-1 关闭
    Step          float64 // 每次调整步长;默认 0.05(对齐 evolve.TauStep)
    MinSamples    uint64  // 首调最小样本;默认 20(对齐 evolve.MinSamples)
    MinHits       uint64  // 条目成为高频的最小复用数;默认 5
    FreezeEpochs  uint64  // 调整后冻结观察数;默认 60(对齐 evolve.DefaultFreezeEpoch)
    Alpha         float64 // 负样本 EWMA 平滑;默认 0.1
    MinTau        float64 // 默认 0.30(对齐 evolve.DefaultMinTau)
    MaxTau        float64 // 默认 0.80(对齐 evolve.DefaultMaxTau)
}

type Entry struct {
    SliceID   string  `json:"slice_id"`
    TauLow    float64 `json:"tau_low"`   // 学习值(未学习 → 0,消费端回退全局)
    Pos       uint64  `json:"pos"`       // 正反馈计数
    Neg       uint64  `json:"neg"`       // 负反馈计数
    NegEWMA   float64 `json:"neg_ewma"`  // 负样本率 EWMA
    Frozen    uint64  `json:"frozen"`    // 剩余冻结观察数
    UpdatedAt int64   `json:"updated_at"`
}

type Engine struct { /* 内部:mu + entries map + cfg + path */ }

func New(cfg Config, path string) (*Engine, error) // 加载既有状态;损坏 → WARN + 空
func (e *Engine) Observe(sliceID string, negative bool)
func (e *Engine) TauLow(sliceID string, global float64) float64 // 未学习/未高频 → global
func (e *Engine) Snapshot() []Entry // 报告用,按 SliceID 排序
func (e *Engine) Adjustments() uint64
```

调整只在 `Observe` 内触发;`TauLow` 是纯查询(读锁)。持久化:变更即写
(Observe 导致计数或阈值变化后落盘,带锁;写失败只记日志,不阻断决策路径)。

### 5.2 gateway 接线

- `gateway/config.go` `RetrievalConfig` 新增 `Adaptive *bool`(nil → true)、
  `ErrorBound float64`(0 → 0.05;−1 关闭)、`AdaptDB string`(空 → `<store
  db 目录>/l3-adapt.json`);validate 值域。
- `gateway/gateway.go` `New`:构造 `adapt.Engine`(关闭时 nil),注入
  `decider.Adapt` 与 gateway 反馈钩子。
- `kernel/cache/l3.go` `L3Decider` 新增:

```go
// Adapt 提供条目级 TauLow 覆盖(nil 安全;不改变其他判定语义)。
Adapt interface {
    TauLow(sliceID string, global float64) float64
}
```

`DecideL3` 候选循环内:`tz := z.ForType(...); if d.Adapt != nil {
tz.TauLow = d.Adapt.TauLow(s.ID, tz.TauLow) }` 后再 `ClassifyL3`。

- 反馈接线(gateway/pipeline.go 与 judge 观测回调):
  - 正:复用服务路径(现有 l3Reuses 记录处)→ `adapt.Observe(sliceID, false)`;
  - 负:同一 session 重试命中(`detectFalseHit` 返回 true)→
    `adapt.Observe(entry.SliceID, true)`;
  - 负:judge 拒绝(`observeJudge`,Verdict == JudgeDeclined)→
    `adapt.Observe(obs.SliceID, true)`。
  - 调整事件:`log.Printf("adapt: slice %s tau_low %.2f -> %.2f (neg_ewma %.3f)")`
    (观测可查,不进 usage 日志,避免污染计费账)。

### 5.3 CLI 只读报告

`verify --calibrate` 增加 adapt 快照汇总(条目数、阈值分布 min/median/max、
调整次数、负 EWMA 分布);`--adapt-db` flag 指定状态文件(默认 `<store db
同目录>/l3-adapt.json`)。

## 6. 配置示例

```toml
[retrieval]
tau_high = 0.8        # 相对置信:明确 hit 下界(缺省 0.8)
tau_low  = 0.55       # 相对置信:grey 下界(缺省 0.55)
abs_high = 0.7        # 绝对分:明确 hit 地板(缺省 0.7)
abs_low  = 0.45       # 绝对分:grey 地板(缺省 0.45)
evolve_db = ""        # evolve state 目录;tuned TauL2 → tau_low(显式 tau_low 优先)
adaptive = true       # 逐条目自适应开关(缺省 true)
error_bound = 0.05    # 条目级误命中率上界;-1 关闭自适应
adapt_db = ""         # 自适应状态文件(缺省 <db 同目录>/l3-adapt.json)

[retrieval.by_type.result]   # per-type 覆盖:只写要改的字段,其余继承全局
tau_high = 0.85              # R 切片直接回答用户 → 更严
tau_low  = 0.60
# [retrieval.by_type.context]  # C/P 仅做上下文 → 可放宽
# tau_low = 0.50
```

## 7. 验收标准

- [x] 阶段 1(gateway):`[retrieval] tau_*` 配置改变 L3 判定(gateway 配置
  单测 + kernel/cache 判定单测);`evolve_db` 的 tuned TauL2 在无显式 tau_low
  时生效,显式配置优先,clamp 到调优带(单测)。
- [x] 阶段 1(CLI):semantix.toml 四键解析 + 值域/交叉校验;
  flag > toml > evolve > default 优先级单测;非法值(τ 超界、
  tau_high ≤ tau_low)启动报错。
- [x] 阶段 2:per-type 配置后,同分数不同 Type 判定不同(zone 单测 +
  gateway 配置组装单测 + kernel/cache 判定单测);未知 type key 启动失败;
  ByType 为空时所有现有测试行为不变。
- [x] 阶段 3:adapt 单测覆盖——正反馈放宽/负反馈收紧、clamp、冻结窗、
  冷启动(样本/复用不足回退全局)、持久化 round-trip、损坏文件恢复、
  `adaptive=false` 零行为变化。
- [x] 阶段 3(gateway):e2e 复用 → 重试 → 该条目 TauLow 提高且落盘;
  judge 拒绝计入负反馈。
- [x] verify --calibrate 输出 per-type 分布与 adapt 汇总(快照断言)。
- [x] 回归:`go test ./...` 全绿;默认配置下行为与 main 一致
  (ByType 空、adapt 冷启动 → 全局阈值)。

## 8. 参考

- vCache(arXiv:2502.03771,ICLR 2026):per-entry 在线阈值学习 + 错误率上界;
- Calibration Gap(arXiv:2606.19719):阈值必须按部署分布校准;
- Online Adaptation CUCB-SC(arXiv:2508.07675):错配代价建模;
- Issue #262 spec(docs/specs/issue-262-l3-negative-observability.md):误命中
  观测与反馈通道;
- Issue #01(docs/issues/issue-01-l3-三段阈值.md):三段阈值验收基线。
