package prefetch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

// fakeExecutor returns canned content per key and fails the listed keys.
type fakeExecutor struct {
	results map[string][]byte
	fails   map[string]error
}

func (f *fakeExecutor) Execute(_ context.Context, t PrefetchTask) ([]byte, error) {
	if err, ok := f.fails[t.Key]; ok {
		return nil, err
	}
	return f.results[t.Key], nil
}

func seedIndexedSlice(t *testing.T, st slice.Store, ix slice.Index, id, content string) {
	t.Helper()
	// Source candidates are Context slices so the dedup assertion can count
	// only the Runner-persisted Result slices.
	s := &slice.Slice{ID: id, Type: slice.Context, Scope: slice.Project, Content: []byte(content)}
	if err := st.Put(s); err != nil {
		t.Fatal(err)
	}
	if err := ix.Insert(s); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerStoresResultSlice(t *testing.T) {
	st := newTestStore(t, filepath.Join(t.TempDir(), "slices.jsonl"))
	ix := bm25.New()
	seedIndexedSlice(t, st, ix, "s1", "readFile usage documentation")

	r := &Runner{Store: st, Executor: &SliceAssembler{Index: ix, K: 3}}
	ids, err := r.Run(context.Background(), []PrefetchTask{{Kind: "slice-assembly", Key: "readFile", Cost: 200, Locality: LocalityLocal}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Fatalf("Run returned %d ids, want 1", len(ids))
	}
	got, err := st.Get(ids[0])
	if err != nil || got == nil {
		t.Fatalf("stored slice not found: %v, %v", got, err)
	}
	if got.Type != slice.Result {
		t.Fatalf("stored type = %v, want Result", got.Type)
	}
	if got.Meta.SourceSession != "prefetch:readFile" {
		t.Fatalf("SourceSession = %q, want prefetch:readFile", got.Meta.SourceSession)
	}
	content := string(got.Content)
	if !strings.Contains(content, "--- slice s1 ---") || !strings.Contains(content, "readFile usage documentation") {
		t.Fatalf("assembled content missing source slice text: %q", content)
	}
	if len(got.ID) != 16 { // 8 content-hash bytes, hex
		t.Fatalf("result slice ID = %q, want 16 hex chars", got.ID)
	}
}

// Default scope: zero-value Scope is stored as Project.
func TestRunnerDefaultScopeIsProject(t *testing.T) {
	st := newTestStore(t, filepath.Join(t.TempDir(), "slices.jsonl"))
	r := &Runner{Store: st, Executor: &fakeExecutor{results: map[string][]byte{"k": []byte("c")}}}
	if _, err := r.Run(context.Background(), []PrefetchTask{{Key: "k", Locality: LocalityLocal}}); err != nil {
		t.Fatal(err)
	}
	all, err := st.List(slice.Project)
	if err != nil || len(all) != 1 {
		t.Fatalf("expected 1 project slice, got %d (%v)", len(all), err)
	}
}

// One failing task must not abort the others; the failure is surfaced.
func TestRunnerFailureContinues(t *testing.T) {
	st := newTestStore(t, filepath.Join(t.TempDir(), "slices.jsonl"))
	r := &Runner{
		Store: st,
		Executor: &fakeExecutor{
			results: map[string][]byte{"good": []byte("good result")},
			fails:   map[string]error{"bad": errors.New("boom")},
		},
	}
	ids, err := r.Run(context.Background(), []PrefetchTask{{Key: "bad", Locality: LocalityLocal}, {Key: "good", Locality: LocalityLocal}})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Run error = %v, want boom", err)
	}
	if len(ids) != 1 {
		t.Fatalf("Run ids = %v, want only the good result", ids)
	}
	all, _ := st.List(slice.Project)
	if len(all) != 1 {
		t.Fatalf("store has %d slices, want 1 (failed task must not persist)", len(all))
	}
}

func TestRunnerRequiresFields(t *testing.T) {
	r := &Runner{}
	if _, err := r.Run(context.Background(), []PrefetchTask{{Key: "k", Locality: LocalityLocal}}); err == nil {
		t.Fatal("expected error for missing Store/Executor")
	}
}

// SliceAssembler output is canonical (ID-sorted) regardless of index order.
func TestSliceAssemblerCanonicalOrder(t *testing.T) {
	st := newTestStore(t, filepath.Join(t.TempDir(), "slices.jsonl"))
	ix := bm25.New()
	seedIndexedSlice(t, st, ix, "bbb", "banana docs")
	seedIndexedSlice(t, st, ix, "aaa", "apple docs")

	content, err := (&SliceAssembler{Index: ix, K: 10}).Execute(context.Background(), PrefetchTask{Key: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	iA, iB := strings.Index(text, "--- slice aaa ---"), strings.Index(text, "--- slice bbb ---")
	if iA < 0 || iB < 0 || iA > iB {
		t.Fatalf("assembled content not ID-sorted: %q", text)
	}
}

func TestSliceAssemblerEmptyHits(t *testing.T) {
	ix := bm25.New() // empty index
	content, err := (&SliceAssembler{Index: ix, K: 3}).Execute(context.Background(), PrefetchTask{Key: "readFile"})
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 0 {
		t.Fatalf("expected empty content for no hits, got %q", content)
	}
}

// Deterministic dedup: assembling the same source twice stores one slice.
func TestRunnerStoresDedupByContent(t *testing.T) {
	st := newTestStore(t, filepath.Join(t.TempDir(), "slices.jsonl"))
	ix := bm25.New()
	seedIndexedSlice(t, st, ix, "s1", "stable candidate content")

	r := &Runner{Store: st, Executor: &SliceAssembler{Index: ix, K: 3}}
	for i := 0; i < 2; i++ {
		if _, err := r.Run(context.Background(), []PrefetchTask{{Kind: "slice-assembly", Key: "stable", Locality: LocalityLocal}}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := st.List(slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	results := 0
	for _, s := range all {
		if s.Type == slice.Result {
			results++
		}
	}
	if results != 1 {
		t.Fatalf("expected 1 deduped Result slice, got %d (%s)", results, fmt.Sprint(ids(all)))
	}
}

func ids(slices []*slice.Slice) []string {
	out := make([]string, len(slices))
	for i, s := range slices {
		out[i] = s.ID
	}
	return out
}
// TestRunnerRejectsEgressTasks (Issue #273): a task that crosses a
// process boundary is never executed — a speculative external call leaks
// inferred intent before any commit (Ghost Tool Calls). The counter
// records the block for observability.
func TestRunnerRejectsEgressTasks(t *testing.T) {
	st := newTestStore(t, filepath.Join(t.TempDir(), "slices.jsonl"))
	executed := 0
	r := &Runner{Store: st, Executor: &countingExecutor{executed: &executed}}
	ids, err := r.Run(context.Background(), []PrefetchTask{
		{Kind: "mcp", Key: "external", Locality: LocalityEgress},
		{Kind: "slice-assembly", Key: "readFile", Locality: LocalityLocal},
	})
	if err != nil {
		t.Fatal(err)
	}
	if executed != 1 {
		t.Fatalf("executed %d tasks, want 1 (egress must not run)", executed)
	}
	if len(ids) != 1 {
		t.Fatalf("stored ids = %v, want 1", ids)
	}
	if got := r.BlockedEgress.Load(); got != 1 {
		t.Fatalf("BlockedEgress = %d, want 1", got)
	}
}

// TestRunnerUndeclaredLocalityFailsClosed (Issue #273): an undeclared
// locality is treated as egress — fail-closed, matching the
// empty-whitelist semantics.
func TestRunnerUndeclaredLocalityFailsClosed(t *testing.T) {
	st := newTestStore(t, filepath.Join(t.TempDir(), "slices.jsonl"))
	executed := 0
	r := &Runner{Store: st, Executor: &countingExecutor{executed: &executed}}
	ids, err := r.Run(context.Background(), []PrefetchTask{{Kind: "file", Key: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if executed != 0 || len(ids) != 0 {
		t.Fatalf("undeclared task must not run: executed=%d ids=%v", executed, ids)
	}
	if got := r.BlockedEgress.Load(); got != 1 {
		t.Fatalf("BlockedEgress = %d, want 1", got)
	}
}

type countingExecutor struct{ executed *int }

func (c *countingExecutor) Execute(_ context.Context, t PrefetchTask) ([]byte, error) {
	*c.executed++
	return []byte("ok"), nil
}
