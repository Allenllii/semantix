package prefetch

import (
	"testing"
	"time"
)

func TestEvaluateLoad(t *testing.T) {
	tests := []struct {
		name string
		hint LoadHint
		want PlanReason
	}{
		{name: "cold start", hint: LoadHint{}, want: ""},
		{name: "below saturation", hint: LoadHint{ConcurrencyUsed: 7, ConcurrencyLimit: 10}, want: ""},
		{name: "at saturation", hint: LoadHint{ConcurrencyUsed: 8, ConcurrencyLimit: 10}, want: ReasonLoadSaturated},
		{name: "short window", hint: LoadHint{WaitWindow: 40 * time.Millisecond, TaskEstimate: 50 * time.Millisecond}, want: ReasonWindowTooShort},
		{name: "long window", hint: LoadHint{WaitWindow: 50 * time.Millisecond, TaskEstimate: 40 * time.Millisecond}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EvaluateLoad(tt.hint, DefaultMaxLoad); got != tt.want {
				t.Fatalf("EvaluateLoad() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatrixPlanWithLoadSkipsAndCounts(t *testing.T) {
	m := NewMatrixPrefetcher(Config{})
	m.Observe("search", "read_file", true)

	decision, err := m.PlanWithLoad([]string{"search"}, LoadHint{ConcurrencyUsed: 4, ConcurrencyLimit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Tasks) != 0 || decision.Reason != ReasonLoadSaturated {
		t.Fatalf("saturated decision = %+v", decision)
	}
	decision, err = m.PlanWithLoad([]string{"search"}, LoadHint{})
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Tasks) != 1 || decision.Reason != ReasonCandidate {
		t.Fatalf("cold-start decision = %+v", decision)
	}
	if got := m.GateStats(); got.LoadSkipped != 1 || got.Candidates != 1 {
		t.Fatalf("gate stats = %+v", got)
	}
}

func TestPlannerPlanWithLoadKeepsFrozenPlanBehavior(t *testing.T) {
	p := &Planner{}
	decision, err := p.PlanWithLoad([]string{"search"}, LoadHint{WaitWindow: time.Millisecond, TaskEstimate: 2 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Reason != ReasonWindowTooShort || len(decision.Tasks) != 0 {
		t.Fatalf("decision = %+v", decision)
	}
	if tasks, err := p.Plan([]string{"search"}); err != nil || tasks != nil {
		t.Fatalf("frozen Plan changed: tasks=%v err=%v", tasks, err)
	}
}
