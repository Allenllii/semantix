package prefetch

import (
	"errors"
	"reflect"
	"testing"

	"semantix/kernel/sched"
)

// fakePrefetcher is a scripted Prefetcher for adapter tests.
type fakePrefetcher struct {
	tasks []PrefetchTask
	err   error
	last  []string // records the last Plan input
}

func TestAsLoadAwarePlanFuncFallsBackToFrozenPlan(t *testing.T) {
	f := &fakePrefetcher{tasks: []PrefetchTask{{Key: "read_file"}}}
	got := AsLoadAwarePlanFunc(f)([]string{"search"}, sched.PrefetchLoadHint{ConcurrencyUsed: 8, ConcurrencyLimit: 8})
	if !reflect.DeepEqual(got.IDs, []string{"read_file"}) || got.Reason != string(ReasonCandidate) {
		t.Fatalf("result = %+v", got)
	}
}

func TestAsLoadAwarePlanFuncUsesMatrixGate(t *testing.T) {
	m := NewMatrixPrefetcher(Config{})
	m.Observe("search", "read_file", true)
	got := AsLoadAwarePlanFunc(m)([]string{"search"}, sched.PrefetchLoadHint{ConcurrencyUsed: 8, ConcurrencyLimit: 8})
	if got.IDs != nil || got.Reason != string(ReasonLoadSaturated) {
		t.Fatalf("result = %+v", got)
	}
}

func (f *fakePrefetcher) Plan(last []string) ([]PrefetchTask, error) {
	f.last = append([]string(nil), last...)
	return f.tasks, f.err
}

func TestAsPlanFuncReducesTasksToKeys(t *testing.T) {
	f := &fakePrefetcher{tasks: []PrefetchTask{
		{Kind: "slice-assembly", Key: "search", Cost: 512},
		{Kind: "slice-assembly", Key: "read_file", Cost: 512},
	}}
	fn := AsPlanFunc(f)
	got := fn([]string{"complete_step", "search"})
	want := []string{"search", "read_file"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AsPlanFunc keys = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(f.last, []string{"complete_step", "search"}) {
		t.Fatalf("last tool names not forwarded: %v", f.last)
	}
}

func TestAsPlanFuncNilPrefetcher(t *testing.T) {
	fn := AsPlanFunc(nil)
	if got := fn([]string{"search"}); got != nil {
		t.Fatalf("nil prefetcher should yield nil plan, got %v", got)
	}
}

func TestAsPlanFuncErrorYieldsNil(t *testing.T) {
	f := &fakePrefetcher{err: errors.New("boom")}
	fn := AsPlanFunc(f)
	if got := fn([]string{"search"}); got != nil {
		t.Fatalf("errored plan should yield nil, got %v", got)
	}
}

func TestAsPlanFuncEmptyPlanYieldsNil(t *testing.T) {
	f := &fakePrefetcher{}
	fn := AsPlanFunc(f)
	if got := fn([]string{"search"}); got != nil {
		t.Fatalf("empty plan should yield nil, got %v", got)
	}
}

func TestAsPlanFuncWorksWithMatrixPrefetcher(t *testing.T) {
	// Integration: the real online prefetcher wired through the adapter
	// behaves the same as calling Plan directly.
	m := NewMatrixPrefetcher(Config{})
	m.Observe("search", "read_file", true)
	m.Observe("search", "read_file", true)
	m.Observe("search", "write_file", false)

	direct, err := m.Plan([]string{"search"})
	if err != nil {
		t.Fatalf("direct Plan: %v", err)
	}
	directKeys := make([]string, 0, len(direct))
	for _, task := range direct {
		directKeys = append(directKeys, task.Key)
	}

	fn := AsPlanFunc(m)
	got := fn([]string{"search"})
	if !reflect.DeepEqual(got, directKeys) {
		t.Fatalf("AsPlanFunc = %v, direct Plan keys = %v", got, directKeys)
	}
}
