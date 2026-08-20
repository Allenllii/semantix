package agent

// U42 (issue #193) scheduling demo driver: the same task family runs twice —
// kernel decision ON (kernel/sched.RuleDecider, the production default) vs
// OFF (an empty-plan decider that degrades executeBatch to the static
// read-only grouping, no suspension enforcement, no tier, no budget hints) —
// and every RoundPlan plus per-round metrics land as JSON evidence for
// docs/reports/agile2-scheduling-demo.md.
//
// The driver is excluded from normal CI runs: it only executes when
// SEMANTIX_SCHED_DEMO=1 (wall-clock assertions on sleeping tools would be
// flaky on loaded runners, and the point is evidence generation, not a gate).
// Reproduce with:
//
//	SEMANTIX_SCHED_DEMO=1 SEMANTIX_SCHED_DEMO_OUT=docs/reports/data/agile2-scheduling-demo \
//	  go test ./harness/agent/ -run TestU42SchedulingDemo -v

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"semantix/harness/event"
	"semantix/harness/provider"
	"semantix/harness/tool"
	kernelevent "semantix/kernel/event"
	"semantix/kernel/sched"
)

// demoGauge tracks live tool-execution concurrency across one round.
type demoGauge struct {
	current atomic.Int32
	peak    atomic.Int32
}

func (g *demoGauge) enter() {
	n := g.current.Add(1)
	for {
		old := g.peak.Load()
		if n <= old || g.peak.CompareAndSwap(old, n) {
			return
		}
	}
}

func (g *demoGauge) exit()      { g.current.Add(-1) }
func (g *demoGauge) resetPeak() { g.peak.Store(0) }
func (g *demoGauge) peakN() int { return int(g.peak.Load()) }

// demoTool is a read-only tool with a configurable execution delay and a
// switchable failure mode (for the behavior-gate warmup).
type demoTool struct {
	name    string
	delay   time.Duration
	failing atomic.Bool
	runs    atomic.Int32
	gauge   *demoGauge
}

func (t *demoTool) Name() string            { return t.name }
func (t *demoTool) Description() string     { return "u42 demo tool" }
func (t *demoTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *demoTool) ReadOnly() bool          { return true }
func (t *demoTool) Execute(context.Context, json.RawMessage) (string, error) {
	t.gauge.enter()
	defer t.gauge.exit()
	t.runs.Add(1)
	time.Sleep(t.delay)
	if t.failing.Load() {
		return "", fmt.Errorf("%s: simulated failure", t.name)
	}
	return t.name + ": ok", nil
}

// demoStaticDecider is the OFF arm: an always-empty plan makes executeBatch
// fall back to the static read-only partition with no suspension enforcement,
// no tier decision, and no budget hint — the pre-U40 behavior.
type demoStaticDecider struct{}

func (demoStaticDecider) DecideRound(context.Context, sched.RoundInput) (sched.RoundPlan, error) {
	return sched.RoundPlan{}, nil
}

// demoPlanRecord is one tee'd scheduling decision (explainability evidence).
type demoPlanRecord struct {
	Scenario  string          `json:"scenario"`
	Arm       string          `json:"arm"`
	Round     int             `json:"round"`
	Tools     []string        `json:"tools"`
	Suspended []string        `json:"suspended_input,omitempty"`
	BudgetUSD json.RawMessage `json:"budget,omitempty"`
	Plan      sched.RoundPlan `json:"plan"`
	Err       string          `json:"err,omitempty"`
}

// demoTeeDecider wraps the arm's decider, recording every decision and
// forwarding behavior observations (Observe) so the RuleDecider's gate
// learns exactly as it does in production.
type demoTeeDecider struct {
	inner    sched.Decider
	mu       sync.Mutex
	scenario string
	arm      string
	round    int
	records  []demoPlanRecord
	lastPlan sched.RoundPlan
}

func (d *demoTeeDecider) DecideRound(ctx context.Context, in sched.RoundInput) (sched.RoundPlan, error) {
	plan, err := d.inner.DecideRound(ctx, in)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.round++
	rec := demoPlanRecord{Scenario: d.scenario, Arm: d.arm, Round: d.round, Plan: plan}
	for _, c := range in.ToolCalls {
		rec.Tools = append(rec.Tools, c.Name)
	}
	rec.Suspended = in.SuspendedTools
	if b, jerr := json.Marshal(in.Budget); jerr == nil && string(b) != "{}" {
		rec.BudgetUSD = b
	}
	if err != nil {
		rec.Err = err.Error()
	}
	d.records = append(d.records, rec)
	d.lastPlan = plan
	return plan, err
}

// Observe forwards behavior statistics to the wrapped decider when it
// supports them (RuleDecider does; the static arm does not).
func (d *demoTeeDecider) Observe(name string, ok bool, durationMs int64) {
	if o, can := d.inner.(interface{ Observe(string, bool, int64) }); can {
		o.Observe(name, ok, durationMs)
	}
}

