package prefetch

import (
	"strings"
	"testing"
)

// seedDemoted returns a prefetcher whose only read-only successor of "bash"
// has been demoted by waste-only feedback.
func seedDemoted(t *testing.T, cfg Config) *MatrixPrefetcher {
	t.Helper()
	m := NewMatrixPrefetcher(cfg)
	for i := 0; i < 10; i++ {
		m.Observe("bash", "read_file", true)
	}
	for i := 0; i < 5; i++ {
		m.ObserveWaste("read_file")
	}
	if tasks, _ := m.Plan([]string{"bash"}); len(tasks) != 0 {
		t.Fatalf("waste-only candidate must start demoted, got %v", tasks)
	}
	return m
}

// drive runs the production feedback loop: whatever Plan proposes is reported
// as a hit. A key Plan never proposes can never receive feedback — which is
// exactly the absorbing state under test.
func drive(m *MatrixPrefetcher, rounds int) {
	for i := 0; i < rounds; i++ {
		tasks, _ := m.Plan([]string{"bash"})
		for _, task := range tasks {
			m.ObserveHit(task.Key)
		}
	}
}

func TestProbationRestoresDemotedCandidateThroughPlanOnly(t *testing.T) {
	m := seedDemoted(t, Config{WasteHitLimit: 0.5, ProbationInterval: 3})

	drive(m, 60)

	tasks, _ := m.Plan([]string{"bash"})
	if len(tasks) != 1 || tasks[0].Key != "read_file" {
		t.Fatalf("probation must let a useful candidate earn its way back, got %v", tasks)
	}
}

func TestProbationKeepsWastingCandidateDemoted(t *testing.T) {
	m := seedDemoted(t, Config{WasteHitLimit: 0.5, ProbationInterval: 3})

	// The tool is genuinely useless: every probe is wasted.
	for i := 0; i < 60; i++ {
		tasks, _ := m.Plan([]string{"bash"})
		for _, task := range tasks {
			m.ObserveWaste(task.Key)
		}
	}

	if tasks, _ := m.Plan([]string{"bash"}); len(tasks) != 0 {
		t.Fatalf("a candidate that keeps wasting must stay demoted, got %v", tasks)
	}
}

func TestProbationDisabledKeepsAbsorbingBehaviour(t *testing.T) {
	m := seedDemoted(t, Config{WasteHitLimit: 0.5, ProbationInterval: -1})

	drive(m, 60)

	if tasks, _ := m.Plan([]string{"bash"}); len(tasks) != 0 {
		t.Fatalf("probation disabled must keep the previous behaviour, got %v", tasks)
	}
}

func TestProbationNeverProbesWriters(t *testing.T) {
	m := NewMatrixPrefetcher(Config{WasteHitLimit: 0.5, ProbationInterval: 2})
	for i := 0; i < 10; i++ {
		m.Observe("bash", "write_file", false)
	}
	for i := 0; i < 5; i++ {
		m.ObserveWaste("write_file")
	}

	for i := 0; i < 20; i++ {
		tasks, _ := m.Plan([]string{"bash"})
		if len(tasks) != 0 {
			t.Fatalf("probation must never propose a writer, got %v", tasks)
		}
	}
}

func TestProbationRespectsTopK(t *testing.T) {
	m := NewMatrixPrefetcher(Config{MinConf: 0.1, TopK: 2, MaxCost: 4096, BaseCost: 512,
		WasteHitLimit: 0.5, ProbationInterval: 2})
	for i := 0; i < 10; i++ {
		m.Observe("bash", "grep", true)
	}
	for i := 0; i < 6; i++ {
		m.Observe("bash", "ls", true)
	}
	for i := 0; i < 4; i++ {
		m.Observe("bash", "read_file", true)
	}
	for i := 0; i < 5; i++ {
		m.ObserveWaste("read_file")
	}

	probed := false
	for i := 0; i < 20; i++ {
		tasks, _ := m.Plan([]string{"bash"})
		if len(tasks) > 2 {
			t.Fatalf("plan exceeded TopK on round %d: %v", i, tasks)
		}
		for _, task := range tasks {
			if task.Key == "read_file" {
				probed = true
			}
		}
	}
	if !probed {
		t.Fatal("demoted candidate was never probed")
	}
}

