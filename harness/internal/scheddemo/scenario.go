package scheddemo

import (
	"fmt"
	"time"

	"semantix/harness/internal/agent/testutil"
	"semantix/harness/internal/provider"
	kernelevent "semantix/kernel/event"
	"semantix/kernel/sched"
)

// Scenario is one task family the demo replays under both arms. The tool specs,
// turn script, budget, and model catalog are the real inputs the scheduler and
// harness consume; tokensIn/tokensOut are the synthetic per-round token estimate
// the cost model multiplies by the chosen tier's price.
type Scenario struct {
	Name      string
	Intent    string
	tools     []toolSpec
	turns     []testutil.Turn
	budget    kernelevent.ResourceBudget
	models    []kernelevent.ResourceModel
	tokensIn  int
	tokensOut int
	cfg       sched.Config
	suspend   []string // ON arm suspends these tools (models a kernel suspend decision)
	warmup    int      // leading rounds that only seed a decision; excluded from the cost delta
}

// decider builds the scheduler for one arm. OFF is always the empty-plan
// baseline; ON is the real RuleDecider, optionally wrapped by a suspend policy.
func (sc Scenario) decider(kernelOn bool) sched.Decider {
	if !kernelOn {
		return offDecider{}
	}
	inner := sched.NewRuleDecider(sc.cfg)
	if len(sc.suspend) > 0 {
		return suspendPolicyDecider{inner: inner, suspend: sc.suspend}
	}
	return inner
}

// round builds one tool-call turn from a set of tool names.
func round(names ...string) testutil.Turn {
	calls := make([]provider.ToolCall, len(names))
	for i, n := range names {
		calls[i] = provider.ToolCall{ID: fmt.Sprintf("c%d", i+1), Name: n, Arguments: "{}"}
	}
	return testutil.Turn{ToolCalls: calls}
}

// defaultModels is the two-tier catalog the scheduler routes between.
func defaultModels() []kernelevent.ResourceModel {
	return []kernelevent.ResourceModel{
		{ID: "deepseek-v4-flash", Tier: "flash"},
		{ID: "deepseek-v4-pro", Tier: "pro"},
	}
}

// BehaviorGateScenario: a flaky read-only tool fails a short warmup, then shares
// a measured round with a healthy read. ON's behavior gate (RuleDecider) pulls
// the now-untrusted flaky tool into its own serial slot, so the round runs at
// peak concurrency 1; OFF parallelizes both blindly at peak 2. MinSamples is
// lowered to 2 so two warmup failures are enough to trip the gate.
func BehaviorGateScenario() Scenario {
	const flaky, healthy = "flaky_grep", "read_file"
	return Scenario{
		Name:   "behavior-gate",
		Intent: "a flaky read-only tool is pulled out of the parallel group once its failures pass the floor",
		tools: []toolSpec{
			{name: flaky, readOnly: true, dur: 30 * time.Millisecond, failFirst: 2},
			{name: healthy, readOnly: true, dur: 30 * time.Millisecond},
		},
		turns: []testutil.Turn{
			round(flaky),          // warmup 1 (fails)
			round(flaky),          // warmup 2 (fails → gate trips)
			round(flaky, healthy), // measured: ON serial, OFF parallel
			{Text: "done"},
		},
		models:    defaultModels(),
		tokensIn:  1000,
		tokensOut: 2000,
		cfg:       sched.Config{MinSamples: 2},
		warmup:    2, // the two flaky-only rounds only seed the behavior gate
	}
}

// TierSwitchScenario: one small all-read-only round. RuleDecider routes it to
// the default (flash) tier because nothing writes and the round is under the
// complexity threshold; OFF makes no tier decision and bills at the pro
// baseline. The delta is pure cost.
func TierSwitchScenario() Scenario {
	return Scenario{
		Name:   "tier-switch",
		Intent: "a cheap read-only round is routed to flash instead of the pro baseline",
		tools: []toolSpec{
			{name: "read_file", readOnly: true, dur: 20 * time.Millisecond},
			{name: "grep", readOnly: true, dur: 20 * time.Millisecond},
		},
		turns:     []testutil.Turn{round("read_file", "grep"), {Text: "done"}},
		models:    defaultModels(),
		tokensIn:  2000,
		tokensOut: 6000,
	}
}

// SuspendScenario: one round pairing a healthy read with a tool the kernel has
// decided to suspend. ON stamps SuspendTools (modeling a BudgetController/operator
// suspend decision); the harness blocks the tool before it runs. OFF has no plan,
// so the tool runs normally.
func SuspendScenario() Scenario {
	return Scenario{
		Name:   "suspend",
		Intent: "the kernel suspends a tool it deems risky; the harness blocks it",
		tools: []toolSpec{
			{name: "read_file", readOnly: true, dur: 20 * time.Millisecond},
			{name: "risky_tool", readOnly: true, dur: 20 * time.Millisecond},
		},
		turns:     []testutil.Turn{round("read_file", "risky_tool"), {Text: "done"}},
		models:    defaultModels(),
		tokensIn:  1500,
		tokensOut: 3000,
		suspend:   []string{"risky_tool"},
	}
}

// BudgetDowngradeScenario: spend is pre-set at 92% of the limit, and the round
// contains a writer so it would otherwise route to pro. RuleDecider's budget
// ladder (70/90/100) fires degrade_tier at 90%, forcing the round to flash; OFF
// ignores the budget and bills at pro. The delta is cost under budget pressure.
func BudgetDowngradeScenario() Scenario {
	return Scenario{
		Name:   "budget-downgrade",
		Intent: "at 92% of budget the kernel degrades a would-be-pro round to flash",
		tools: []toolSpec{
			{name: "read_file", readOnly: true, dur: 20 * time.Millisecond},
			{name: "apply_patch", readOnly: false, dur: 20 * time.Millisecond},
		},
		turns:     []testutil.Turn{round("read_file", "apply_patch"), {Text: "done"}},
		budget:    kernelevent.ResourceBudget{LimitUSD: 1.0, SpentUSD: 0.92, Window: "session"},
		models:    defaultModels(),
		tokensIn:  2000,
		tokensOut: 8000,
	}
}

// FanoutScenario: one round of four contiguous read-only tools. Both arms fan it
// out (the harness static grouping already parallelizes read-only batches); it
// is the honest baseline that proves OFF is not a straw man.
func FanoutScenario() Scenario {
	names := []string{"read_file", "grep", "list_dir", "stat_path"}
	specs := make([]toolSpec, len(names))
	for i, n := range names {
		specs[i] = toolSpec{name: n, readOnly: true, dur: 30 * time.Millisecond}
	}
	return Scenario{
		Name:      "fanout",
		Intent:    "four independent read-only lookups in one round",
		tools:     specs,
		turns:     []testutil.Turn{round(names...), {Text: "done"}},
		models:    defaultModels(),
		tokensIn:  1000,
		tokensOut: 2000,
	}
}