func (d *demoTeeDecider) setScenario(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.scenario = s
	d.round = 0
}

// demoRound is one measured executeBatch invocation.
type demoRound struct {
	Scenario        string         `json:"scenario"`
	Arm             string         `json:"arm"`
	WallMs          float64        `json:"wall_ms"`
	PeakConcurrency int            `json:"peak_concurrency"`
	Results         []string       `json:"results"`
	ToolRuns        map[string]int `json:"tool_runs"`
	TierResolved    string         `json:"tier_resolved,omitempty"`
	BudgetAction    string         `json:"budget_action,omitempty"`
	HardStopped     bool           `json:"hard_stopped,omitempty"`
	HardStopMsg     string         `json:"hard_stop_msg,omitempty"`
}

// demoArm bundles one arm's agent and instrumentation.
type demoArm struct {
	name   string
	agent  *Agent
	tee    *demoTeeDecider
	gauge  *demoGauge
	tools  map[string]*demoTool
	tiers  []string
	rounds []demoRound
}

func newDemoArm(name string, inner sched.Decider, limitUSD float64) *demoArm {
	gauge := &demoGauge{}
	reg := tool.NewRegistry()
	tools := map[string]*demoTool{}
	for toolName, delay := range map[string]time.Duration{
		"read_a":      40 * time.Millisecond,
		"read_b":      40 * time.Millisecond,
		"read_c":      40 * time.Millisecond,
		"read_d":      40 * time.Millisecond,
		"flaky_probe": 40 * time.Millisecond,
		"noisy_scan":  200 * time.Millisecond,
	} {
		dt := &demoTool{name: toolName, delay: delay, gauge: gauge}
		tools[toolName] = dt
		reg.Add(dt)
	}
	arm := &demoArm{name: name, gauge: gauge, tools: tools}
	tee := &demoTeeDecider{inner: inner, arm: name}
	arm.tee = tee
	arm.agent = New(namedProvider("pro"), reg, NewSession(""), Options{
		Decider: tee,
		TierResolver: func(tier string) (provider.Provider, *provider.Pricing, int, error) {
			arm.tiers = append(arm.tiers, tier)
			return namedProvider(tier), nil, 0, nil
		},
		ResourceBudget: kernelevent.ResourceBudget{LimitUSD: limitUSD, Window: "session"},
	}, event.Discard)
	return arm
}

// runRound mirrors the run_loop seam: hard-stop guard before the batch
// (run_loop.go:647), executeBatch, then the scheduled-tier application
// (run_loop.go:657) from the decided plan.
func (arm *demoArm) runRound(scenario string, calls []provider.ToolCall) demoRound {
	arm.tee.setScenario(scenario)
	rec := demoRound{Scenario: scenario, Arm: arm.name, ToolRuns: map[string]int{}}
	before := map[string]int32{}
	for n, dt := range arm.tools {
		before[n] = dt.runs.Load()
	}
	if ctrl := arm.agent.budgetCtrl; ctrl != nil && ctrl.Action() == sched.BudgetActionHardStop {
		rec.HardStopped = true
		rec.HardStopMsg = arm.agent.budgetHardStopError().Error()
		rec.BudgetAction = sched.BudgetActionHardStop
		return rec
	}
	arm.gauge.resetPeak()
	t0 := time.Now()
	batch := arm.agent.executeBatch(context.Background(), &turnRuntime{}, calls)
	rec.WallMs = float64(time.Since(t0).Microseconds()) / 1000
	rec.PeakConcurrency = arm.gauge.peakN()
	rec.Results = batch.results
	for n, dt := range arm.tools {
		if d := int(dt.runs.Load() - before[n]); d > 0 {
			rec.ToolRuns[n] = d
		}
	}
	plan := arm.tee.lastPlan
	rec.BudgetAction = plan.BudgetAction
	if plan.Tier != "" || plan.BudgetAction == sched.BudgetActionDegradeTier {
		tiersBefore := len(arm.tiers)
		arm.agent.applyScheduledTier(plan.Tier, plan.BudgetAction)
		if len(arm.tiers) > tiersBefore {
			rec.TierResolved = arm.tiers[len(arm.tiers)-1]
		}
	}
	arm.rounds = append(arm.rounds, rec)
	return rec
}

func demoCalls(names ...string) []provider.ToolCall {
	calls := make([]provider.ToolCall, len(names))
	for i, n := range names {
		calls[i] = provider.ToolCall{ID: fmt.Sprintf("c%d", i+1), Name: n, Arguments: `{}`}
	}
	return calls
}

