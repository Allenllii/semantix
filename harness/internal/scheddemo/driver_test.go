package scheddemo

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// The OFF arm must still fan a contiguous read-only batch across goroutines: the
// harness static partitionToolCalls does that on its own. Proving OFF stays
// parallel keeps the comparison honest — the kernel's wins must come from real
// decisions, not from a hobbled baseline.
func TestOffParallelizesReadOnlyFanout(t *testing.T) {
	res := RunScenario(context.Background(), FanoutScenario(), false)
	if res.PeakConcurrency < 2 {
		t.Fatalf("OFF must still parallelize contiguous read-only tools (not a straw man); peak=%d", res.PeakConcurrency)
	}
}

// After a read-only tool accumulates failures past the floor, the kernel's
// behavior gate pulls it out of the parallel group — so ON runs the measured
// round at lower peak concurrency than OFF, which parallelizes the flaky tool
// blindly. This is the "并行分组生效" case: the kernel decision changes how the
// harness fans out work.
func TestBehaviorGateLowersConcurrencyVsOff(t *testing.T) {
	on := RunScenario(context.Background(), BehaviorGateScenario(), true)
	off := RunScenario(context.Background(), BehaviorGateScenario(), false)
	if on.PeakConcurrency >= off.PeakConcurrency {
		t.Fatalf("behavior gate must lower ON concurrency below OFF: on=%d off=%d", on.PeakConcurrency, off.PeakConcurrency)
	}
}

// A small all-read-only round is routed to the cheap flash tier by the kernel,
// while OFF has no tier decision and bills at the capable (pro) baseline. This
// is the "tier 切换" case: same work, lower cost, because the kernel chose the
// tier the round actually needed.
func TestTierSwitchRoutesCheapRoundToFlash(t *testing.T) {
	on := RunScenario(context.Background(), TierSwitchScenario(), true)
	off := RunScenario(context.Background(), TierSwitchScenario(), false)
	if len(on.TierSequence) == 0 || on.TierSequence[0] != "flash" {
		t.Fatalf("ON must route the cheap read-only round to flash, got %v", on.TierSequence)
	}
	if on.CostUSD >= off.CostUSD {
		t.Fatalf("flash routing must cost less than the static pro baseline: on=%.6f off=%.6f", on.CostUSD, off.CostUSD)
	}
}

// The kernel's suspend decision is enforced by the harness: ON blocks the
// suspended tool before it runs, OFF (no scheduler plan) runs it normally. This
// is the "工具挂起" case.
func TestSuspendBlocksToolOnlyWhenKernelOn(t *testing.T) {
	on := RunScenario(context.Background(), SuspendScenario(), true)
	off := RunScenario(context.Background(), SuspendScenario(), false)
	if !toolBlocked(on, "risky_tool") {
		t.Fatalf("ON must block the suspended tool, got %+v", on.Tools)
	}
	if toolBlocked(off, "risky_tool") {
		t.Fatalf("OFF must run the tool (no suspension), got %+v", off.Tools)
	}
}

func toolBlocked(res Result, name string) bool {
	for _, ev := range res.Tools {
		if ev.Name == name && strings.Contains(ev.Err, "suspended by scheduler") {
			return true
		}
	}
	return false
}

// RunAll must replay all five scenarios under both arms and produce at least one
// scheduling round per arm — the guard the cmd demo relies on.
func TestRunAllCoversEveryScenario(t *testing.T) {
	cmps := RunAll(context.Background())
	if len(cmps) != 5 {
		t.Fatalf("RunAll returned %d comparisons, want 5", len(cmps))
	}
	for _, c := range cmps {
		if len(c.On.Rounds) == 0 || len(c.Off.Rounds) == 0 {
			t.Fatalf("%s: both arms must record rounds (on=%d off=%d)", c.Scenario, len(c.On.Rounds), len(c.Off.Rounds))
		}
	}
}

// With spend past the 90% rung, the kernel degrades a would-be-pro (writer) round
// to flash to contain cost; OFF ignores the budget and bills at pro. This is the
// "预算降级" case — a budget signal changing the tier the harness runs.
func TestBudgetDowngradeForcesFlash(t *testing.T) {
	on := RunScenario(context.Background(), BudgetDowngradeScenario(), true)
	off := RunScenario(context.Background(), BudgetDowngradeScenario(), false)
	if !slices.Contains(on.BudgetActions, "degrade_tier") {
		t.Fatalf("ON must emit degrade_tier at 92%% spend, got %v", on.BudgetActions)
	}
	if len(on.TierSequence) == 0 || on.TierSequence[0] != "flash" {
		t.Fatalf("degrade_tier must force the round to flash, got %v", on.TierSequence)
	}
	if on.CostUSD >= off.CostUSD {
		t.Fatalf("budget downgrade must cost less than the pro baseline: on=%.6f off=%.6f", on.CostUSD, off.CostUSD)
	}
}
