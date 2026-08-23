package prefetch

import (
	"testing"
)

// TestPlanEpsilonProbeEscapesAbsorbingState is the regression for #254: when
// every candidate falls below MinConf, Plan would be empty and the evolve
// loop would starve of hit/waste signal, freezing PrefetchConf in the
// absorbing state. With a positive Epsilon it probes the best excluded
// candidate; with Epsilon=0 (the default) behavior is unchanged.
func TestPlanEpsilonProbeEscapesAbsorbingState(t *testing.T) {
	// A high MinConf with low-probability candidates reproduces the
	// absorbing-state condition: nothing would be proposed.
	m := NewMatrixPrefetcher(Config{MinConf: 0.8, Epsilon: 1.0})
	m.Observe("grep", "read_file", true)
	m.Observe("grep", "glob", true)
	m.Observe("grep", "glob", true)

	tasks, err := m.Plan([]string{"grep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("want 1 epsilon probe, got %d tasks", len(tasks))
	}
	// glob has prob 2/3 > read_file 1/3, so it is the best excluded candidate.
	if tasks[0].Key != "glob" {
		t.Fatalf("probe key = %q, want %q", tasks[0].Key, "glob")
	}
	if tasks[0].Cost != Defaults().BaseCost {
		t.Fatalf("probe cost = %d, want default BaseCost %d", tasks[0].Cost, Defaults().BaseCost)
	}
	if !tasks[0].Probe {
		t.Fatal("epsilon escape task must be marked as a probe")
	}
}

// TestPlanEpsilonZeroKeepsEmptyPlan verifies backward compatibility: the
// default Epsilon=0 must keep the existing empty-plan behavior even when a
// candidate is excluded by MinConf.
func TestPlanEpsilonZeroKeepsEmptyPlan(t *testing.T) {
	m := NewMatrixPrefetcher(Config{MinConf: 0.8, Epsilon: 0})
	m.Observe("grep", "read_file", true)
	m.Observe("grep", "glob", true)

	tasks, err := m.Plan([]string{"grep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("Epsilon=0: want empty plan, got %d tasks", len(tasks))
	}
}

// TestPlanEpsilonRespectsNoData ensures the escape probe is only used when
// there is learned transition knowledge; an empty transition table still
// yields an empty plan even at Epsilon=1.
func TestPlanEpsilonRespectsNoData(t *testing.T) {
	m := NewMatrixPrefetcher(Config{Epsilon: 1.0})
	tasks, err := m.Plan([]string{"unseen"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("no data: want empty plan, got %d tasks", len(tasks))
	}
}

// TestPlanEpsilonSkipsDemotedCandidates ensures a demoted candidate (bad
// waste/hit history) is never chosen as the escape probe.
func TestPlanEpsilonSkipsDemotedCandidates(t *testing.T) {
	m := NewMatrixPrefetcher(Config{MinConf: 0.8, Epsilon: 1.0, WasteHitLimit: 3.0})
	m.Observe("grep", "stale", true)
	m.Observe("grep", "stale", true)
	m.Observe("grep", "stale", true)
	m.Observe("grep", "fresh", true)
	// stale: waste history via repeated ObserveWaste demotes it below limit.
	for i := 0; i < 10; i++ {
		m.ObserveWaste("stale")
	}
	tasks, err := m.Plan([]string{"grep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Key != "fresh" {
		t.Fatalf("want probe=fresh (non-demoted), got %+v", tasks)
	}
}