func TestU42SchedulingDemo(t *testing.T) {
	if os.Getenv("SEMANTIX_SCHED_DEMO") != "1" {
		t.Skip("set SEMANTIX_SCHED_DEMO=1 to run the U42 evidence generator")
	}
	outDir := os.Getenv("SEMANTIX_SCHED_DEMO_OUT")
	if outDir == "" {
		outDir = t.TempDir()
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	on := newDemoArm("on", sched.NewRuleDecider(sched.Config{}), 1.00)
	off := newDemoArm("off", demoStaticDecider{}, 0) // pre-U40: no plan, no budget
	arms := []*demoArm{on, off}
	var allRounds []demoRound

	// S1 并行分组基线: five read-only calls; both arms must parallelize —
	// the decider preserves the static read-only win (no regression).
	for _, arm := range arms {
		allRounds = append(allRounds, arm.runRound("S1-parallel-baseline",
			demoCalls("read_a", "read_b", "read_c", "read_d", "flaky_probe")))
	}

	// S2 行为门: six failing warmup rounds teach the ON decider that
	// flaky_probe is unreliable (MinSamples 5); the measured round then
	// isolates it out of the parallel group while OFF keeps one wide batch.
	for _, arm := range arms {
		arm.tools["flaky_probe"].failing.Store(true)
		for i := 0; i < 6; i++ {
			arm.runRound("S2-warmup", demoCalls("flaky_probe"))
		}
		arm.tools["flaky_probe"].failing.Store(false)
		allRounds = append(allRounds, arm.runRound("S2-behavior-gate",
			demoCalls("read_a", "read_b", "read_c", "read_d", "flaky_probe")))
	}

	// S3 工具挂起: the resource layer marks noisy_scan suspended (the
	// kernel catalog's SuspendTools execution point, U40). ON echoes the
	// suspension through the plan and executeBatch skips the tool; OFF has
	// no plan, so the suspension is not enforced and 200ms burn.
	for _, arm := range arms {
		arm.agent.resources.setSuspended(arm.agent.svc.tools, []string{"noisy_scan"})
		allRounds = append(allRounds, arm.runRound("S3-suspend",
			demoCalls("read_a", "read_b", "noisy_scan")))
		arm.agent.resources.setSuspended(arm.agent.svc.tools, nil)
	}

	// S4 预算阶梯 (U41: 70/90/100): spend crosses each threshold; the ON
	// arm's decider emits halt_prefetch → degrade_tier (tier forced to
	// flash) and the run_loop guard refuses the post-100% round. OFF has
	// no budget configured (pre-U41) and never reacts.
	// observeSpend mirrors run_budget.go:158-159: fold the billed cost into
	// the controller AND push the live snapshot into the resource catalog so
	// the next DecideRound sees real spend.
	observeSpend := func(arm *demoArm, cost float64) {
		if arm.agent.budgetCtrl == nil {
			return
		}
		arm.agent.budgetCtrl.Observe(cost)
		arm.agent.SetResourceBudget(arm.agent.budgetCtrl.Snapshot())
	}
	for _, arm := range arms {
		observeSpend(arm, 0.75) // 75% of $1.00
		allRounds = append(allRounds, arm.runRound("S4a-halt-prefetch", demoCalls("read_a", "read_b")))
		observeSpend(arm, 0.20) // 95%
		allRounds = append(allRounds, arm.runRound("S4b-degrade-tier", demoCalls("read_c")))
		observeSpend(arm, 0.10) // 105%
		allRounds = append(allRounds, arm.runRound("S4c-hard-stop", demoCalls("read_d")))
	}

	// S5 组合: suspension + gated flaky history + healthy reads in one
	// round (budget already latched at hard_stop on the ON arm keeps its
	// round refused — the sticky ladder is part of the evidence).
	for _, arm := range arms {
		arm.agent.resources.setSuspended(arm.agent.svc.tools, []string{"noisy_scan"})
		allRounds = append(allRounds, arm.runRound("S5-composite",
			demoCalls("read_a", "read_b", "read_c", "flaky_probe", "noisy_scan")))
		arm.agent.resources.setSuspended(arm.agent.svc.tools, nil)
	}

	// ---- evidence out ----
	writeJSON := func(name string, v any) {
		b, err := json.MarshalIndent(v, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outDir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON("rounds.json", allRounds)
	var plans []demoPlanRecord
	for _, arm := range arms {
		plans = append(plans, arm.tee.records...)
	}
	pf, err := os.Create(filepath.Join(outDir, "roundplans.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(pf)
	for _, p := range plans {
		if err := enc.Encode(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := pf.Close(); err != nil {
		t.Fatal(err)
	}
	for _, r := range allRounds {
		t.Logf("%-22s %-3s wall=%7.1fms peak=%d runs=%v tier=%s action=%s stopped=%v",
			r.Scenario, r.Arm, r.WallMs, r.PeakConcurrency, r.ToolRuns, r.TierResolved, r.BudgetAction, r.HardStopped)
	}
	t.Logf("evidence written to %s", outDir)
}
