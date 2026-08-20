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
	Success                      bool
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
// directly and nothing reaches an engine.
func playSession(seq int, session, actual string, matrix *prefetch.MatrixPrefetcher, bus kernelevent.Bus, manual *prefetch.MatrixPrefetcher) row {
	tasks, err := matrix.Plan([]string{"grep"})
	if err != nil {
		panic(err)
	}
	r := row{Sequence: seq, Session: session, Success: true}
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
		payload := any(kernelevent.PrefetchWastePayload{Targets: []string{task.Key}})
		if hit {
			kind = kernelevent.PrefetchHit
			payload = kernelevent.PrefetchHitPayload{Targets: []string{task.Key}}
		}
		data, _ := json.Marshal(payload)
		bus.Emit(kernelevent.Event{Kind: kind, SessionID: session, Turn: 1, At: time.Date(2026, 8, 20, 0, seq, 0, 0, time.UTC), Data: data})
	}
	denom := r.Hits + r.Waste
	if denom > 0 {
		r.HitRate = float64(r.Hits) / float64(denom)
	}
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
	_ = w.Write([]string{"sequence", "session_id", "prefetch_hit", "prefetch_waste", "hit_rate", "cost_usd", "tau_l2", "prefetch_conf", "task_success"})
	for _, r := range rows {
		_ = w.Write([]string{strconv.Itoa(r.Sequence), r.Session, strconv.Itoa(r.Hits), strconv.Itoa(r.Waste), f3(r.HitRate), f3(r.Cost), f3(r.Tau), f3(r.Conf), strconv.FormatBool(r.Success)})
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
	b.WriteString("**定性（评审修订）**：本实验证明闭环**接线生效**——三事件正常发射入 JSONL、`EvolutionTick` 在参数变化时出现、`prefetch_conf` 于第 19-20 会话按正确方向下调（持续命中 → 放宽预取门槛）。**命中率 0.5→1.0 的提升发生在第 2 会话，来自转移矩阵热身，与参数进化无因果**（tau 全程未动，conf 首次变化晚于提升 17 个会话）。自进化的收益证明见实验 2。\n\n")
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

	b.WriteString("### 机制分解（三层防线的各自贡献）\n\n")
	b.WriteString("churn 下止损有三条独立机制，实验数据可分离：① **evolve 全局门控**（仅 ON）：纯 waste 流抬升 `PrefetchConf` 越过过期候选的转移概率，一次性关停（第 21 会话生效；EWMA 惯性带来 ~4 会话调整滞后，conf 先随阶段 A 余温降至 0.25 再爬升）；② **计数漂移**（两组皆有，**本窗口内失效**）：阶段 A 把 `grep→read_file` 概率推至 0.80，churn 稀释 15 会话后仍为 0.46 > MinConf 0.30——OFF 组浪费在窗口内没有自然上界；③ **单目标降级 `demoted()`**（两组皆有，本场景失效）：曾有命中历史的候选 hit EWMA 冻结高位，waste/hit 比值数学上限 1/hitEWMA < 3.0 门槛，**永不触发**（见「发现」）。\n\n")

	b.WriteString("### 实验暴露的两个产品级发现（随本 PR 立 issue 跟进）\n\n")
	b.WriteString("1. **闭环吸收态**：conf 抬升把预取全部关停后，hit/waste 事件流随之消失，引擎再无信号、参数永久冻结——churn 结束后 ON 组无法自动恢复预取（本实验 conf 轨迹尾段平直即为证据）。需要 ε-探索或 conf 时间衰减作为逃逸机制。\n")
	b.WriteString("2. **`demoted()` 免疫缺陷**：hit EWMA 只在命中时更新，纯 waste 流下冻结不衰减；`WasteHitLimit=3.0` 时任何 hit EWMA > 1/3 的历史优等生永不降级。单目标防线对「曾经好用、现已过期」的候选失效。\n\n")

	b.WriteString("## 数据表（实验 2，ON 组）\n\n| # | session | hit | waste | cost | conf |\n|---:|---|---:|---:|---:|---:|\n")
	for _, r := range on {
		fmt.Fprintf(&b, "| %d | %s | %d | %d | %.3f | %.3f |\n", r.Sequence, r.Session, r.Hits, r.Waste, r.Cost, r.Conf)
	}
	b.WriteString("\n## 原始证据\n\n- `docs/reports/data/agile2-evolution-curve/summary.csv`（实验 1）\n- `docs/reports/data/agile2-evolution-curve/causal-on.csv` / `causal-off.csv`（实验 2）\n- `docs/reports/data/agile2-evolution-curve/sessions.jsonl` / `causal-sessions-on.jsonl`（三事件 wire 流）\n- 重跑：`go run ./scripts/evolution-curve`（确定性，逐位一致）\n\n")
	b.WriteString("## 边界声明\n\n受控实验隔离的是调度/预取/进化机制本身，不含 LLM 质量维度；「真实会话 JSONL + `semantix search` 可检索」的验收项按 #194 顺序在合入后以真实采数补第二版（依赖 #234 的 usage 重放与 #239 的固定环境）。\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		panic(err)
	}
}
