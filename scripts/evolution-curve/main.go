// Command evolution-curve produces the U43 DoD evidence: two deterministic
// controlled experiments over the production prefetch/evolve/event components.
//
// Experiment 1 (stable family): proves the closed loop is wired — events
// flow, EvolutionTick fires, and the confidence knob moves in the correct
// direction. Its metric improvement comes from matrix warm-up, NOT from
// parameter evolution (the report states this explicitly).
//
// Experiment 2 (regime churn, evolve ON vs OFF): isolates the causal
// contribution of evolution. Both runs share identical priors, workload,
// and matrix-level hit/waste feedback; the ONLY delta is whether the
// EvolutionLoop retunes PrefetchConf. Under churn the evolved run stops
// wasting prefetches earlier than count-drift alone.
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	sembridge "semantix/harness/semantix"
	kernelevent "semantix/kernel/event"
	"semantix/kernel/evolve"
	"semantix/kernel/prefetch"
	"semantix/kernel/sched"
)

type row struct {
	Sequence, Hits, Waste, Ticks int
	Session                      string
	HitRate, Cost, Tau, Conf     float64
	// Markov evaluation triple (Issue #272): Coverage/Accuracy come from
	// the production matrix Stats() snapshot; LeadMs is a deterministic
	// simulated clock (seq×80 ms) proving the timeliness event→aggregate→
	// curve pipeline, not real time.
	Coverage, Accuracy, LeadMs float64
	Success                    bool
}

// Experiment 2 cost model: a wasted prefetch executes a real read-only tool
// whose output is discarded (paid work), while a hit only recovers latency
// budget — waste must cost more than a hit saves, or gating could never be
// rational (break-even accuracy = 0.006/0.011 ≈ 54.5%).
const (
	causalBase  = 0.020
	causalHit   = 0.005
	causalWaste = 0.006
)

func main() {
	out := "docs/reports/data/agile2-evolution-curve"
	reportPath := filepath.Join("docs", "reports", "agile2-evolution-curve.md")
	if len(os.Args) > 1 {
		out = os.Args[1]
		reportPath = filepath.Join(out, "agile2-evolution-curve.md")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		panic(err)
	}
	stableRows, stableEvents := runStable()
	onRows, onEvents := runCausal(true)
	offRows, _ := runCausal(false)

	must(os.WriteFile(filepath.Join(out, "sessions.jsonl"), []byte(stableEvents), 0o600))
	must(os.WriteFile(filepath.Join(out, "causal-sessions-on.jsonl"), []byte(onEvents), 0o600))
	writeCSV(filepath.Join(out, "summary.csv"), stableRows)
	writeCSV(filepath.Join(out, "causal-on.csv"), onRows)
	writeCSV(filepath.Join(out, "causal-off.csv"), offRows)
	writeReport(reportPath, stableRows, onRows, offRows)
}

// runStable is Experiment 1: the original 20-session stable task family.
func runStable() ([]row, string) {
	bus := kernelevent.NewSyncBus()
	engine := evolve.New(evolve.Config{MinSamples: 20, FreezeEpochs: 1})
	rule := sched.NewRuleDecider(sched.Config{MinSamples: 2})
	matrix := prefetch.NewMatrixPrefetcher(prefetch.Config{MinConf: 0.30, TopK: 3})
	rule.SetPrefetchPlanFunc(prefetch.AsPlanFunc(matrix))
	loop := sembridge.NewEvolutionLoop(bus, engine)
	loop.Attach(rule, matrix)
	defer loop.Close()

	for i := 0; i < 6; i++ {
		matrix.Observe("grep", "glob", true)
	}
	for i := 0; i < 4; i++ {
		matrix.Observe("grep", "read_file", true)
	}

	var raw strings.Builder
	bus.Subscribe(func(e kernelevent.Event) {
		if e.Kind == kernelevent.PrefetchHit || e.Kind == kernelevent.PrefetchWaste || e.Kind == kernelevent.EvolutionTick {
			appendEvent(&raw, e)
		}
	})
	var rows []row
	for seq := 1; seq <= 20; seq++ {
		session := fmt.Sprintf("u43-task-family-%02d", seq)
		r := playSession(seq, session, "read_file", matrix, bus, nil)
		matrix.Observe("grep", "read_file", true)
		p := loop.Params()
		r.Tau, r.Conf = p.TauL2, p.PrefetchConf
		r.Cost = 0.020 - float64(r.Hits)*0.005 + float64(r.Waste)*0.002
		rows = append(rows, r)
	}
	return rows, raw.String()
}

