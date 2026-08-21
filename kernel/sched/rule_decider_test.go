package sched

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync"
	"testing"

	"semantix/kernel/slice"
)

func TestApplyEvolutionChangesNextRoundBehaviorGate(t *testing.T) {
	d := NewRuleDecider(Config{MinSamples: 2, SuccessFloor: 0.7})
	d.Observe("grep", true, 10)
	d.Observe("grep", false, 10)
	in := RoundInput{ToolCalls: []ToolCallInfo{call("a", "grep", true), call("b", "read_file", true)}}
	before, _ := d.DecideRound(context.Background(), in)
	if len(before.ParallelGroups) != 2 {
		t.Fatalf("before tuning: want unreliable grep serialized, got %v", before.ParallelGroups)
	}
	if err := d.ApplyEvolution(0.4); err != nil {
		t.Fatal(err)
	}
	after, _ := d.DecideRound(context.Background(), in)
	if len(after.ParallelGroups) != 1 || len(after.ParallelGroups[0]) != 2 {
		t.Fatalf("after tuning: want next round parallel, got %v", after.ParallelGroups)
	}
}

func TestApplyEvolutionRejectsNonFiniteThreshold(t *testing.T) {
	if err := NewRuleDecider(Config{}).ApplyEvolution(math.NaN()); err == nil {
		t.Fatal("want NaN rejected")
	}
}

// helpers

func call(id, name string, ro bool) ToolCallInfo {
	return ToolCallInfo{CallID: id, Name: name, ReadOnly: ro}
}

func hit(id string, score float64) slice.Hit {
	return slice.Hit{Slice: &slice.Slice{ID: id}, Score: score}
}

func idsOf(groups [][]string) [][]string { return groups }

