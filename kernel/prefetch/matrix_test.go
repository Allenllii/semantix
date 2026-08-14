package prefetch

import (
	"sync"
	"testing"
)

// --- transition learning ---

func TestPlanReturnsHighConfidenceSuccessor(t *testing.T) {
	m := NewMatrixPrefetcher(Config{})
	// bash -> grep 9/10, bash -> read_file 1/10
	for i := 0; i < 9; i++ {
		m.Observe("bash", "grep", true)
	}
	m.Observe("bash", "read_file", true)

	tasks, err := m.Plan([]string{"bash"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("want at least one task")
	}
	if tasks[0].Key != "grep" {
		t.Fatalf("want grep first, got %v", tasks)
	}
	if tasks[0].Kind != "slice-assembly" {
		t.Fatalf("want slice-assembly kind, got %s", tasks[0].Kind)
	}
}

func TestPlanEmptyOnUnknownLastTool(t *testing.T) {
	m := NewMatrixPrefetcher(Config{})
	m.Observe("bash", "grep", true)
	tasks, err := m.Plan([]string{"never_seen"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if tasks != nil {
		t.Fatalf("want nil, got %v", tasks)
	}
}

func TestPlanEmptyWithoutHistory(t *testing.T) {
	m := NewMatrixPrefetcher(Config{})
	tasks, err := m.Plan([]string{"bash"})
	if err != nil || tasks != nil {
		t.Fatalf("want nil,nil got %v,%v", tasks, err)
	}
}

// --- safety: never prefetch writers ---

func TestPlanNeverPrefetchesWriters(t *testing.T) {
	m := NewMatrixPrefetcher(Config{})
	m.Observe("bash", "edit_file", false) // writer: not marked read-only
	m.Observe("bash", "grep", true)
	tasks, _ := m.Plan([]string{"bash"})
	for _, task := range tasks {
		if task.Key == "edit_file" {
			t.Fatalf("must never prefetch a writer: %v", tasks)
		}
	}
	if len(tasks) != 1 || tasks[0].Key != "grep" {
		t.Fatalf("want only grep, got %v", tasks)
	}
}

// --- confidence filter ---

func TestPlanMinConfFilter(t *testing.T) {
	m := NewMatrixPrefetcher(Config{MinConf: 0.8})
	for i := 0; i < 5; i++ {
		m.Observe("bash", "grep", true)
	}
	m.Observe("bash", "ls", true) // 1/6 ≈ 0.17 < 0.8
	tasks, _ := m.Plan([]string{"bash"})
	if len(tasks) != 1 || tasks[0].Key != "grep" {
		t.Fatalf("low-confidence successor must be filtered, got %v", tasks)
	}
}

// --- budget & topk ---

func TestPlanRespectsTopKAndBudget(t *testing.T) {
	m := NewMatrixPrefetcher(Config{MinConf: 0.1, TopK: 2, MaxCost: 1024, BaseCost: 512})
	for i := 0; i < 10; i++ {
		m.Observe("bash", "grep", true)
		m.Observe("bash", "ls", true)
		m.Observe("bash", "find", true)
		m.Observe("bash", "cat", true)
	}
	tasks, _ := m.Plan([]string{"bash"})
	if len(tasks) != 2 { // TopK=2, budget 1024 = 2×512
		t.Fatalf("want 2 tasks, got %v", tasks)
	}
}

// --- waste penalty ---

func TestWastePenaltyDemotesCandidate(t *testing.T) {
	m := NewMatrixPrefetcher(Config{WasteHitLimit: 3.0})
	for i := 0; i < 10; i++ {
		m.Observe("bash", "grep", true)
	}
	// heavy waste, no hits -> ratio ∞ -> demoted
	for i := 0; i < 5; i++ {
		m.ObserveWaste("grep")
	}
	tasks, _ := m.Plan([]string{"bash"})
	if tasks != nil {
		t.Fatalf("all-waste candidate must be demoted, got %v", tasks)
	}
}

func TestWastePenaltyAllowsHealthyCandidate(t *testing.T) {
	m := NewMatrixPrefetcher(Config{WasteHitLimit: 3.0})
	for i := 0; i < 10; i++ {
		m.Observe("bash", "grep", true)
	}
	// hits outweigh waste -> ratio 1/4 = 0.25 < 3 -> still proposed
	m.ObserveHit("grep")
	m.ObserveHit("grep")
	m.ObserveHit("grep")
	m.ObserveHit("grep")
	m.ObserveWaste("grep")
	tasks, _ := m.Plan([]string{"bash"})
	if len(tasks) != 1 || tasks[0].Key != "grep" {
		t.Fatalf("healthy candidate must survive, got %v", tasks)
	}
}

// --- determinism & concurrency ---

func TestPlanDeterministicOrder(t *testing.T) {
	m := NewMatrixPrefetcher(Config{})
	// equal counts: sort by probability only, so ties are stable by sort
	// stability? sort.Slice is not stable — keep probabilities distinct.
	for i := 0; i < 5; i++ {
		m.Observe("bash", "grep", true)
	}
	for i := 0; i < 3; i++ {
		m.Observe("bash", "ls", true)
	}
	t1, _ := m.Plan([]string{"bash"})
	t2, _ := m.Plan([]string{"bash"})
	if t1[0].Key != t2[0].Key {
		t.Fatalf("nondeterministic order: %v vs %v", t1, t2)
	}
}

func TestConcurrentObserveAndPlan(t *testing.T) {
	m := NewMatrixPrefetcher(Config{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.Observe("bash", "grep", true)
				_, _ = m.Plan([]string{"bash"})
			}
		}()
	}
	wg.Wait()
	tasks, err := m.Plan([]string{"bash"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected learned transitions after concurrent observe")
	}
}

// --- edge cases ---

func TestObserveIgnoresEmpty(t *testing.T) {
	m := NewMatrixPrefetcher(Config{})
	m.Observe("", "grep", true)
	m.Observe("bash", "", true)
	tasks, _ := m.Plan([]string{"bash"})
	if tasks != nil {
		t.Fatalf("empty transitions must not create state, got %v", tasks)
	}
}

func TestPlanUsesLastToolOfSequence(t *testing.T) {
	m := NewMatrixPrefetcher(Config{})
	m.Observe("bash", "grep", true)
	m.Observe("grep", "read_file", true)
	// sequence [bash, grep] → last is grep → read_file
	tasks, _ := m.Plan([]string{"bash", "grep"})
	if len(tasks) != 1 || tasks[0].Key != "read_file" {
		t.Fatalf("want read_file, got %v", tasks)
	}
}
