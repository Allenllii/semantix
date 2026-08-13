package prefetch

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"semantix/kernel/slice"
)

// demoStore builds a deterministic ToolPattern slice library:
//
//	tp0: grep readFile editFile test
//	tp1: grep readFile test
//	tp2: readFile editFile test
//
// Learned probabilities (2-grams):
//
//	P(readFile|grep)    = 2/2 = 1.0
//	P(editFile|readFile) = 2/3
//	P(test|readFile)     = 1/3
func demoStore(t *testing.T) slice.Store {
	t.Helper()
	st, err := slice.NewFileStore(filepath.Join(t.TempDir(), "slices.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	patterns := []string{
		"grep readFile editFile test",
		"grep readFile test",
		"readFile editFile test",
	}
	for i, p := range patterns {
		if err := st.Put(&slice.Slice{
			ID:      fmt.Sprintf("tp%d", i),
			Type:    slice.ToolPattern,
			Scope:   slice.Project,
			Content: []byte(p),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// Issue 62 acceptance: the transition matrix is learnable from a demo slice
// library, deterministically.
func TestLearnTransitionMatrix(t *testing.T) {
	p := &Planner{Store: demoStore(t)}
	if err := p.Learn(); err != nil {
		t.Fatal(err)
	}

	p.mu.RLock()
	m := p.matrix
	p.mu.RUnlock()
	if m == nil {
		t.Fatal("matrix not learned")
	}
	cases := []struct {
		from, to string
		want     float64
	}{
		{"grep", "readFile", 1.0},
		{"readFile", "editFile", 2.0 / 3.0},
		{"readFile", "test", 1.0 / 3.0},
		{"grep", "writeFile", 0.0}, // unseen transition
	}
	for _, c := range cases {
		if got := m.prob(c.from, c.to); got != c.want {
			t.Errorf("P(%s|%s) = %v, want %v", c.to, c.from, got, c.want)
		}
	}

	// Idempotent: re-learning yields the same matrix.
	if err := p.Learn(); err != nil {
		t.Fatal(err)
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if got := p.matrix.prob("readFile", "editFile"); got != 2.0/3.0 {
		t.Errorf("after re-learn P(editFile|readFile) = %v, want 2/3", got)
	}
}

func TestPlanPredictsTopK(t *testing.T) {
	p := &Planner{Store: demoStore(t), WhiteList: []string{"readFile", "grep", "lookup", "search"}}
	if err := p.Learn(); err != nil {
		t.Fatal(err)
	}
	tasks, err := p.Plan([]string{"grep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("Plan([grep]) = %d tasks, want 1", len(tasks))
	}
	tk := tasks[0]
	if tk.Kind != "slice-assembly" || tk.Key != "readFile" || tk.Cost != DefaultTaskCost {
		t.Fatalf("task = %+v, want {slice-assembly readFile %d}", tk, DefaultTaskCost)
	}
}

// Probability-descending order: lookup (2/3) precedes readFile (1/3).
func TestPlanOrderProbabilityDesc(t *testing.T) {
	st, err := slice.NewFileStore(filepath.Join(t.TempDir(), "slices.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range []string{"grep readFile editFile test", "grep lookup search", "grep lookup search"} {
		if err := st.Put(&slice.Slice{ID: fmt.Sprintf("tp%d", i), Type: slice.ToolPattern, Scope: slice.Project, Content: []byte(p)}); err != nil {
			t.Fatal(err)
		}
	}
	pl := &Planner{Store: st, WhiteList: []string{"readFile", "lookup"}}
	if err := pl.Learn(); err != nil {
		t.Fatal(err)
	}
	tasks, err := pl.Plan([]string{"grep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].Key != "lookup" || tasks[1].Key != "readFile" {
		t.Fatalf("Plan order = %+v, want [lookup readFile]", tasks)
	}
}

func TestPlanBudgetTruncation(t *testing.T) {
	st, err := slice.NewFileStore(filepath.Join(t.TempDir(), "slices.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range []string{"grep readFile editFile test", "grep lookup search", "grep lookup search"} {
		if err := st.Put(&slice.Slice{ID: fmt.Sprintf("tp%d", i), Type: slice.ToolPattern, Scope: slice.Project, Content: []byte(p)}); err != nil {
			t.Fatal(err)
		}
	}

	// High-probability candidate survives a tight budget; low one is truncated
	// by budget (not by whitelist).
	pl := &Planner{Store: st, WhiteList: []string{"readFile", "lookup"}, Budget: 250}
	if err := pl.Learn(); err != nil {
		t.Fatal(err)
	}
	tasks, err := pl.Plan([]string{"grep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Key != "lookup" {
		t.Fatalf("Plan with Budget=250 = %+v, want [lookup] only", tasks)
	}

	// Budget below one task cost yields an empty plan.
	pl2 := &Planner{Store: st, WhiteList: []string{"readFile", "lookup"}, Budget: 10}
	if err := pl2.Learn(); err != nil {
		t.Fatal(err)
	}
	tasks, err = pl2.Plan([]string{"grep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("Plan with Budget=10 = %d tasks, want 0", len(tasks))
	}
}

// Issue 62 acceptance: non-read-only tools never enter the plan (fail-closed).
func TestPlanWhiteListFailClosed(t *testing.T) {
	st, err := slice.NewFileStore(filepath.Join(t.TempDir(), "slices.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// writeFile/editFile are write tools; the learned successor of grep is writeFile.
	for i, p := range []string{"grep writeFile editFile", "grep writeFile editFile"} {
		if err := st.Put(&slice.Slice{ID: fmt.Sprintf("tp%d", i), Type: slice.ToolPattern, Scope: slice.Project, Content: []byte(p)}); err != nil {
			t.Fatal(err)
		}
	}

	pl := &Planner{Store: st, WhiteList: []string{"readFile"}}
	if err := pl.Learn(); err != nil {
		t.Fatal(err)
	}
	tasks, err := pl.Plan([]string{"grep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("non-white-listed write tool entered the plan: %+v", tasks)
	}

	// Empty whitelist is fail-closed too.
	pl2 := &Planner{Store: st}
	if err := pl2.Learn(); err != nil {
		t.Fatal(err)
	}
	if tasks, err = pl2.Plan([]string{"grep"}); err != nil || len(tasks) != 0 {
		t.Fatalf("empty whitelist must yield empty plan, got %+v, %v", tasks, err)
	}
}

// Generic invariant: every planned Key is white-listed.
func TestPlanKeysAlwaysWhiteListed(t *testing.T) {
	pl := &Planner{Store: demoStore(t), WhiteList: []string{"grep", "readFile"}}
	if err := pl.Learn(); err != nil {
		t.Fatal(err)
	}
	tasks, err := pl.Plan([]string{"readFile"})
	if err != nil {
		t.Fatal(err)
	}
	white := map[string]bool{"grep": true, "readFile": true}
	for _, tk := range tasks {
		if !white[tk.Key] {
			t.Fatalf("task key %q not in whitelist", tk.Key)
		}
	}
}

func TestPlanEmptyOrUnlearned(t *testing.T) {
	// Not learned: no guesses, no error.
	pl := &Planner{Store: demoStore(t), WhiteList: []string{"readFile"}}
	if tasks, err := pl.Plan([]string{"grep"}); err != nil || len(tasks) != 0 {
		t.Fatalf("unlearned Plan = %+v, %v; want empty, nil", tasks, err)
	}

	// Learned but no usable last tool name.
	if err := pl.Learn(); err != nil {
		t.Fatal(err)
	}
	for _, names := range [][]string{nil, {}, {""}, {"  "}} {
		if tasks, err := pl.Plan(names); err != nil || len(tasks) != 0 {
			t.Fatalf("Plan(%v) = %+v, %v; want empty, nil", names, tasks, err)
		}
	}
}

func TestPlanConcurrent(t *testing.T) {
	pl := &Planner{Store: demoStore(t), WhiteList: []string{"readFile", "grep", "test"}}
	if err := pl.Learn(); err != nil {
		t.Fatal(err)
	}
	const g = 8
	var wg sync.WaitGroup
	errs := make(chan error, g)
	for i := 0; i < g; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := pl.Plan([]string{"grep"}); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
