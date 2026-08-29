package slice

import (
	"path/filepath"
	"strings"
	"testing"
)

func newAdmissionStore(t *testing.T) Store {
	t.Helper()
	store, err := NewFileStore(filepath.Join(t.TempDir(), "db.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c, ok := store.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	})
	return store
}

// TestAdmissionMatrixDowngradesTRFromUser pins Issue #268: T/R slices can
// never persist in User scope — the store downgrades them to Project.
func TestAdmissionMatrixDowngradesTRFromUser(t *testing.T) {
	store := newAdmissionStore(t)
	tr := []*Slice{
		{ID: "t1", Type: ToolPattern, Scope: User, Content: []byte("grep readFile editFile")},
		{ID: "r1", Type: Result, Scope: User, Content: []byte("the answer was 42")},
	}
	for _, s := range tr {
		if err := store.Put(s); err != nil {
			t.Fatal(err)
		}
	}
	user, err := store.List(User)
	if err != nil {
		t.Fatal(err)
	}
	if len(user) != 0 {
		t.Fatalf("User scope must be empty after T/R puts, got %d", len(user))
	}
	proj, err := store.List(Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(proj) != 2 {
		t.Fatalf("T/R must land in Project scope, got %d", len(proj))
	}
}

// TestAdmissionMatrixAllowsHighLevelCM: P/C/M slices keep User scope.
func TestAdmissionMatrixAllowsHighLevelCM(t *testing.T) {
	s := &Slice{ID: "c1", Type: Context, Scope: User, Content: []byte("project layout knowledge")}
	if EnforceAdmission(s) {
		t.Fatal("Context slice must not be downgraded")
	}
	if s.Scope != User {
		t.Fatal("Context scope must be preserved")
	}
	p := &Slice{ID: "p1", Type: Prompt, Scope: User, Content: []byte("run tests before committing")}
	if EnforceAdmission(p) || p.Scope != User {
		t.Fatal("Prompt slice must keep User scope")
	}
	// Downgrades never touch lower scopes.
	pr := &Slice{ID: "t2", Type: ToolPattern, Scope: Project, Content: []byte("a b")}
	if EnforceAdmission(pr) || pr.Scope != Project {
		t.Fatal("Project scope must be untouched")
	}
}

func ctxSlice(id, content string) *Slice {
	return &Slice{ID: id, Type: Context, Scope: Project, Content: []byte(content), CreatedAt: 100}
}

const ctxA = `Project context observed from repeated tool calls:
Frequent paths:
- cmd/semantix/main.go (7)
- kernel/slice/extractor.go (5)
Frequent directories:
- kernel/slice (9)
Common command heads:
- go test (12)
- go build (4)`

const ctxB = `Project context observed from repeated tool calls:
Frequent paths:
- cmd/semantix/main.go (6)
- kernel/slice/extractor.go (4)
Frequent directories:
- kernel/slice (8)
Common command heads:
- go test (10)
- go vet (3)`

const ctxUnrelated = `Project context observed from repeated tool calls:
Frequent paths:
- src/api/client.ts (5)
Frequent directories:
- src/api (6)
Common command heads:
- npm run build (7)`

// TestConsolidateContextMergesNearDuplicates: two near-identical Context
// slices merge into one union slice; the unrelated one survives.
func TestConsolidateContextMergesNearDuplicates(t *testing.T) {
	store := newAdmissionStore(t)
	for _, s := range []*Slice{ctxSlice("a", ctxA), ctxSlice("b", ctxB), ctxSlice("z", ctxUnrelated)} {
		if err := store.Put(s); err != nil {
			t.Fatal(err)
		}
	}
	res, err := ConsolidateContext(store, ConsolidateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Groups != 1 {
		t.Fatalf("groups = %d, want 1", res.Groups)
	}
	if res.Merged != 1 || len(res.Created) != 1 {
		t.Fatalf("merged = %d, want 1", res.Merged)
	}
	// The unrelated slice must survive.
	all, _ := store.ListAll()
	ids := map[string]bool{}
	for _, s := range all {
		ids[s.ID] = true
	}
	if !ids["z"] {
		t.Fatal("unrelated Context slice must survive consolidation")
	}
	if ids["a"] || ids["b"] {
		t.Fatal("merged members must be removed")
	}
	// The union keeps both slices' entries.
	merged, _ := store.Get(res.Created[0])
	if merged == nil {
		t.Fatal("merged slice missing")
	}
	content := string(merged.Content)
	if !strings.Contains(content, "go vet") || !strings.Contains(content, "go test") {
		t.Fatalf("union must contain entries from both members:\n%s", content)
	}
	if !strings.Contains(content, "cmd/semantix/main.go (13)") {
		t.Fatalf("counts must sum across members:\n%s", content)
	}
}

// TestConsolidateContextDeterministic: same store, same merge plan.
func TestConsolidateContextDeterministic(t *testing.T) {
	build := func() ConsolidateResult {
		store := newAdmissionStore(t)
		for _, s := range []*Slice{ctxSlice("a", ctxA), ctxSlice("b", ctxB)} {
			_ = store.Put(s)
		}
		res, err := ConsolidateContext(store, ConsolidateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	r1, r2 := build(), build()
	if len(r1.Created) != len(r2.Created) {
		t.Fatal("merge plan lengths differ")
	}
	for i := range r1.Created {
		if r1.Created[i] != r2.Created[i] {
			t.Fatalf("nondeterministic merge ids: %s vs %s", r1.Created[i], r2.Created[i])
		}
	}
}

// TestConsolidateContextDryRun: nothing is mutated.
func TestConsolidateContextDryRun(t *testing.T) {
	store := newAdmissionStore(t)
	for _, s := range []*Slice{ctxSlice("a", ctxA), ctxSlice("b", ctxB)} {
		_ = store.Put(s)
	}
	res, err := ConsolidateContext(store, ConsolidateOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Groups == 0 || res.Merged != 0 {
		t.Fatalf("dry run: groups=%d merged=%d, want >0 groups, 0 merged", res.Groups, res.Merged)
	}
	all, _ := store.ListAll()
	if len(all) != 2 {
		t.Fatalf("dry run must not mutate, store has %d slices", len(all))
	}
}

func testSessionJSONL(toolCalls string) []byte {
	var b strings.Builder
	b.WriteString(`{"role":"user","content":"fix the failing tests in module A"}` + "\n")
	b.WriteString(toolCalls)
	b.WriteString("\n")
	b.WriteString(`{"role":"assistant","content":"done, all tests pass"}` + "\n")
	return []byte(b.String())
}

// TestExtractorTStepSplit: with TStepSplit, one long turn with interleaved
// verification commands becomes per-subtask ToolPattern slices.
func TestExtractorTStepSplit(t *testing.T) {
	calls := `{"role":"assistant","tool_calls":[{"id":"1","name":"grep","arguments":"{\"command\":\"grep -r TODO\"}"},{"id":"2","name":"readFile","arguments":"{\"path\":\"a.go\"}"},{"id":"3","name":"bash","arguments":"{\"command\":\"go test ./...\"}"},{"id":"4","name":"editFile","arguments":"{\"path\":\"a.go\"}"},{"id":"5","name":"bash","arguments":"{\"command\":\"go test ./...\"}"}]}`
	ex := NewExtractorWithOptions(ExtractOptions{TStepSplit: true})
	slices, err := ex.Extract(testSessionJSONL(calls), SliceMeta{SourceSession: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	var patterns []string
	for _, s := range slices {
		if s.Type == ToolPattern {
			patterns = append(patterns, string(s.Content))
		}
	}
	// Boundary after each "go test": segments are [grep readFile bash] and
	// [editFile bash].
	if len(patterns) != 2 {
		t.Fatalf("step split produced %d T-slices (%v), want 2", len(patterns), patterns)
	}
	if patterns[0] != "grep readFile bash" || patterns[1] != "editFile bash" {
		t.Fatalf("segments = %v, want [grep readFile bash | editFile bash]", patterns)
	}

	// Default extractor stays turn-level: one slice covering all calls.
	def := NewExtractor()
	turnSlices, err := def.Extract(testSessionJSONL(calls), SliceMeta{SourceSession: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	turnPatterns := 0
	for _, s := range turnSlices {
		if s.Type == ToolPattern {
			turnPatterns++
		}
	}
	if turnPatterns != 1 {
		t.Fatalf("default extractor must keep turn-level T-slices, got %d", turnPatterns)
	}
}

// TestExtractorTStepSplitShortSegmentsDropped: segments under minToolSeq are
// noise and must not become slices. Two consecutive single-call verify runs
// produce only 1-call segments — all dropped.
func TestExtractorTStepSplitShortSegmentsDropped(t *testing.T) {
	calls := `{"role":"assistant","tool_calls":[{"id":"1","name":"bash","arguments":"{\"command\":\"go test ./...\"}"},{"id":"2","name":"bash","arguments":"{\"command\":\"go test ./...\"}"}]}`
	ex := NewExtractorWithOptions(ExtractOptions{TStepSplit: true})
	slices, err := ex.Extract(testSessionJSONL(calls), SliceMeta{SourceSession: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range slices {
		if s.Type == ToolPattern {
			t.Fatalf("short segment must be dropped, got %q", string(s.Content))
		}
	}
}