// seedTiedDemoted gives "bash" three demoted successors with IDENTICAL counts,
// so probability alone cannot order them. Map iteration order is randomized and
// sort.Slice is not stable, so without an explicit tie-break the rotation would
// differ between runs.
func seedTiedDemoted(t *testing.T, cfg Config) *MatrixPrefetcher {
	t.Helper()
	m := NewMatrixPrefetcher(cfg)
	for _, key := range []string{"alpha", "beta", "gamma"} {
		for i := 0; i < 4; i++ {
			m.Observe("bash", key, true)
		}
		for i := 0; i < 5; i++ {
			m.ObserveWaste(key)
		}
	}
	return m
}

func TestProbationIsDeterministicUnderTies(t *testing.T) {
	cfg := Config{MinConf: 0.1, WasteHitLimit: 0.5, ProbationInterval: 2}

	var want []string
	for run := 0; run < 30; run++ {
		m := seedTiedDemoted(t, cfg)
		var got []string
		for i := 0; i < 12; i++ {
			tasks, _ := m.Plan([]string{"bash"})
			for _, task := range tasks {
				got = append(got, task.Key)
				m.ObserveWaste(task.Key) // keep every candidate demoted
			}
		}
		if len(got) == 0 {
			t.Fatal("no probes fired; the test would be vacuous")
		}
		if want == nil {
			want = got
			continue
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("run %d diverged: want %v, got %v", run, want, got)
		}
	}
}

// A demoted candidate below MinConf is reachable by neither the #254 epsilon
// probe (which skips demoted candidates) nor a MinConf-gated probation slot.
// Probation must therefore ignore MinConf.
func TestProbationReachesDemotedBelowMinConf(t *testing.T) {
	m := NewMatrixPrefetcher(Config{MinConf: 0.9, WasteHitLimit: 0.5, ProbationInterval: 2})
	for i := 0; i < 5; i++ {
		m.Observe("bash", "read_file", true)
		m.Observe("bash", "grep", true) // keeps read_file's probability at 0.5 < MinConf
	}
	for i := 0; i < 5; i++ {
		m.ObserveWaste("read_file")
		m.ObserveWaste("grep")
	}

	probed := false
	for i := 0; i < 20 && !probed; i++ {
		tasks, _ := m.Plan([]string{"bash"})
		for _, task := range tasks {
			probed = true
			m.ObserveWaste(task.Key)
		}
	}
	if !probed {
		t.Fatal("a demoted candidate below MinConf is unreachable by both probes")
	}
}

// Documented trade-off: with TopK == 1 a probation round spends the only slot
// on the probe. One round in ProbationInterval is the price of recovery being
// possible at all when there is a single slot.
func TestProbationWithSingleSlotYieldsTheRound(t *testing.T) {
	m := NewMatrixPrefetcher(Config{MinConf: 0.1, TopK: 1, WasteHitLimit: 0.5, ProbationInterval: 4})
	for i := 0; i < 10; i++ {
		m.Observe("bash", "healthy", true)
	}
	for i := 0; i < 4; i++ {
		m.Observe("bash", "stale", true)
	}
	for i := 0; i < 5; i++ {
		m.ObserveWaste("stale")
	}

	var plan []string
	for i := 0; i < 8; i++ {
		tasks, _ := m.Plan([]string{"bash"})
		if len(tasks) != 1 {
			t.Fatalf("round %d: TopK=1 must always yield exactly one task, got %v", i, tasks)
		}
		plan = append(plan, tasks[0].Key)
	}

	got := strings.Join(plan, ",")
	want := "healthy,healthy,healthy,stale,healthy,healthy,healthy,stale"
	if got != want {
		t.Fatalf("single-slot probation schedule changed: want %s, got %s", want, got)
	}
}

