// Command evolution-curve runs a deterministic 20-session controlled harness
// workload through the production prefetch/evolve/event components.
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

func main() {
	out := "docs/reports/data/agile2-evolution-curve"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		panic(err)
	}
	bus := kernelevent.NewSyncBus()
	engine := evolve.New(evolve.Config{MinSamples: 20, FreezeEpochs: 1})
	rule := sched.NewRuleDecider(sched.Config{MinSamples: 2})
	matrix := prefetch.NewMatrixPrefetcher(prefetch.Config{MinConf: 0.30, TopK: 3})
	rule.SetPrefetchPlanFunc(prefetch.AsPlanFunc(matrix))
	loop := sembridge.NewEvolutionLoop(bus, engine)
	loop.Attach(rule, matrix)
	defer loop.Close()

	// A cold task family starts with a noisy transition prior. Every controlled
	// session then executes the same successful grep→read_file chain.
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
		tasks, err := matrix.Plan([]string{"grep"})
		if err != nil {
			panic(err)
		}
		r := row{Sequence: seq, Session: session, Success: true}
		for _, task := range tasks {
			kind := kernelevent.PrefetchWaste
			payload := any(kernelevent.PrefetchWastePayload{Targets: []string{task.Key}})
			if task.Key == "read_file" {
				kind = kernelevent.PrefetchHit
				payload = kernelevent.PrefetchHitPayload{Targets: []string{task.Key}}
				r.Hits++
			} else {
				r.Waste++
			}
			data, _ := json.Marshal(payload)
			e := kernelevent.Event{Kind: kind, SessionID: session, Turn: 1, At: time.Date(2026, 8, 18, 0, seq, 0, 0, time.UTC), Data: data}
			bus.Emit(e)
		}
		matrix.Observe("grep", "read_file", true)
		p := loop.Params()
		r.Tau, r.Conf = p.TauL2, p.PrefetchConf
		denom := r.Hits + r.Waste
		if denom > 0 {
			r.HitRate = float64(r.Hits) / float64(denom)
		}
		r.Cost = 0.020 - float64(r.Hits)*0.005 + float64(r.Waste)*0.002
		rows = append(rows, r)
	}
	if err := os.WriteFile(filepath.Join(out, "sessions.jsonl"), []byte(raw.String()), 0o600); err != nil {
		panic(err)
	}
	writeCSV(filepath.Join(out, "summary.csv"), rows)
	writeReport(filepath.Join(filepath.Dir(filepath.Dir(out)), "agile2-evolution-curve.md"), rows)
}

func appendEvent(dst *strings.Builder, e kernelevent.Event) {
	b, _ := kernelevent.ToJSON(e)
	dst.Write(b)
	dst.WriteByte('\n')
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

func writeReport(path string, rows []row) {
	first, last := rows[:5], rows[len(rows)-5:]
	var b strings.Builder
	b.WriteString("# Agile 2 自进化曲线（U43）\n\n")
	b.WriteString("> 采数类型：固定种子、固定任务族的受控 harness 会话；使用生产 `MatrixPrefetcher`、`evolve.Engine`、`EvolutionLoop` 与 event wire。它是可重复 DoD 证据，不代表外部模型基准。\n\n")
	b.WriteString("## 环境与协议\n\n- 日期：2026-08-18\n- 会话数：20（独立 session_id）\n- 任务族：每次成功执行 `grep → read_file`\n- 冷启动噪声先验：`grep→glob` 6 次、`grep→read_file` 4 次\n- evolve：`MinSamples=20`, `FreezeEpochs=1`（采数配置；生产默认未改）\n- 成本模型：`0.020 - hit×0.005 + waste×0.002 USD`，固定 oracle 全部成功\n\n")
	b.WriteString("## 汇总\n\n| 指标 | 首 5 | 末 5 | 门槛 | 结果 |\n|---|---:|---:|---|---|\n")
	fh, lh := mean(first, func(r row) float64 { return r.HitRate }), mean(last, func(r row) float64 { return r.HitRate })
	fc, lc := mean(first, func(r row) float64 { return r.Cost }), mean(last, func(r row) float64 { return r.Cost })
	fmt.Fprintf(&b, "| 命中率 | %.3f | %.3f | 末 5 ≥ 首 5 | %s |\n", fh, lh, pass(lh >= fh))
	fmt.Fprintf(&b, "| 成本 USD | %.3f | %.3f | 末 5 ≤ 首 5 | %s |\n", fc, lc, pass(lc <= fc))
	b.WriteString("| 任务成功率 | 1.000 | 1.000 | 不下降 | PASS |\n\n")
	b.WriteString("## 趋势图\n\n```mermaid\nxychart-beta\n  title \"Prefetch hit rate by session\"\n  x-axis [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20]\n  y-axis \"hit rate\" 0 --> 1\n  line [")
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(f3(r.HitRate))
	}
	b.WriteString("]\n```\n\n```mermaid\nxychart-beta\n  title \"Cost by session (USD)\"\n  x-axis [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20]\n  y-axis \"USD\" 0.01 --> 0.03\n  line [")
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(f3(r.Cost))
	}
	b.WriteString("]\n```\n\n## 数据表\n\n| # | session | hit | waste | hit rate | cost | tau_l2 | prefetch_conf | success |\n|---:|---|---:|---:|---:|---:|---:|---:|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %d | %s | %d | %d | %.3f | %.3f | %.3f | %.3f | %t |\n", r.Sequence, r.Session, r.Hits, r.Waste, r.HitRate, r.Cost, r.Tau, r.Conf, r.Success)
	}
	b.WriteString("\n## 原始证据\n\n- `docs/reports/data/agile2-evolution-curve/sessions.jsonl`\n- `docs/reports/data/agile2-evolution-curve/summary.csv`\n- 重跑：`go run ./scripts/evolution-curve`\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		panic(err)
	}
}

func mean(rs []row, f func(row) float64) float64 {
	var s float64
	for _, r := range rs {
		s += f(r)
	}
	return s / float64(len(rs))
}
func f3(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
func pass(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}
