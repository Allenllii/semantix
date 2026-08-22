package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"semantix/harness/event"
	"semantix/harness/provider"
	"semantix/harness/taskintent"
	"semantix/harness/tool"
	kernelevent "semantix/kernel/event"
	"semantix/kernel/sched"
)

type testTool struct {
	name string
	runs atomic.Int32
}

type errorDecider struct{}

func (errorDecider) DecideRound(context.Context, sched.RoundInput) (sched.RoundPlan, error) {
	return sched.RoundPlan{}, errors.New("scheduler unavailable")
}

func (t *testTool) Name() string            { return t.name }
func (t *testTool) Description() string     { return "test" }
func (t *testTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *testTool) ReadOnly() bool          { return true }
func (t *testTool) Execute(context.Context, json.RawMessage) (string, error) {
	t.runs.Add(1)
	return "ok", nil
}

type sequenceDecider struct {
	mu     sync.Mutex
	plans  []sched.RoundPlan
	inputs []sched.RoundInput
}

func (d *sequenceDecider) DecideRound(_ context.Context, in sched.RoundInput) (sched.RoundPlan, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inputs = append(d.inputs, in)
	if len(d.plans) == 0 {
		return sched.RoundPlan{}, nil
	}
	p := d.plans[0]
	d.plans = d.plans[1:]
	return p, nil
}

func TestSchedulerReceivesFrozenTurnIntent(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&testTool{name: "read_file"})
	d := &sequenceDecider{}
	a := New(nil, reg, NewSession(""), Options{Decider: d}, event.Discard)
	a.prefetchWaitMS.Store(120)
	a.prefetchTaskMS.Store(40)
	turn := &turnRuntime{}
	turn.policy.Intent = taskintent.Mutation
	turn.policySet = true

	a.decideRound(context.Background(), turn, []provider.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{}`}})

	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.inputs) != 1 || d.inputs[0].Intent != "mutation" {
		t.Fatalf("scheduler input intent = %+v, want mutation", d.inputs)
	}
	if got := d.inputs[0].PrefetchLoad; got.WaitWindowMS != 120 || got.TaskEstimateMS != 40 {
		t.Fatalf("scheduler prefetch load = %+v", got)
	}
}

func TestSchedulerSuspendThenRecover(t *testing.T) {
	reg := tool.NewRegistry()
	target := &testTool{name: "read_file"}
	reg.Add(target)
	d := &sequenceDecider{plans: []sched.RoundPlan{
		{SuspendTools: []string{"read_file"}},
		{ParallelGroups: [][]string{{"c2"}}},
	}}
	a := New(nil, reg, NewSession(""), Options{Decider: d}, event.Discard)

	blocked := a.executeBatch(context.Background(), &turnRuntime{}, []provider.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{}`}})
	if blocked.results[0] != "suspended by scheduler" || blocked.outcomes[0].errMsg != "suspended by scheduler" {
		t.Fatalf("unexpected suspended result: %+v", blocked.outcomes[0])
	}
	if target.runs.Load() != 0 {
		t.Fatal("suspended tool executed")
	}

	recovered := a.executeBatch(context.Background(), &turnRuntime{}, []provider.ToolCall{{ID: "c2", Name: "read_file", Arguments: `{}`}})
	if recovered.results[0] != "ok" || target.runs.Load() != 1 {
		t.Fatalf("tool did not recover: result=%q runs=%d", recovered.results[0], target.runs.Load())
	}
}

// TestPlanHardStopBlocksRound is the Issue #250 regression: a hard_stop
// carried on the scheduler's RoundPlan must block every call in the round
// before any tool executes — not just be copied into the batch and ignored.
func TestPlanHardStopBlocksRound(t *testing.T) {
	reg := tool.NewRegistry()
	target := &testTool{name: "read_file"}
	reg.Add(target)
	d := &sequenceDecider{plans: []sched.RoundPlan{
		{BudgetAction: sched.BudgetActionHardStop},
		{}, // control round: no budget action
	}}
	a := New(nil, reg, NewSession(""), Options{Decider: d}, event.Discard)

	calls := []provider.ToolCall{
		{ID: "c1", Name: "read_file", Arguments: `{}`},
		{ID: "c2", Name: "read_file", Arguments: `{}`},
	}
	stopped := a.executeBatch(context.Background(), &turnRuntime{}, calls)
	if stopped.BudgetAction != sched.BudgetActionHardStop {
		t.Fatalf("batch did not carry hard_stop: %+v", stopped.BudgetAction)
	}
	if target.runs.Load() != 0 {
		t.Fatalf("hard_stop round executed %d tool calls", target.runs.Load())
	}
	for i, o := range stopped.outcomes {
		if !o.blocked || o.errMsg == "" {
			t.Fatalf("call %d not blocked under hard_stop: %+v", i, o)
		}
	}

	control := a.executeBatch(context.Background(), &turnRuntime{}, []provider.ToolCall{{ID: "c3", Name: "read_file", Arguments: `{}`}})
	if control.results[0] != "ok" || target.runs.Load() != 1 {
		t.Fatalf("control round did not execute: result=%q runs=%d", control.results[0], target.runs.Load())
	}
}