// runCausal is Experiment 2. Phase A (sessions 1-10) is a stable family
// whose next tool is always read_file; phase B (11-25) is regime churn:
// every session introduces a fresh next tool, so all learned candidates
// turn stale. evolveOn attaches the EvolutionLoop; the control run feeds
// the SAME matrix-level hit/waste signals by hand so the only difference
// between the runs is confidence retuning.
func runCausal(evolveOn bool) ([]row, string) {
	bus := kernelevent.NewSyncBus()
	matrix := prefetch.NewMatrixPrefetcher(prefetch.Config{MinConf: 0.30, TopK: 1})
	rule := sched.NewRuleDecider(sched.Config{MinSamples: 2})
	rule.SetPrefetchPlanFunc(prefetch.AsPlanFunc(matrix))

	var loop *sembridge.EvolutionLoop
	if evolveOn {
		engine := evolve.New(evolve.Config{MinSamples: 10, FreezeEpochs: 1})
		loop = sembridge.NewEvolutionLoop(bus, engine)
		loop.Attach(rule, matrix)
		defer loop.Close()
	}

	for i := 0; i < 4; i++ {
		matrix.Observe("grep", "glob", true)
	}
	for i := 0; i < 6; i++ {
		matrix.Observe("grep", "read_file", true)
	}

	var raw strings.Builder
	bus.Subscribe(func(e kernelevent.Event) {
		if e.Kind == kernelevent.PrefetchHit || e.Kind == kernelevent.PrefetchWaste || e.Kind == kernelevent.EvolutionTick {
			appendEvent(&raw, e)
		}
	})

	var manual *prefetch.MatrixPrefetcher
	if !evolveOn {
		manual = matrix // control run: hand-deliver matrix feedback, no engine
	}
	var rows []row
	for seq := 1; seq <= 25; seq++ {
		mode := "on"
		if !evolveOn {
			mode = "off"
		}
		session := fmt.Sprintf("u43-causal-%s-%02d", mode, seq)
		actual := "read_file"
		if seq > 10 {
			actual = fmt.Sprintf("tool-%02d", seq) // regime churn: novel every session
		}
		r := playSession(seq, session, actual, matrix, bus, manual)
		matrix.Observe("grep", actual, true)
		if loop != nil {
			p := loop.Params()
			r.Tau, r.Conf = p.TauL2, p.PrefetchConf
		} else {
			r.Conf = 0.30 // static MinConf: the control never retunes
		}
		r.Cost = causalBase - float64(r.Hits)*causalHit + float64(r.Waste)*causalWaste
		rows = append(rows, r)
	}
	return rows, raw.String()
}

// playSession plans one round and settles each planned target as hit or
// waste against the session's actual next tool. With a nil manual matrix the
// outcome is emitted on the bus (the EvolutionLoop both feeds the matrix and
// retunes parameters); with manual set, the same matrix feedback is applied
// directly and nothing reaches an engine. Each emitted event carries a
// deterministic simulated lead time (seq×80 ms) so the timeliness pipeline
// is exercised end-to-end (Issue #272).
func playSession(seq int, session, actual string, matrix *prefetch.MatrixPrefetcher, bus kernelevent.Bus, manual *prefetch.MatrixPrefetcher) row {
	tasks, err := matrix.Plan([]string{"grep"})
	if err != nil {
		panic(err)
	}
	r := row{Sequence: seq, Session: session, Success: true}
	leadMs := int64(seq * 80) // simulated clock: warm-up always finishes first
	for _, task := range tasks {
		hit := task.Key == actual
		if hit {
			r.Hits++
		} else {
			r.Waste++
		}
		if manual != nil {
			if hit {
				manual.ObserveHit(task.Key)
			} else {
				manual.ObserveWaste(task.Key)
			}
			continue
		}
		kind := kernelevent.PrefetchWaste
		payload := any(kernelevent.PrefetchWastePayload{Targets: []string{task.Key}, LeadMs: leadMs})
		if hit {
			kind = kernelevent.PrefetchHit
			payload = kernelevent.PrefetchHitPayload{Targets: []string{task.Key}, LeadMs: leadMs}
		}
		data, _ := json.Marshal(payload)
		bus.Emit(kernelevent.Event{Kind: kind, SessionID: session, Turn: 1, At: time.Date(2026, 8, 20, 0, seq, 0, 0, time.UTC), Data: data})
	}
	denom := r.Hits + r.Waste
	if denom > 0 {
		r.HitRate = float64(r.Hits) / float64(denom)
	}
	st := matrix.Stats()
	r.Coverage = st.Coverage
	r.Accuracy = st.Accuracy
	r.LeadMs = float64(leadMs)
	return r
}