func TestProbationRotatesAmongDemotedCandidates(t *testing.T) {
	m := NewMatrixPrefetcher(Config{MinConf: 0.1, TopK: 2, MaxCost: 4096, BaseCost: 512,
		WasteHitLimit: 0.5, ProbationInterval: 2})
	for i := 0; i < 10; i++ {
		m.Observe("bash", "grep", true) // higher probability, stays useless
	}
	for i := 0; i < 6; i++ {
		m.Observe("bash", "read_file", true) // lower probability, actually useful
	}
	for i := 0; i < 5; i++ {
		m.ObserveWaste("grep")
		m.ObserveWaste("read_file")
	}

	// grep keeps wasting; a leader that never recovers must not starve the
	// rest of the demoted set out of the probe slot.
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		tasks, _ := m.Plan([]string{"bash"})
		for _, task := range tasks {
			seen[task.Key] = true
			m.ObserveWaste(task.Key)
		}
	}

	if !seen["read_file"] {
		t.Fatal("a permanently wasting leader starved the other demoted candidate")
	}
}

// --- multi-tool workloads: the probation counter must be per last-tool ---

// seedTwoTools gives "bash" and "grep" one demoted read-only successor each.
func seedTwoTools(t *testing.T, cfg Config) *MatrixPrefetcher {
	t.Helper()
	m := NewMatrixPrefetcher(cfg)
	for i := 0; i < 10; i++ {
		m.Observe("bash", "read_file", true)
		m.Observe("grep", "cat_file", true)
	}
	for i := 0; i < 5; i++ {
		m.ObserveWaste("read_file")
		m.ObserveWaste("cat_file")
	}
	return m
}

func TestProbationProbesEveryToolUnderAlternation(t *testing.T) {
	m := seedTwoTools(t, Config{WasteHitLimit: 0.5})

	// Real traffic alternates tools; a global probation counter would put one
	// tool's calls in a residue class that never lands on a probation round.
	probed := map[string]int{}
	for i := 0; i < 200; i++ {
		for _, last := range []string{"bash", "grep"} {
			tasks, _ := m.Plan([]string{last})
			for _, task := range tasks {
				probed[task.Key]++
				m.ObserveHit(task.Key)
			}
		}
	}

	for _, key := range []string{"read_file", "cat_file"} {
		if probed[key] == 0 {
			t.Fatalf("%s was never probed under alternation: %v", key, probed)
		}
	}
}

func TestProbationRecoversEveryToolUnderAlternation(t *testing.T) {
	m := seedTwoTools(t, Config{WasteHitLimit: 0.5})

	for i := 0; i < 200; i++ {
		for _, last := range []string{"bash", "grep"} {
			tasks, _ := m.Plan([]string{last})
			for _, task := range tasks {
				m.ObserveHit(task.Key)
			}
		}
	}

	for last, want := range map[string]string{"bash": "read_file", "grep": "cat_file"} {
		tasks, _ := m.Plan([]string{last})
		if len(tasks) != 1 || tasks[0].Key != want {
			t.Fatalf("last=%s: useful candidate never recovered, got %v", last, tasks)
		}
	}
}

func TestProbationProbesEveryToolUnderRoundRobin(t *testing.T) {
	tools := []string{"t0", "t1", "t2", "t3"}
	m := NewMatrixPrefetcher(Config{MinConf: 0.1, WasteHitLimit: 0.5})
	for _, tool := range tools {
		for i := 0; i < 10; i++ {
			m.Observe(tool, tool+"_next", true)
		}
		for i := 0; i < 5; i++ {
			m.ObserveWaste(tool + "_next")
		}
	}

	probed := map[string]int{}
	for i := 0; i < 200; i++ {
		for _, last := range tools {
			tasks, _ := m.Plan([]string{last})
			for _, task := range tasks {
				probed[task.Key]++
				m.ObserveWaste(task.Key) // stay demoted: only probation can propose them
			}
		}
	}

	for _, tool := range tools {
		if probed[tool+"_next"] == 0 {
			t.Fatalf("%s_next starved under round-robin: %v", tool, probed)
		}
	}
}