func flatten(groups [][]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// --- static partitioning ---

func TestPartitionReadOnlyParallelWritersSerial(t *testing.T) {
	d := NewRuleDecider(Config{})
	in := RoundInput{ToolCalls: []ToolCallInfo{
		call("a", "grep", true),
		call("b", "read_file", true),
		call("c", "bash", false),
		call("d", "ls", true),
	}}
	plan, err := d.DecideRound(context.Background(), in)
	if err != nil {
		t.Fatalf("DecideRound: %v", err)
	}
	// a+b parallel; c serial; d serial (single trailing read-only is a
	// one-element group, still its own group).
	if len(plan.ParallelGroups) != 3 {
		t.Fatalf("want 3 groups, got %v", plan.ParallelGroups)
	}
	if got := flatten(plan.ParallelGroups); got[0] != "a" || got[1] != "b" || got[2] != "c" || got[3] != "d" {
		t.Fatalf("order broken: %v", got)
	}
}

func TestPartitionBlacklistNeverParallel(t *testing.T) {
	d := NewRuleDecider(Config{})
	in := RoundInput{ToolCalls: []ToolCallInfo{
		call("a", "read_file", true),
		call("b", "complete_step", true), // read-only declared but blacklisted
		call("c", "wait", true),
		call("d", "grep", true),
	}}
	plan, err := d.DecideRound(context.Background(), in)
	if err != nil {
		t.Fatalf("DecideRound: %v", err)
	}
	// a alone, b alone, c alone, d alone: blacklist breaks contiguity.
	if len(plan.ParallelGroups) != 4 {
		t.Fatalf("want 4 groups, got %v", plan.ParallelGroups)
	}
}

func TestPartitionMaxParallelSubsplit(t *testing.T) {
	d := NewRuleDecider(Config{MaxParallel: 2})
	var calls []ToolCallInfo
	for i := 0; i < 5; i++ {
		calls = append(calls, call(string(rune('a'+i)), "read_file", true))
	}
	plan, err := d.DecideRound(context.Background(), RoundInput{ToolCalls: calls})
	if err != nil {
		t.Fatalf("DecideRound: %v", err)
	}
	if len(plan.ParallelGroups) != 3 { // 2+2+1
		t.Fatalf("want 3 capped groups, got %v", plan.ParallelGroups)
	}
	if len(plan.ParallelGroups[0]) != 2 || len(plan.ParallelGroups[2]) != 1 {
		t.Fatalf("cap not applied: %v", plan.ParallelGroups)
	}
	if plan.MaxParallel != 2 {
		t.Fatalf("want maxParallel 2, got %d", plan.MaxParallel)
	}
}

func TestSuspendToolsAreDeclarativeAndExcludedFromGroups(t *testing.T) {
	d := NewRuleDecider(Config{})
	calls := []ToolCallInfo{call("a", "grep", true), call("b", "read_file", true)}
	plan, _ := d.DecideRound(context.Background(), RoundInput{
		ToolCalls: calls, SuspendedTools: []string{"read_file", "read_file", ""},
	})
	if !equalStrings(plan.SuspendTools, []string{"read_file"}) {
		t.Fatalf("unexpected suspend set %v", plan.SuspendTools)
	}
	if got := flatten(plan.ParallelGroups); !equalStrings(got, []string{"a"}) {
		t.Fatalf("suspended call remained in groups: %v", plan.ParallelGroups)
	}
	plan, _ = d.DecideRound(context.Background(), RoundInput{ToolCalls: calls})
	if len(plan.SuspendTools) != 0 || !equalStrings(flatten(plan.ParallelGroups), []string{"a", "b"}) {
		t.Fatalf("tool did not recover on next round: %+v", plan)
	}
}

// --- behavior learning ---

func TestBehaviorGatePullsUnreliableReadOutOfParallel(t *testing.T) {
	d := NewRuleDecider(Config{MinSamples: 3, SuccessFloor: 0.7})
	// grep fails 2 of 3 → success rate 0.33 < 0.7 → must go serial.
	for i := 0; i < 2; i++ {
		d.Observe("grep", false, 10)
	}
	d.Observe("grep", true, 10)

	in := RoundInput{ToolCalls: []ToolCallInfo{
		call("a", "grep", true),
		call("b", "read_file", true),
	}}
	plan, err := d.DecideRound(context.Background(), in)
	if err != nil {
		t.Fatalf("DecideRound: %v", err)
	}
	// grep (failing gate) and read_file (no history, passes) must NOT share
	// a group: two serial singles.
	if len(plan.ParallelGroups) != 2 {
		t.Fatalf("want grep split out, got %v", plan.ParallelGroups)
	}
	for _, g := range plan.ParallelGroups {
		if len(g) != 1 {
			t.Fatalf("group not serialized: %v", plan.ParallelGroups)
		}
	}
}

func TestBehaviorGateUnknownToolPasses(t *testing.T) {
	d := NewRuleDecider(Config{MinSamples: 3, SuccessFloor: 0.7})
	// No history for read_file → static rule applies (parallel).
	in := RoundInput{ToolCalls: []ToolCallInfo{
		call("a", "grep", true),
		call("b", "read_file", true),
	}}
	plan, err := d.DecideRound(context.Background(), in)
	if err != nil {
		t.Fatalf("DecideRound: %v", err)
	}
	if len(plan.ParallelGroups) != 1 || len(plan.ParallelGroups[0]) != 2 {
		t.Fatalf("want one parallel pair, got %v", plan.ParallelGroups)
	}
}

func TestBehaviorGateRecoversAfterImprovement(t *testing.T) {
	d := NewRuleDecider(Config{MinSamples: 3, SuccessFloor: 0.5})
	for i := 0; i < 3; i++ {
		d.Observe("grep", false, 5)
	}
	in := RoundInput{ToolCalls: []ToolCallInfo{call("a", "grep", true), call("b", "read_file", true)}}
	plan, _ := d.DecideRound(context.Background(), in)
	if len(plan.ParallelGroups) != 2 {
		t.Fatalf("unreliable grep must be serial first, got %v", plan.ParallelGroups)
	}
	// Now grep succeeds 3 more times → 3/6 = 0.5 → passes floor 0.5 (>=).
	for i := 0; i < 3; i++ {
		d.Observe("grep", true, 5)
	}
	plan, _ = d.DecideRound(context.Background(), in)
	if len(plan.ParallelGroups) != 1 || len(plan.ParallelGroups[0]) != 2 {
		t.Fatalf("recovered grep should parallelize again, got %v", plan.ParallelGroups)
	}
}

// --- tier ---

func TestTierRules(t *testing.T) {
	d := NewRuleDecider(Config{})
	// all read-only, small → flash
	plan, _ := d.DecideRound(context.Background(), RoundInput{ToolCalls: []ToolCallInfo{
		call("a", "grep", true), call("b", "read_file", true),
	}})
	if plan.Tier != "flash" {
		t.Fatalf("want flash, got %s", plan.Tier)
	}
	// writer present → pro
	plan, _ = d.DecideRound(context.Background(), RoundInput{ToolCalls: []ToolCallInfo{
		call("a", "grep", true), call("b", "bash", false),
	}})
	if plan.Tier != "pro" {
		t.Fatalf("want pro (writer), got %s", plan.Tier)
	}
	// many read-only calls → pro
	var many []ToolCallInfo
	for i := 0; i < 4; i++ {
		many = append(many, call(string(rune('a'+i)), "grep", true))
	}
	plan, _ = d.DecideRound(context.Background(), RoundInput{ToolCalls: many})
	if plan.Tier != "pro" {
		t.Fatalf("want pro (complex), got %s", plan.Tier)
	}
}

func TestTierCustomNames(t *testing.T) {
	d := NewRuleDecider(Config{DefaultTier: "cheap", ProTier: "strong"})
	plan, _ := d.DecideRound(context.Background(), RoundInput{ToolCalls: []ToolCallInfo{
		call("a", "grep", true), call("b", "bash", false),
	}})
	if plan.Tier != "strong" {
		t.Fatalf("want strong, got %s", plan.Tier)
	}
}

func TestBudgetActions(t *testing.T) {
	d := NewRuleDecider(Config{})
	tests := []struct {
		spent  float64
		action string
		tier   string
	}{
		{0.69, "", "pro"},
		{0.70, BudgetActionHaltPrefetch, "pro"},
	{0.80, BudgetActionDegradeInject, "pro"},
		{0.90, BudgetActionDegradeTier, "flash"},
		{1.00, BudgetActionHardStop, "pro"},
	}
	for _, tt := range tests {
		plan, err := d.DecideRound(context.Background(), RoundInput{
			ToolCalls: []ToolCallInfo{call("w", "bash", false)},
			Budget:    BudgetState{LimitUSD: 1, SpentUSD: tt.spent, Window: "session"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if plan.BudgetAction != tt.action || plan.Tier != tt.tier {
			t.Fatalf("spent %.2f: got action=%q tier=%q", tt.spent, plan.BudgetAction, plan.Tier)
		}
	}
}

func TestRoundPlanJSONKeepsLegacyNamesAndOmitsZeroExtensions(t *testing.T) {
	b, err := json.Marshal(RoundPlan{ParallelGroups: [][]string{{"a"}}, Tier: "flash"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, `"ParallelGroups"`) || !strings.Contains(text, `"Tier"`) {
		t.Fatalf("legacy field names changed: %s", text)
	}
	for _, field := range []string{"suspendTools", "maxParallel", "budgetAction"} {
		if strings.Contains(text, field) {
			t.Fatalf("zero extension %q was not omitted: %s", field, text)
		}
	}
}

// --- injection ids ---

func TestInjectIDsCanonicalOrderAndCap(t *testing.T) {
	d := NewRuleDecider(Config{InjectMax: 2})
	in := RoundInput{SliceHits: []slice.Hit{
		hit("z-slice", 0.9),
		hit("a-slice", 0.8),
		hit("m-slice", 0.7),
	}}
	plan, err := d.DecideRound(context.Background(), in)
	if err != nil {
		t.Fatalf("DecideRound: %v", err)
	}
	want := []string{"a-slice", "m-slice"} // ID-ascending, capped at 2
	if !equalStrings(plan.InjectIDs, want) {
		t.Fatalf("want %v, got %v", want, plan.InjectIDs)
	}
}

func TestInjectIDsNilSliceSafe(t *testing.T) {
	d := NewRuleDecider(Config{})
	plan, _ := d.DecideRound(context.Background(), RoundInput{SliceHits: []slice.Hit{
		{Slice: nil, Score: 1},
		hit("b", 0.5),
	}})
	if !equalStrings(plan.InjectIDs, []string{"b"}) {
		t.Fatalf("want [b], got %v", plan.InjectIDs)
	}
}

// --- prefetch callback ---

func TestPrefetchPlanFuncWired(t *testing.T) {
	d := NewRuleDecider(Config{})
	called := false
	d.SetPrefetchPlanFunc(func(names []string) []string {
		called = true
		return []string{"read_file"}
	})
	plan, _ := d.DecideRound(context.Background(), RoundInput{ToolCalls: []ToolCallInfo{
		call("a", "bash", true),
	}})
	if !called {
		t.Fatal("prefetch func not called")
	}
	if !equalStrings(plan.PrefetchIDs, []string{"read_file"}) {
		t.Fatalf("want [read_file], got %v", plan.PrefetchIDs)
	}
}

func TestPrefetchPlanFuncNil(t *testing.T) {
	d := NewRuleDecider(Config{})
	plan, _ := d.DecideRound(context.Background(), RoundInput{ToolCalls: []ToolCallInfo{
		call("a", "bash", true),
	}})
	if plan.PrefetchIDs != nil {
		t.Fatalf("want nil prefetch, got %v", plan.PrefetchIDs)
	}
}

// --- stats / concurrency ---

func TestStatsSnapshot(t *testing.T) {
	d := NewRuleDecider(Config{})
	d.Observe("grep", true, 10)
	d.Observe("grep", false, 20)
	s := d.Stats()
	st, ok := s["grep"]
	if !ok {
		t.Fatal("grep missing from stats")
	}
	if st.Runs != 2 || st.Failures != 1 || st.DurationMs != 30 {
		t.Fatalf("unexpected stats %+v", st)
	}
	if st.SuccessRate != 0.5 {
		t.Fatalf("want 0.5, got %f", st.SuccessRate)
	}
}

func TestConcurrentDecideAndObserve(t *testing.T) {
	d := NewRuleDecider(Config{MinSamples: 10})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				d.Observe("grep", j%2 == 0, int64(j))
				_, _ = d.DecideRound(context.Background(), RoundInput{
					ToolCalls: []ToolCallInfo{call("a", "grep", true)},
				})
			}
		}()
	}
	wg.Wait()
	if len(d.Stats()) == 0 {
		t.Fatal("stats empty after concurrent observe")
	}
}

// --- helpers ---

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]string(nil), a...)
	bb := append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

// silence unused import guard (idsOf keeps flatten used in tests)
var _ = idsOf