func appendEvent(dst *strings.Builder, e kernelevent.Event) {
	b, _ := kernelevent.ToJSON(e)
	dst.Write(b)
	dst.WriteByte('\n')
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func writeCSV(path string, rows []row) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"sequence", "session_id", "prefetch_hit", "prefetch_waste", "hit_rate", "cost_usd", "tau_l2", "prefetch_conf", "coverage", "accuracy", "lead_ms", "task_success"})
	for _, r := range rows {
		_ = w.Write([]string{strconv.Itoa(r.Sequence), r.Session, strconv.Itoa(r.Hits), strconv.Itoa(r.Waste), f3(r.HitRate), f3(r.Cost), f3(r.Tau), f3(r.Conf), f3(r.Coverage), f3(r.Accuracy), f3(r.LeadMs), strconv.FormatBool(r.Success)})
	}
}

func sum(rs []row, f func(row) float64) float64 {
	var s float64
	for _, r := range rs {
		s += f(r)
	}
	return s
}

func mean(rs []row, f func(row) float64) float64 { return sum(rs, f) / float64(len(rs)) }

func line(rs []row, f func(row) float64) string {
	var b strings.Builder
	for i, r := range rs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(f3(f(r)))
	}
	return b.String()
}

func f3(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }

func pass(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

func writeReport(path string, stable, on, off []row) {
	var b strings.Builder
	b.WriteString("# Agile 2 自进化曲线（U43）\n\n")
	b.WriteString("> 采数类型：固定种子、固定任务族的受控 harness 会话；使用生产 `MatrixPrefetcher`、`evolve.Engine`、`EvolutionLoop` 与 event wire。可重复 DoD 证据（`go run ./scripts/evolution-curve` 逐位复现），不代表外部模型基准。\n\n")

	// ---- Experiment 1 ----
	b.WriteString("## 实验 1：闭环接线验证（稳定任务族，20 会话）\n\n")
	b.WriteString("- 任务族：每会话成功执行 `grep → read_file`；冷启动噪声先验 `grep→glob` 6 次、`grep→read_file` 4 次\n")
	b.WriteString("- evolve：`MinSamples=20, FreezeEpochs=1`（采数配置）\n")
	b.WriteString("- 成本模型：`0.020 − hit×0.005 + waste×0.002 USD`\n\n")
	b.WriteString("**定性（评审修订）**：本实验证明闭环**接线生效**——三事件正常发射入 JSONL、`EvolutionTick` 在参数变化时出现、`prefetch_conf` 于第 17-20 会话按正确方向下调（持续命中 → 放宽预取门槛，0.50→0.30）。**命中率 0.5→1.0 的提升发生在第 4 会话**（首 5 命中率 0.700）：冷启动噪声先验 `grep→glob` 需 3 次 waste 反馈才被 Beta 后验驱逐（μ < 0.25；Issue #272 先验平滑的预期行为，非回归），此后每会话只提议 `read_file`。**提升来自转移矩阵热身与 demote 收敛，与参数进化无因果**（tau 全程未动，conf 首次变化晚于命中率收敛 13 个会话）。自进化的收益证明见实验 2。\n\n")
	fh, lh := mean(stable[:5], func(r row) float64 { return r.HitRate }), mean(stable[len(stable)-5:], func(r row) float64 { return r.HitRate })
	fc, lc := mean(stable[:5], func(r row) float64 { return r.Cost }), mean(stable[len(stable)-5:], func(r row) float64 { return r.Cost })
	b.WriteString("| 指标 | 首 5 | 末 5 | 门槛 | 结果 |\n|---|---:|---:|---|---|\n")
	fmt.Fprintf(&b, "| 命中率 | %.3f | %.3f | 末 5 ≥ 首 5 | %s |\n", fh, lh, pass(lh >= fh))
	fmt.Fprintf(&b, "| 成本 USD | %.3f | %.3f | 末 5 ≤ 首 5 | %s |\n", fc, lc, pass(lc <= fc))
	fmt.Fprintf(&b, "| EvolutionTick | — | conf %.2f→%.2f | 至少一次参数变化被发射 | %s |\n\n", stable[0].Conf, stable[len(stable)-1].Conf, pass(stable[len(stable)-1].Conf != stable[0].Conf))

	// ---- Experiment 2 ----
	b.WriteString("## 实验 2：evolve 因果对照（制度性变更 churn，25 会话 × on/off）\n\n")
	b.WriteString("- 阶段 A（1-10）：稳定族 `grep → read_file`；阶段 B（11-25）：**制度性变更**——每会话出现全新后继工具，既学候选全部过期\n")
	b.WriteString("- 两组共享：相同先验、相同负载、相同矩阵级 hit/waste 反馈（对照组手动喂入）——**唯一差异 = EvolutionLoop 是否在线调 `PrefetchConf`**\n")
	b.WriteString("- 采数配置：`TopK=1, MinConf=0.30`；ON 组 `MinSamples=10, FreezeEpochs=1`\n")
	fmt.Fprintf(&b, "- 成本模型：`%.3f − hit×%.3f + waste×%.3f USD`——waste 高于 hit 节省，反映「预取错误执行了真实只读工具且产物被丢弃」（盈亏平衡精度 ≈ 54.5%%）\n\n", causalBase, causalHit, causalWaste)

	bOn, bOff := on[10:], off[10:]
	wOn, wOff := sum(bOn, func(r row) float64 { return float64(r.Waste) }), sum(bOff, func(r row) float64 { return float64(r.Waste) })
	cOn, cOff := sum(on, func(r row) float64 { return r.Cost }), sum(off, func(r row) float64 { return r.Cost })
	b.WriteString("| 指标 | evolve ON | evolve OFF | 门槛 | 结果 |\n|---|---:|---:|---|---|\n")
	fmt.Fprintf(&b, "| churn 期浪费预取（次） | %.0f | %.0f | ON < OFF | %s |\n", wOn, wOff, pass(wOn < wOff))
	fmt.Fprintf(&b, "| 25 会话总成本 USD | %.3f | %.3f | ON < OFF | %s |\n", cOn, cOff, pass(cOn < cOff))
	fmt.Fprintf(&b, "| 阶段 A 两组一致性 | %.3f | %.3f | 完全一致（对照有效） | %s |\n\n", sum(on[:10], func(r row) float64 { return r.Cost }), sum(off[:10], func(r row) float64 { return r.Cost }), pass(sum(on[:10], func(r row) float64 { return r.Cost }) == sum(off[:10], func(r row) float64 { return r.Cost })))

	b.WriteString("```mermaid\nxychart-beta\n  title \"Cost by session: evolve ON vs OFF (USD)\"\n  x-axis [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25]\n  y-axis \"USD\" 0.010 --> 0.030\n  line [")
	b.WriteString(line(on, func(r row) float64 { return r.Cost }))
	b.WriteString("]\n  line [")
	b.WriteString(line(off, func(r row) float64 { return r.Cost }))
	b.WriteString("]\n```\n\n（上线 = ON，下线 = OFF；churn 开始于第 11 会话）\n\n")
	b.WriteString("```mermaid\nxychart-beta\n  title \"PrefetchConf trajectory (ON)\"\n  x-axis [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25]\n  y-axis \"conf\" 0 --> 1\n  line [")
	b.WriteString(line(on, func(r row) float64 { return r.Conf }))
	b.WriteString("]\n```\n\n")

	// ---- Experiment 2 Markov triple (Issue #272) ----
	b.WriteString("### Markov 三指标（Issue #272，实验 2 ON 组）\n\n")
	b.WriteString("- coverage：只读调用中被预取覆盖的比例（生产 `MatrixPrefetcher.Stats()` 快照）；accuracy：预取命中率 hit/(hit+waste)（反馈事件）；timeliness：LeadMs = 消费 − 预热完成（**模拟时钟** `seq×80 ms`，确定性，验证事件 → payload → 聚合 → 曲线管线，非真实时间）\n\n")
	b.WriteString("```mermaid\nxychart-beta\n  title \"Coverage by session (experiment 2 ON)\"\n  x-axis [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25]\n  y-axis \"coverage\" 0 --> 1\n  line [")
	b.WriteString(line(on, func(r row) float64 { return r.Coverage }))
	b.WriteString("]\n```\n\n")
	b.WriteString("```mermaid\nxychart-beta\n  title \"Accuracy by session (experiment 2 ON)\"\n  x-axis [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25]\n  y-axis \"accuracy\" 0 --> 1\n  line [")
	b.WriteString(line(on, func(r row) float64 { return r.Accuracy }))
	b.WriteString("]\n```\n\n")
	b.WriteString("```mermaid\nxychart-beta\n  title \"Timeliness LeadMs by session (simulated clock)\"\n  x-axis [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25]\n  y-axis \"lead_ms\" 0 --> 2000\n  line [")
	b.WriteString(line(on, func(r row) float64 { return r.LeadMs }))
	b.WriteString("]\n```\n\n")
	b.WriteString("> 三曲线证明度量管线接通：coverage/accuracy 来自矩阵 `Stats()` 实时快照，timeliness 走 #228 事件 wire 的 `lead_ms` 字段。真实时间数据待真实会话采数（见边界声明）。\n\n")

	b.WriteString("### 机制分解（三层防线的各自贡献，2026-08-21 重采）\n\n")
	b.WriteString("churn 下止损机制按新数据分解（两组第 11-19 会话各 9 次 waste、第 20 会话起**同时停手**）：① **单目标降级 `demoted()`**（两组皆有，**本窗口主导**）：`read_file` 的 6 次先验 + 10 次命中 + 9 次 waste 后，β 后验均值 μ = (hw+1)/(hw+ww+2) ≈ 0.231 < 1/(1+3)=0.25，第 20 会话起被驱逐——#255 时间折扣让旧命中权重衰减，Issue #272 的后验均值判定取代脆弱的率比；② **evolve 全局门控**（仅 ON，本窗口无额外可观测收益）：conf 由 0.30 逐会话上调至 0.50（11-19 waste 驱动），但 `read_file` 转移概率稀释到 0.55 仍高于两组 MinConf（0.30 / 0.50），门控阈值未先于 demote 拦截；③ **计数漂移**（两组皆有，本窗口未失效）：`read_file` prob 从 0.80 稀释到 0.53，仍高于 MinConf 0.30，无自然上界。\n\n")
	b.WriteString("> 实验 2 的「ON < OFF」门槛本次为 FAIL（9 vs 9）：差异来自防线分工变化——单目标降级（#255 衰减 + Issue #272 β 后验）已足够快，抹平了 ON/OFF 行为；evolve 门控的独立收益需在「候选不被 demote、但概率缓慢稀释」的场景下单独验证。\n\n")

	b.WriteString("### 实验暴露的两个产品级发现（均已修复并验证）\n\n")
	b.WriteString("1. **闭环吸收态**（#254）：conf 抬升把预取关停后 hit/waste 事件流随之消失、参数冻结——已由 ε-escape 探针修复（`SetEpsilon`，固定随机序列 seed=1）。本窗口第 20-25 会话 conf 平直 0.500 属于「无 waste 无信号」稳态，探针存在保证该稳态可被 ε 概率打破并恢复信号。\n")
	b.WriteString("2. **`demoted()` 免疫缺陷**（#255 时间折扣 + Issue #272 β 后验）：曾有命中历史的候选 hit 权重不再冻结——waste 流逐次折扣命中权重，后验均值跌破 `1/(1+WasteHitLimit)` 即驱逐。本窗口 `read_file` 第 20 会话被驱逐、两组同时停手，即为修复生效的直接证据。\n\n")

	b.WriteString("## 数据表（实验 2，ON 组）\n\n| # | session | hit | waste | cost | conf |\n|---:|---|---:|---:|---:|---:|\n")
	for _, r := range on {
		fmt.Fprintf(&b, "| %d | %s | %d | %d | %.3f | %.3f |\n", r.Sequence, r.Session, r.Hits, r.Waste, r.Cost, r.Conf)
	}
	b.WriteString("\n## 原始证据\n\n- `docs/reports/data/agile2-evolution-curve/summary.csv`（实验 1，含 coverage/accuracy/lead_ms 列）\n- `docs/reports/data/agile2-evolution-curve/causal-on.csv` / `causal-off.csv`（实验 2，同上）\n- `docs/reports/data/agile2-evolution-curve/sessions.jsonl` / `causal-sessions-on.jsonl`（三事件 wire 流，`PrefetchHit/Waste` 事件带 `lead_ms`）\n- 重跑：`go run ./scripts/evolution-curve`（确定性，逐位一致）\n\n")
	b.WriteString("## 边界声明\n\n受控实验隔离的是调度/预取/进化机制本身，不含 LLM 质量维度；「真实会话 JSONL + `semantix search` 可检索」的验收项按 #194 顺序在合入后以真实采数补第二版（依赖 #234 的 usage 重放与 #239 的固定环境）。Issue #272 的 timeliness 曲线为**模拟时钟**（`seq×80 ms`，验证事件→聚合→曲线管线），真实 lead 数据随真实会话采数补充。\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		panic(err)
	}
}
