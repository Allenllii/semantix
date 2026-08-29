package prefetch

import (
	"math"
	"sync"
	"testing"
)

func TestApplyEvolutionChangesNextPlanConfidence(t *testing.T) {
	m := NewMatrixPrefetcher(Config{MinConf: 0.8})
	m.Observe("grep", "read_file", true)
	m.Observe("grep", "glob", true)
	m.Observe("grep", "glob", true)
	before, _ := m.Plan([]string{"grep"})
	if len(before) != 0 {
		t.Fatalf("before tuning: want no candidates, got %v", before)
	}
	if err := m.ApplyEvolution(0.3); err != nil {
		t.Fatal(err)
	}
	after, _ := m.Plan([]string{"grep"})
	if len(after) != 2 {
		t.Fatalf("after tuning: want both next-round candidates, got %v", after)
	}
}

func TestApplyEvolutionRejectsNonFiniteConfidence(t *testing.T) {
	if err := NewMatrixPrefetcher(Config{}).ApplyEvolution(math.Inf(1)); err == nil {
		t.Fatal("want infinity rejected")
	}
}

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

func TestWastePenaltyDemotesStaleHealthyCandidate(t *testing.T) {
	m := NewMatrixPrefetcher(Config{WasteHitLimit: 3.0})
	for i := 0; i < 10; i++ {
		m.Observe("bash", "read_file", true)
	}
	for i := 0; i < 5; i++ {
		m.ObserveHit("read_file")
	}
	if tasks, _ := m.Plan([]string{"bash"}); len(tasks) != 1 {
		t.Fatalf("healthy candidate must start eligible, got %v", tasks)
	}

	for i := 0; i < 15; i++ {
		m.ObserveWaste("read_file")
	}
	if tasks, _ := m.Plan([]string{"bash"}); len(tasks) != 0 {
		t.Fatalf("sustained waste must age stale hits and demote candidate, got %v", tasks)
	}
}