// TestPlanHardStopError covers the kernel-only wiring: no local controller,
// so the terminal error names the scheduler as the source.
func TestPlanHardStopError(t *testing.T) {
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	err := a.budgetHardStopError()
	if err == nil || !strings.Contains(err.Error(), "scheduler issued hard_stop") {
		t.Fatalf("kernel-path error missing scheduler attribution: %v", err)
	}
}

func TestSchedulerErrorFailsOpen(t *testing.T) {
	reg := tool.NewRegistry()
	target := &testTool{name: "read_file"}
	reg.Add(target)
	a := New(nil, reg, NewSession(""), Options{Decider: errorDecider{}}, event.Discard)
	a.resources.setSuspended(reg, []string{"read_file"})
	batch := a.executeBatch(context.Background(), &turnRuntime{}, []provider.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{}`}})
	if batch.results[0] != "ok" || target.runs.Load() != 1 {
		t.Fatalf("scheduler error did not fail open: result=%q runs=%d", batch.results[0], target.runs.Load())
	}
}

func TestRunParallelHonorsPlanCap(t *testing.T) {
	var current, peak atomic.Int32
	runParallel(context.Background(), 0, 4, 1, func(int) {
		n := current.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		current.Add(-1)
	})
	if peak.Load() != 1 {
		t.Fatalf("maxParallel=1 allowed %d concurrent calls", peak.Load())
	}
}

type namedProvider string

func (p namedProvider) Name() string { return string(p) }
func (p namedProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	close(ch)
	return ch, nil
}

func TestScheduledTierSwitchesProvider(t *testing.T) {
	var resolved []string
	a := New(namedProvider("base"), tool.NewRegistry(), NewSession(""), Options{
		ModelRef: "base",
		TierResolver: func(tier string) (provider.Provider, *provider.Pricing, int, error) {
			resolved = append(resolved, tier)
			return namedProvider(tier), &provider.Pricing{Input: 1}, 12345, nil
		},
	}, event.Discard)
	a.applyScheduledTier("pro", "")
	if a.svc.prov.Name() != "pro" || a.modelRef != "pro" || a.contextWindow != 12345 {
		t.Fatalf("tier did not switch runtime: provider=%s model=%s window=%d", a.svc.prov.Name(), a.modelRef, a.contextWindow)
	}
	a.applyScheduledTier("pro", sched.BudgetActionDegradeTier)
	if a.svc.prov.Name() != "flash" || len(resolved) != 2 || resolved[1] != "flash" {
		t.Fatalf("degrade_tier did not override tier: provider=%s resolved=%v", a.svc.prov.Name(), resolved)
	}
}

func TestResourceCatalogEmitsFullSnapshots(t *testing.T) {
	bus := kernelevent.NewSyncBus()
	var got []kernelevent.ResourceCatalogPayload
	bus.Subscribe(func(e kernelevent.Event) {
		if e.Kind != kernelevent.ResourceCatalog {
			return
		}
		var p kernelevent.ResourceCatalogPayload
		if err := kernelevent.UnmarshalData(e, &p); err != nil {
			t.Errorf("catalog payload: %v", err)
			return
		}
		got = append(got, p)
	})
	reg := tool.NewRegistry()
	reg.Add(&testTool{name: "read_file"})
	a := New(nil, reg, NewSession(""), Options{
		KernelEvents:   bus,
		ResourceModels: []kernelevent.ResourceModel{{ID: "cheap", Tier: "flash"}},
		ResourceBudget: kernelevent.ResourceBudget{LimitUSD: 1, Window: "session"},
	}, event.Discard)
	if len(got) != 1 || len(got[0].Tools) != 1 || len(got[0].Models) != 1 {
		t.Fatalf("startup catalog not full: %+v", got)
	}
	a.resources.setSuspended(reg, []string{"read_file"})
	a.SetResourceBudget(kernelevent.ResourceBudget{LimitUSD: 1, SpentUSD: 0.5, Window: "session"})
	reg.Add(&testTool{name: "grep"})
	a.syncResourceCatalog()
	if len(got) != 4 || !got[1].Tools[0].Suspended || got[2].Budget.SpentUSD != 0.5 || !got[2].Tools[0].Suspended || len(got[3].Tools) != 2 {
		t.Fatalf("change snapshots not full: %+v", got)
	}
}
