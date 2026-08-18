package scheddemo

import (
	"context"
	"sync"
	"time"

	"semantix/harness/internal/agent"
	"semantix/harness/internal/agent/testutil"
	"semantix/harness/internal/event"
	"semantix/harness/internal/tool"
	kernelevent "semantix/kernel/event"
	"semantix/kernel/sched"
)

// offDecider is the OFF arm: it returns an empty plan for every round, so the
// harness falls back to its own static partitionToolCalls grouping with no
// suspension, no tier decision, and no budget action — exactly the baseline
// Issue #193 defines ("静态 ReadOnly 分组、无挂起、无预算动作").
type offDecider struct{}

func (offDecider) DecideRound(context.Context, sched.RoundInput) (sched.RoundPlan, error) {
	return sched.RoundPlan{}, nil
}

// suspendPolicyDecider models the ON arm's tool-suspension decision (Issue #193
// case "工具挂起"). It delegates parallel grouping, tier, and budget to the real
// RuleDecider, then stamps SuspendTools — the same RoundPlan field U41's
// BudgetController emits and the same enforcement path the production
// scheduler_integration_test exercises. Only the decision to suspend is modeled;
// the harness blocking is real code.
type suspendPolicyDecider struct {
	inner   sched.Decider
	suspend []string
}

func (d suspendPolicyDecider) DecideRound(ctx context.Context, in sched.RoundInput) (sched.RoundPlan, error) {
	plan, err := d.inner.DecideRound(ctx, in)
	if err != nil {
		return plan, err
	}
	plan.SuspendTools = append([]string(nil), d.suspend...)
	return plan, nil
}

func (d suspendPolicyDecider) Observe(name string, success bool, durMs int64) {
	forwardObserve(d.inner, name, success, durMs)
}

// recordingDecider wraps whichever decider an arm uses and captures the exact
// (input, plan) pair the harness acted on each round, so the demo can dump the
// real RoundPlan sequence as the "决策可解释性" evidence.
type recordingDecider struct {
	inner  sched.Decider
	mu     sync.Mutex
	rounds []RoundRecord
}

// RoundRecord is one round's scheduling decision as the harness saw it.
type RoundRecord struct {
	ToolCalls      []string   `json:"toolCalls"`
	ParallelGroups [][]string `json:"parallelGroups"`
	Tier           string     `json:"tier"`
	SuspendTools   []string   `json:"suspendTools,omitempty"`
	BudgetAction   string     `json:"budgetAction,omitempty"`
	MaxParallel    int        `json:"maxParallel,omitempty"`
}

func (d *recordingDecider) DecideRound(ctx context.Context, in sched.RoundInput) (sched.RoundPlan, error) {
	plan, err := d.inner.DecideRound(ctx, in)
	names := make([]string, len(in.ToolCalls))
	for i, c := range in.ToolCalls {
		names[i] = c.Name
	}
	d.mu.Lock()
	d.rounds = append(d.rounds, RoundRecord{
		ToolCalls:      names,
		ParallelGroups: plan.ParallelGroups,
		Tier:           plan.Tier,
		SuspendTools:   plan.SuspendTools,
		BudgetAction:   plan.BudgetAction,
		MaxParallel:    plan.MaxParallel,
	})
	d.mu.Unlock()
	return plan, err
}

func (d *recordingDecider) Observe(name string, success bool, durMs int64) {
	forwardObserve(d.inner, name, success, durMs)
}

func (d *recordingDecider) records() []RoundRecord {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]RoundRecord(nil), d.rounds...)
}

// forwardObserve passes behavior feedback through to an inner decider that keeps
// statistics (the RuleDecider), so the ON arm's behavior gate learns from the
// warmup rounds. Deciders without Observe (offDecider) ignore it.
func forwardObserve(inner sched.Decider, name string, success bool, durMs int64) {
	if o, ok := inner.(interface {
		Observe(string, bool, int64)
	}); ok {
		o.Observe(name, success, durMs)
	}
}

// ToolEvent is one captured ToolResult, the harness-side proof of what actually
// ran (or was blocked) each round.
type ToolEvent struct {
	Name       string `json:"name"`
	Err        string `json:"err,omitempty"`
	Output     string `json:"output"`
	ReadOnly   bool   `json:"readOnly"`
	DurationMs int64  `json:"durationMs"`
}

// recordSink captures ToolResult events off the harness event stream.
type recordSink struct {
	mu    sync.Mutex
	tools []ToolEvent
}