func TestWastePenaltyRecoversAfterSustainedHits(t *testing.T) {
	m := NewMatrixPrefetcher(Config{WasteHitLimit: 0.5})
	for i := 0; i < 10; i++ {
		m.Observe("bash", "read_file", true)
	}
	for i := 0; i < 5; i++ {
		m.ObserveWaste("read_file")
	}
	if tasks, _ := m.Plan([]string{"bash"}); len(tasks) != 0 {
		t.Fatalf("waste-only candidate must start demoted, got %v", tasks)
	}

	for i := 0; i < 15; i++ {
		m.ObserveHit("read_file")
	}
	if tasks, _ := m.Plan([]string{"bash"}); len(tasks) != 1 {
		t.Fatalf("sustained hits must age stale waste and restore candidate, got %v", tasks)
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

// --- Issue #272: Markov metrics (coverage / accuracy) + Beta posterior ---

func TestStatsMarkovMetricsAccuracyAndZeroDenominators(t *testing.T) {
	m := NewMatrixPrefetcher(Config{})
	s := m.Stats()
	if s.ReadOnlyCalls != 0 || s.Coverage != 0 || s.TotalHits != 0 || s.TotalWastes != 0 || s.Accuracy != 0 {
		t.Fatalf("empty stats must be zero-valued (N/A via zero denominators), got %+v", s)
	}
	m.Observe("bash", "grep", true)
	m.ObserveHit("grep")
	m.ObserveHit("grep")
	m.ObserveWaste("grep")
	s = m.Stats()
	if s.TotalHits != 2 || s.TotalWastes != 1 || s.Accuracy != 2.0/3.0 {
		t.Fatalf("accuracy: hits=%d wastes=%d accuracy=%v", s.TotalHits, s.TotalWastes, s.Accuracy)
	}
	// legacy fields unchanged
	if s.Transitions != 1 || len(s.Pairs) != 1 || s.Pairs["bash"] != 1 {
		t.Fatalf("legacy transition fields broken: %+v", s)
	}
	if len(s.TopNext) != 1 || s.TopNext["bash→grep"] != 1 {
		t.Fatalf("legacy TopNext broken: %v", s.TopNext)
	}
}

func TestCoverageTracksRecentPlan(t *testing.T) {
	m := NewMatrixPrefetcher(Config{MinConf: 0.3})
	// cold-start calls before any Plan: count toward the read-only
	// denominator but can never be covered
	m.Observe("bash", "grep", true)
	m.Observe("bash", "grep", true)
	m.Observe("bash", "glob", true)
	// 9 grep vs 2 glob → grep 0.82 > 0.3 proposed, glob 0.18 filtered
	for i := 0; i < 7; i++ {
		m.Observe("bash", "grep", true)
	}
	m.Observe("bash", "glob", true)
	tasks, err := m.Plan([]string{"bash"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Key != "grep" {
		t.Fatalf("want only grep proposed, got %v", tasks)
	}
	// post-plan executions: grep covered, glob not, writer excluded
	m.Observe("bash", "grep", true)
	m.Observe("bash", "glob", true)
	m.Observe("bash", "edit_file", false)
	s := m.Stats()
	if s.ReadOnlyCalls != 13 || s.CoveredCalls != 1 {
		t.Fatalf("readOnly=%d covered=%d", s.ReadOnlyCalls, s.CoveredCalls)
	}
	if s.Coverage != 1.0/13.0 {
		t.Fatalf("coverage=%v want %v", s.Coverage, 1.0/13.0)
	}
	// a second plan with no proposals clears the proposal set: nothing covers
	if _, err := m.Plan([]string{"never_seen"}); err != nil {
		t.Fatal(err)
	}
	m.Observe("bash", "grep", true)
	if s2 := m.Stats(); s2.CoveredCalls != 1 {
		t.Fatalf("empty plan must clear proposal set, covered=%d", s2.CoveredCalls)
	}
}

func TestStatsPerKeyPosterior(t *testing.T) {
	m := NewMatrixPrefetcher(Config{}) // Decay 0.2
	m.ObserveHit("grep")               // hw=1.0  ww=0.0
	m.ObserveHit("grep")               // hw=1.8  ww=0.0
	m.ObserveWaste("grep")             // hw=1.44 ww=1.0
	s := m.Stats()
	if a, ok := s.Alpha["grep"]; !ok || math.Abs(a-2.44) > 1e-9 {
		t.Fatalf("Alpha=%v (ok=%v), want 2.44", a, ok)
	}
	if b, ok := s.Beta["grep"]; !ok || math.Abs(b-2.0) > 1e-9 {
		t.Fatalf("Beta=%v (ok=%v), want 2.0", b, ok)
	}
	if mu := s.HitRate["grep"]; math.Abs(mu-2.44/4.44) > 1e-9 {
		t.Fatalf("HitRate=%v, want %v", mu, 2.44/4.44)
	}
	if lb := s.LowerBound["grep"]; lb <= 0 || lb >= s.HitRate["grep"] {
		t.Fatalf("lower bound %v must lie in (0, mu=%v)", lb, s.HitRate["grep"])
	}
	if s.Hits["grep"] != 2 || s.Wastes["grep"] != 1 {
		t.Fatalf("counts hits=%d wastes=%d", s.Hits["grep"], s.Wastes["grep"])
	}
}

// TestBetaSingleWasteDoesNotDemote pins the small-sample stability of the
// Beta posterior: one waste feedback leaves the posterior mean at
// (0+1)/(0+1+2) = 1/3 > 1/4, so a learned candidate survives. The former
// raw EWMA (w/h = ∞ after one waste) demoted immediately — that overreaction
// is what the prior smoothing removes (Issue #272).
func TestBetaSingleWasteDoesNotDemote(t *testing.T) {
	m := NewMatrixPrefetcher(Config{WasteHitLimit: 3.0})
	for i := 0; i < 10; i++ {
		m.Observe("bash", "grep", true)
	}
	m.ObserveWaste("grep")
	tasks, _ := m.Plan([]string{"bash"})
	if len(tasks) != 1 || tasks[0].Key != "grep" {
		t.Fatalf("single waste must not demote a learned candidate, got %v", tasks)
	}
}

// TestBetaDriftInjection is the drift-injection experiment (Issue #272
// acceptance): 10 hits → sustained wastes → 20 hits. Asserts ① one waste
// does not demote (prior smoothing), ② sustained waste monotonically drags
// the posterior mean below the threshold within a bounded window (drift is
// eventually recognized), ③ sustained hits restore eligibility.
func TestBetaDriftInjection(t *testing.T) {
	m := NewMatrixPrefetcher(Config{WasteHitLimit: 3.0})
	for i := 0; i < 10; i++ {
		m.Observe("bash", "grep", true)
	}
	for i := 0; i < 10; i++ {
		m.ObserveHit("grep")
	}
	// ① first waste: posterior smoothing must keep the candidate eligible
	m.ObserveWaste("grep")
	if tasks, _ := m.Plan([]string{"bash"}); len(tasks) == 0 {
		t.Fatal("drift phase: first waste must not demote (prior smoothing)")
	}
	// ② sustained waste must demote within a bounded window (verified by
	// hand: with Decay 0.2 the 9th waste crosses μ < 0.25)
	demotedAt := -1
	for i := 0; i < 24; i++ {
		m.ObserveWaste("grep")
		if tasks, _ := m.Plan([]string{"bash"}); len(tasks) == 0 {
			demotedAt = i + 2 // first waste was outside the loop
			break
		}
	}
	if demotedAt <= 0 {
		t.Fatal("sustained waste must eventually demote the candidate")
	}
	if demotedAt > 12 {
		t.Fatalf("drift must be recognized within a bounded window, demoted at waste #%d", demotedAt)
	}
	// ③ recovery: sustained hits must restore eligibility
	for i := 0; i < 20; i++ {
		m.ObserveHit("grep")
	}
	if tasks, _ := m.Plan([]string{"bash"}); len(tasks) != 1 {
		t.Fatalf("sustained hits must restore the candidate, got %v", tasks)
	}
}