func (s *recordSink) Emit(e event.Event) {
	if e.Kind != event.ToolResult {
		return
	}
	s.mu.Lock()
	s.tools = append(s.tools, ToolEvent{
		Name:       e.Tool.Name,
		Err:        e.Tool.Err,
		Output:     e.Tool.Output,
		ReadOnly:   e.Tool.ReadOnly,
		DurationMs: e.Tool.DurationMs,
	})
	s.mu.Unlock()
}

func (s *recordSink) events() []ToolEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ToolEvent(nil), s.tools...)
}

// Result is one arm (ON or OFF) of a scenario run: the real scheduling
// decisions, the harness behavior they produced, and the synthetic cost/latency
// they imply.
type Result struct {
	Scenario        string        `json:"scenario"`
	KernelOn        bool          `json:"kernelOn"`
	Rounds          []RoundRecord `json:"rounds"`
	Tools           []ToolEvent   `json:"tools"`
	PeakConcurrency int           `json:"peakConcurrency"`
	WallClock       time.Duration `json:"wallClockNs"`
	CostUSD         float64       `json:"costUSD"`
	TierSequence    []string      `json:"tierSequence"`
	BudgetActions   []string      `json:"budgetActions,omitempty"`
}

// RunScenario runs one scenario end to end through the real agent loop
// (Agent.Run → executeBatch → the scheduler), once per arm. kernelOn selects the
// real RuleDecider (plus any scenario-specific policy overlay); otherwise the
// empty-plan OFF baseline.
func RunScenario(ctx context.Context, sc Scenario, kernelOn bool) Result {
	probe := &concurrencyProbe{}
	reg := tool.NewRegistry()
	for _, spec := range sc.tools {
		reg.Add(newFixtureTool(spec, probe))
	}

	inner := sc.decider(kernelOn)
	dec := &recordingDecider{inner: inner}
	sink := &recordSink{}

	opts := agent.Options{
		Decider:        dec,
		KernelEvents:   kernelevent.NewSyncBus(),
		ResourceModels: sc.models,
		ResourceBudget: sc.budget,
	}
	mp := testutil.NewMock("scheddemo", sc.turns...)
	a := agent.New(mp, reg, agent.NewSession(""), opts, sink)

	start := time.Now()
	_ = a.Run(ctx, "run scenario "+sc.Name)
	wall := time.Since(start)

	return assembleResult(sc, kernelOn, dec.records(), sink.events(), probe.Peak(), wall)
}

// Comparison holds both arms of one scenario for side-by-side reporting.
type Comparison struct {
	Scenario string `json:"scenario"`
	Intent   string `json:"intent"`
	On       Result `json:"on"`
	Off      Result `json:"off"`
}

// AllScenarios is the demo's task family, one scenario per Issue #193 case (plus
// the fan-out honesty baseline).
func AllScenarios() []Scenario {
	return []Scenario{
		FanoutScenario(),
		BehaviorGateScenario(),
		TierSwitchScenario(),
		SuspendScenario(),
		BudgetDowngradeScenario(),
	}
}

// RunAll replays every scenario under both arms.
func RunAll(ctx context.Context) []Comparison {
	scs := AllScenarios()
	out := make([]Comparison, len(scs))
	for i, sc := range scs {
		out[i] = Comparison{
			Scenario: sc.Name,
			Intent:   sc.Intent,
			On:       RunScenario(ctx, sc, true),
			Off:      RunScenario(ctx, sc, false),
		}
	}
	return out
}

func assembleResult(sc Scenario, kernelOn bool, rounds []RoundRecord, tools []ToolEvent, peak int, wall time.Duration) Result {
	tiers := make([]string, 0, len(rounds))
	actions := make([]string, 0, len(rounds))
	var cost float64
	for i, r := range rounds {
		tiers = append(tiers, r.Tier)
		// Warmup rounds only exist to seed a decision (e.g. the behavior gate);
		// they are scaffolding, not work the scheduler made cheaper, so they are
		// excluded from the cost delta to keep the economic claim honest.
		if i >= sc.warmup {
			cost += RoundCostUSD(r.Tier, sc.tokensIn, sc.tokensOut)
		}
		if r.BudgetAction != "" {
			actions = append(actions, r.BudgetAction)
		}
	}
	return Result{
		Scenario:        sc.Name,
		KernelOn:        kernelOn,
		Rounds:          rounds,
		Tools:           tools,
		PeakConcurrency: peak,
		WallClock:       wall,
		CostUSD:         cost,
		TierSequence:    tiers,
		BudgetActions:   actions,
	}
}
