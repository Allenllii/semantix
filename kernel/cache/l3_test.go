package cache

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"semantix/kernel/bm25"
	"semantix/kernel/fingerprint"
	"semantix/kernel/judge"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// buildTestLib creates a temp project with one dependency file, indexes a
// Result slice captured against it, and returns the pieces for tests.
func buildTestLib(t *testing.T) (root string, idx *bm25.Index, dep string, sl *slice.Slice) {
	t.Helper()
	root = t.TempDir()
	dep = "dep.txt"
	if err := os.WriteFile(filepath.Join(root, dep), []byte("module demo\nv1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := fingerprint.Capture(root, []string{dep})
	if err != nil {
		t.Fatal(err)
	}
	mtimes := map[string]int64{}
	st, err := os.Stat(filepath.Join(root, dep))
	if err != nil {
		t.Fatal(err)
	}
	mtimes[dep] = st.ModTime().Unix()

	idx = bm25.New()
	sl = &slice.Slice{
		ID:      "l3-1",
		Type:    slice.Result,
		Scope:   slice.Project,
		Content: []byte("已复用的验证结果：修复 go 测试失败需要先跑 go vet"),
		Meta: slice.SliceMeta{
			SourceSession: "s1",
			Deps:          deps,
			Mtimes:        mtimes,
		},
	}
	idx.Insert(sl)
	return root, idx, dep, sl
}

func TestDecideL3ReusesWhenDepsUnchanged(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	d := &L3Decider{Index: idx, Root: root}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected reuse, got nil")
	}
	if res.SliceID != "l3-1" {
		t.Fatalf("SliceID = %s, want l3-1", res.SliceID)
	}
	if res.Response == "" {
		t.Fatal("response must carry the cached content")
	}
}

func TestDecideL3RejectsWhenDepModified(t *testing.T) {
	root, idx, dep, _ := buildTestLib(t)
	// Modify the dependency: mtime fast-fail must reject.
	if err := os.WriteFile(filepath.Join(root, dep), []byte("module demo\nv2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &L3Decider{Index: idx, Root: root}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatal("modified dep must reject reuse (fail-closed), got reuse")
	}
}

func TestDecideL3RejectsWhenDepDeleted(t *testing.T) {
	root, idx, dep, _ := buildTestLib(t)
	if err := os.Remove(filepath.Join(root, dep)); err != nil {
		t.Fatal(err)
	}
	d := &L3Decider{Index: idx, Root: root}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatal("deleted dep must reject reuse (fail-closed), got reuse")
	}
}

func TestDecideL3SkipsNonResultSlices(t *testing.T) {
	root := t.TempDir()
	dep := "dep.txt"
	if err := os.WriteFile(filepath.Join(root, dep), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := fingerprint.Capture(root, []string{dep})
	if err != nil {
		t.Fatal(err)
	}
	idx := bm25.New()
	// A Prompt slice with identical content: not reusable as L3 outcome.
	idx.Insert(&slice.Slice{
		ID:      "l3-p",
		Type:    slice.Prompt,
		Scope:   slice.Project,
		Content: []byte("已复用的验证结果：修复 go 测试失败需要先跑 go vet"),
		Meta:    slice.SliceMeta{SourceSession: "s1", Deps: deps},
	})
	d := &L3Decider{Index: idx, Root: root}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatal("Prompt slice must not be reused as L3 result")
	}
}

func TestDecideL3NoDepsRequiresOptIn(t *testing.T) {
	idx := bm25.New()
	// Without Deps AND without L3Safe: must NOT be reusable (MEDIUM fix —
	// a shared/injected library cannot mark results reusable by omission).
	idx.Insert(&slice.Slice{
		ID:      "l3-nodeps-unsafe",
		Type:    slice.Result,
		Scope:   slice.Project,
		Content: []byte("无依赖结果：格式化代码用 gofmt -w ."),
		Meta:    slice.SliceMeta{SourceSession: "s2"},
	})
	// With explicit opt-in: reusable.
	idx.Insert(&slice.Slice{
		ID:      "l3-nodeps-safe",
		Type:    slice.Result,
		Scope:   slice.Project,
		Content: []byte("无依赖结果：格式化代码用 gofmt -w ."),
		Meta:    slice.SliceMeta{SourceSession: "s2", L3Safe: true},
	})
	d := &L3Decider{Index: idx, Root: t.TempDir()}
	q := Query{UserInput: "格式化代码", Scope: slice.Project}
	res, err := d.DecideL3(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("the opt-in slice must be reusable")
	}
	if res.SliceID != "l3-nodeps-safe" {
		t.Fatalf("got %s, want the opt-in slice (unsafe one must be skipped)", res.SliceID)
	}
}

func TestDecideL3EmptyQueryNoHit(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	d := &L3Decider{Index: idx, Root: root}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "完全不相关的主题", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatal("unrelated query must not reuse")
	}
}

func TestDecideL3MtimeChangeWithSameContentRejects(t *testing.T) {
	root, idx, dep, _ := buildTestLib(t)
	// Backdate the file: mtime changes (even with identical content) →
	// fast-fail rejects (conservative: mtime drift is a stale signal).
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(root, dep), past, past); err != nil {
		t.Fatal(err)
	}
	d := &L3Decider{Index: idx, Root: root}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatal("mtime change must reject (fast-fail semantics), got reuse")
	}
}

func TestDecideL3RejectsInconsistentKeys(t *testing.T) {
	// Mtimes present but Deps empty: inconsistent entry → reject (LOW fix).
	idx := bm25.New()
	idx.Insert(&slice.Slice{
		ID:      "l3-inconsistent",
		Type:    slice.Result,
		Scope:   slice.Project,
		Content: []byte("不一致元数据的结果"),
		Meta: slice.SliceMeta{
			SourceSession: "s4",
			Mtimes:        map[string]int64{"dep.txt": 123},
		},
	})
	d := &L3Decider{Index: idx, Root: t.TempDir()}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "不一致元数据", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatal("inconsistent Mtimes/Deps entry must be rejected")
	}
}

func TestDecideL3RejectsSymlinkedDep(t *testing.T) {
	root := t.TempDir()
	dep := "dep.txt"
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, dep)); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	deps, err := fingerprint.Capture(root, []string{dep})
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(root, dep))
	if err != nil {
		t.Fatal(err)
	}
	idx := bm25.New()
	idx.Insert(&slice.Slice{
		ID:      "l3-symlink",
		Type:    slice.Result,
		Scope:   slice.Project,
		Content: []byte("符号链接依赖的结果"),
		Meta: slice.SliceMeta{
			SourceSession: "s5",
			Deps:          deps,
			Mtimes:        map[string]int64{dep: st.ModTime().Unix()},
		},
	})
	d := &L3Decider{Index: idx, Root: root}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "符号链接依赖", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatal("symlinked dep must be rejected (Lstat guard)")
	}
}

func TestDecideL3RejectsSymlinkInDepsOnly(t *testing.T) {
	// Deps-only entry (no Mtimes, legacy data): the uniform Deps guard must
	// still reject a symlinked dependency (MEDIUM fix, sa_20260811_123652).
	root := t.TempDir()
	dep := "dep.txt"
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, dep)); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	deps, err := fingerprint.Capture(root, []string{dep})
	if err != nil {
		t.Fatal(err)
	}
	idx := bm25.New()
	idx.Insert(&slice.Slice{
		ID:      "l3-symlink-nomtime",
		Type:    slice.Result,
		Scope:   slice.Project,
		Content: []byte("符号链接依赖（无 mtime 快照）的结果"),
		Meta: slice.SliceMeta{
			SourceSession: "s6",
			Deps:          deps, // no Mtimes: legacy entry
		},
	})
	d := &L3Decider{Index: idx, Root: root}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "符号链接依赖", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatal("symlinked dep in Deps-only entry must be rejected")
	}
}

func TestDecideL2FiltersGrey(t *testing.T) {
	root, idx, dep, _ := buildTestLib(t)
	if err := os.WriteFile(filepath.Join(root, dep), []byte("module demo\nv3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Add a weaker unrelated slice so top1 is clear.
	idx.Insert(&slice.Slice{
		ID:      "l3-weak",
		Type:    slice.Prompt,
		Scope:   slice.Project,
		Content: []byte("完全无关的内容关于烹饪食谱"),
		Meta:    slice.SliceMeta{SourceSession: "s3"},
	})
	d := &L3Decider{Index: idx, Root: root}
	hits, err := d.DecideL2(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Slice.ID == "l3-weak" {
			t.Fatal("grey/miss candidate must be filtered out of L2 injection")
		}
	}
}

// TestDecideL3ContextIsolation (Issue #133): a cached outcome stamped with a
// different messages-context hash or model must never be served — the same
// query under another history or model fails closed.
func TestDecideL3ContextIsolation(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	d := &L3Decider{Index: idx, Root: root}
	ctx := context.Background()

	// legacy entry (no stamp) with a context/model-carrying query is
	// rejected: the gateway must never serve an outcome whose provenance
	// is unknown (fail closed).
	res, err := d.DecideL3(ctx, Query{
		UserInput: "修复 go 测试失败", Scope: slice.Project,
		ContextHash: "ctx-a", Model: "ds",
	})
	if err != nil || res != nil {
		t.Fatalf("unstamped entry must fail closed under context query: %v %v", res, err)
	}

	// stamped with matching context+model → reuse
	stamped := &slice.Slice{
		ID: "l3-ctx", Type: slice.Result, Scope: slice.Project,
		Content: []byte("修复 go 测试失败 的缓存答案"),
		Meta: slice.SliceMeta{
			Deps: idxDep(t, root), Mtimes: idxMtimes(t, root),
			ContextHash: "ctx-a", Model: "ds",
		},
	}
	idx.Insert(stamped)
	res, err = d.DecideL3(ctx, Query{
		UserInput: "修复 go 测试失败", Scope: slice.Project,
		ContextHash: "ctx-a", Model: "ds",
	})
	if err != nil || res == nil || res.SliceID != "l3-ctx" {
		t.Fatalf("matching context/model must reuse: %v %v", res, err)
	}

	// different context → rejected
	res, err = d.DecideL3(ctx, Query{
		UserInput: "修复 go 测试失败", Scope: slice.Project,
		ContextHash: "ctx-b", Model: "ds",
	})
	if err != nil || res != nil {
		t.Fatalf("different context must fail closed: %v %v", res, err)
	}

	// different model → rejected
	res, err = d.DecideL3(ctx, Query{
		UserInput: "修复 go 测试失败", Scope: slice.Project,
		ContextHash: "ctx-a", Model: "claude",
	})
	if err != nil || res != nil {
		t.Fatalf("different model must fail closed: %v %v", res, err)
	}
}

func idxDep(t *testing.T, root string) fingerprint.Deps {
	t.Helper()
	deps, err := fingerprint.Capture(root, []string{"dep.txt"})
	if err != nil {
		t.Fatal(err)
	}
	return deps
}

func idxMtimes(t *testing.T, root string) map[string]int64 {
	t.Helper()
	st, err := os.Stat(filepath.Join(root, "dep.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]int64{"dep.txt": st.ModTime().Unix()}
}

// ---- Issue #186 / GW6: grey-zone judge wiring (spec §3.5 RuleGate.Chain) ----

type mockJudge struct {
	confirm bool
	err     error
	got     *judge.Candidate
}

func (m *mockJudge) Confirm(ctx context.Context, c judge.Candidate) (bool, error) {
	cc := c
	m.got = &cc
	return m.confirm, m.err
}

// greyZones forces every candidate into the grey band: TauHigh above 1 means
// no score/top1 ratio can reach Hit, so even the top-1 candidate is Grey.
func greyZones() *zone.Zones {
	return &zone.Zones{TauHigh: 1.5, TauLow: 0.5, AbsHigh: 0.7, AbsLow: 0.45}
}

func TestDecideL3GreyZoneWithoutJudgeRejects(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones()}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatalf("grey without judge must be conservatively rejected, got %+v", res)
	}
}

func TestDecideL3GreyZoneJudgeConfirmsReuse(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	mj := &mockJudge{confirm: true}
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: mj}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("judge-confirmed grey candidate should be reused, got nil")
	}
	if res.SliceID != "l3-1" {
		t.Fatalf("SliceID = %q, want l3-1", res.SliceID)
	}
	if mj.got == nil {
		t.Fatal("judge was not consulted")
	}
	if mj.got.Zone != zone.Grey || mj.got.Query != "修复 go 测试失败" || mj.got.SliceID != "l3-1" {
		t.Fatalf("candidate = %+v, want zone=grey query/slice propagated", mj.got)
	}
	if len(mj.got.Deps) == 0 {
		t.Fatal("candidate deps must be forwarded to the judge (spec §3.5 fingerprint gate)")
	}
}

func TestDecideL3GreyZoneJudgeRejectsReuse(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	mj := &mockJudge{confirm: false}
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: mj}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatalf("judge-declined grey candidate must not be reused, got %+v", res)
	}
}

func TestDecideL3GreyZoneJudgeErrorFailsClosed(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	mj := &mockJudge{confirm: true, err: context.DeadlineExceeded}
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: mj}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatalf("judge error must fail closed, got %+v", res)
	}
}

// fakeIndex returns a fixed hit list; used to exercise the lexical gate
// independent of a real retriever.
type fakeIndex struct {
	hits []slice.Hit
}

func (f *fakeIndex) Insert(*slice.Slice) error { return nil }
func (f *fakeIndex) Remove(string) error       { return nil }
func (f *fakeIndex) Search(string, int, slice.Scope) ([]slice.Hit, error) {
	return f.hits, nil
}

// l3Fixture is a dependency-captured Result slice that passes the
// fingerprint gates when nothing changed. It returns the dep root so the
// decider can verify against it.
func l3Fixture(t *testing.T) (string, *slice.Slice) {
	root := t.TempDir()
	dep := "dep.txt"
	if err := os.WriteFile(filepath.Join(root, dep), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := fingerprint.Capture(root, []string{dep})
	if err != nil {
		t.Fatal(err)
	}
	return root, &slice.Slice{ID: "l3-g", Type: slice.Result, Scope: slice.Project,
		Content: []byte("复用结果：修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "s1", Deps: deps}}
}

func TestL3LexicalGateBlocksPureVectorHit(t *testing.T) {
	root, sl := l3Fixture(t)
	idx := &fakeIndex{hits: []slice.Hit{{Slice: sl, Score: 0.9, Lexical: 0, LexicalValid: true}}}
	d := &L3Decider{Index: idx, Root: root}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatal("pure-vector hit without judge must be downgraded and rejected")
	}
}
func TestL3LexicalGateAllowsLexicalHit(t *testing.T) {
	root, sl := l3Fixture(t)
	idx := &fakeIndex{hits: []slice.Hit{{Slice: sl, Score: 0.9, Lexical: 1, LexicalValid: true}}}
	d := &L3Decider{Index: idx, Root: root}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.SliceID != "l3-g" {
		t.Fatalf("lexically supported Hit must reuse, got %+v", res)
	}
}

func TestL3LexicalGateIgnoresUnmeasured(t *testing.T) {
	root, sl := l3Fixture(t)
	// Zero-value LexicalValid=false: legacy/third-party index, not measured.
	idx := &fakeIndex{hits: []slice.Hit{{Slice: sl, Score: 0.9}}}
	d := &L3Decider{Index: idx, Root: root}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("unmeasured lexical support must not block reuse")
	}
}

func TestL3LexicalGateJudgeApprovesDowngrade(t *testing.T) {
	root, sl := l3Fixture(t)
	idx := &fakeIndex{hits: []slice.Hit{{Slice: sl, Score: 0.9, Lexical: 0, LexicalValid: true}}}
	d := &L3Decider{Index: idx, Root: root, Judge: &mockJudge{confirm: true}}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("judge-approved downgrade must reuse")
	}
}

func TestL3LexicalGateObservations(t *testing.T) {
	root, sl := l3Fixture(t)
	idx := &fakeIndex{hits: []slice.Hit{
		{Slice: sl, Score: 0.9, Lexical: 0, LexicalValid: true},
		{Slice: sl, Score: 0.8, Lexical: 1, LexicalValid: true},
	}}
	var obs []LexicalGateObservation
	d := &L3Decider{Index: idx, Root: root,
		OnLexicalGate: func(_ context.Context, o LexicalGateObservation) { obs = append(obs, o) }}
	if _, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project}); err != nil {
		t.Fatal(err)
	}
	if len(obs) != 2 {
		t.Fatalf("observations = %d, want 2 (blocked + allowed)", len(obs))
	}
	if !obs[0].Blocked || obs[0].Zone != "grey" || obs[0].Lexical != 0 {
		t.Fatalf("obs[0] = %+v, want blocked grey lexical=0", obs[0])
	}
	if obs[1].Blocked || obs[1].Zone != "hit" || obs[1].Lexical != 1 {
		t.Fatalf("obs[1] = %+v, want allowed hit lexical=1", obs[1])
	}
}

// --- Issue #262: negative observability (Obs / OnDecide / ObsAccum) ---

// wantObs asserts every field of the observed snapshot. Zero expectations
// are meaningful: each rejection class must be counted separately and
// nothing may leak across classes.
func wantObs(t *testing.T, got, want Obs, what string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: obs = %+v, want %+v", what, got, want)
	}
}

func TestL3DeciderObsCounters(t *testing.T) {
	t.Run("reuse counts candidate+reused", func(t *testing.T) {
		root, idx, _, _ := buildTestLib(t)
		d := &L3Decider{Index: idx, Root: root, Obs: &ObsAccum{}}
		res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
		if err != nil || res == nil {
			t.Fatalf("reuse failed: res=%v err=%v", res, err)
		}
		wantObs(t, d.Obs.Snapshot(), Obs{Candidates: 1, Reused: 1}, "clear hit reuse")
	})

	t.Run("modified deps count as fingerprint reject", func(t *testing.T) {
		root, idx, dep, _ := buildTestLib(t)
		if err := os.WriteFile(filepath.Join(root, dep), []byte("module demo\nv2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		d := &L3Decider{Index: idx, Root: root, Obs: &ObsAccum{}}
		if res, _ := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project}); res != nil {
			t.Fatal("modified deps must reject")
		}
		wantObs(t, d.Obs.Snapshot(), Obs{Candidates: 1, FingerprintReject: 1}, "dep modified")
	})

	t.Run("context isolation counts separately", func(t *testing.T) {
		root, idx, _, _ := buildTestLib(t)
		d := &L3Decider{Index: idx, Root: root, Obs: &ObsAccum{}}
		q := Query{UserInput: "修复 go 测试失败", Scope: slice.Project, ContextHash: "h-other"}
		if res, _ := d.DecideL3(context.Background(), q); res != nil {
			t.Fatal("context mismatch must reject")
		}
		wantObs(t, d.Obs.Snapshot(), Obs{Candidates: 1, IsolatedReject: 1}, "context isolation")
	})

	t.Run("grey without judge counts rules reject", func(t *testing.T) {
		root, idx, _, _ := buildTestLib(t)
		d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Obs: &ObsAccum{}}
		if res, _ := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project}); res != nil {
			t.Fatal("grey without judge must reject")
		}
		wantObs(t, d.Obs.Snapshot(), Obs{Candidates: 1, Grey: 1, RulesReject: 1}, "grey no judge")
	})

	t.Run("judge declined counts judge reject", func(t *testing.T) {
		root, idx, _, _ := buildTestLib(t)
		mj := &mockJudge{confirm: false}
		d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: mj, Obs: &ObsAccum{}}
		if res, _ := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project}); res != nil {
			t.Fatal("judge declined must reject")
		}
		wantObs(t, d.Obs.Snapshot(), Obs{Candidates: 1, Grey: 1, JudgeReject: 1}, "judge declined")
	})

	t.Run("judge approved then verified counts approve+reused", func(t *testing.T) {
		root, idx, _, _ := buildTestLib(t)
		mj := &mockJudge{confirm: true}
		d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: mj, Obs: &ObsAccum{}}
		res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
		if err != nil || res == nil {
			t.Fatalf("judge-approved grey must reuse: res=%v err=%v", res, err)
		}
		wantObs(t, d.Obs.Snapshot(), Obs{Candidates: 1, Grey: 1, JudgeApproved: 1, Reused: 1}, "judge approved")
	})

	// Issue #245: a judge that cannot be reached is unavailability, not a
	// rule rejection and not a decline. wantObs compares the whole struct,
	// so these also assert RulesReject == 0 and JudgeReject == 0 for free.
	t.Run("judge error counts judge error not rules reject", func(t *testing.T) {
		root, idx, _, _ := buildTestLib(t)
		mj := &mockJudge{confirm: true, err: context.DeadlineExceeded}
		d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: mj, Obs: &ObsAccum{}}
		if res, _ := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project}); res != nil {
			t.Fatal("judge error must fail closed")
		}
		wantObs(t, d.Obs.Snapshot(), Obs{Candidates: 1, Grey: 1, JudgeError: 1}, "grey-arm judge error")
	})

	// The second arm that reaches judgeGrey: an Issue #260 lexical-support
	// downgrade of a zone.Hit. JudgeError is therefore not a grey-verdict-only
	// counter, and the spec's definition depends on both arms feeding it.
	t.Run("lexical-gate judge error counts judge error", func(t *testing.T) {
		root, sl := l3Fixture(t)
		idx := &fakeIndex{hits: []slice.Hit{{Slice: sl, Score: 0.9, Lexical: 0, LexicalValid: true}}}
		mj := &mockJudge{confirm: true, err: context.DeadlineExceeded}
		d := &L3Decider{Index: idx, Root: root, Judge: mj, Obs: &ObsAccum{}}
		if res, _ := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project}); res != nil {
			t.Fatal("lexical-downgraded hit with an erroring judge must fail closed")
		}
		wantObs(t, d.Obs.Snapshot(), Obs{Candidates: 1, Grey: 1, JudgeError: 1}, "lexical-arm judge error")
	})
}

func TestL3DeciderOnDecidePerCallDelta(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	var calls []Obs
	d := &L3Decider{
		Index: idx, Root: root,
		OnDecide: func(o Obs) { calls = append(calls, o) },
	}
	// Two calls with different outcomes: hit-reuse, then miss (unrelated).
	if res, _ := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project}); res == nil {
		t.Fatal("first call must reuse")
	}
	if res, _ := d.DecideL3(context.Background(), Query{UserInput: "完全不相关的主题", Scope: slice.Project}); res != nil {
		t.Fatal("second call must miss")
	}
	if len(calls) != 2 {
		t.Fatalf("OnDecide calls = %d, want 2", len(calls))
	}
	wantObs(t, calls[0], Obs{Candidates: 1, Reused: 1}, "delta call 1")
	// The unrelated query still retrieves the Result slice but the zone
	// verdict is miss → rules reject (Candidates + RulesReject, no reuse).
	wantObs(t, calls[1], Obs{Candidates: 1, RulesReject: 1}, "delta call 2 (clear miss)")
}

func TestL3ObsAccumSnapshotIsThreadSafe(t *testing.T) {
	acc := &ObsAccum{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				acc.add(Obs{Candidates: 1, JudgeReject: 1, JudgeError: 1})
				_ = acc.Snapshot()
			}
		}()
	}
	wg.Wait()
	got := acc.Snapshot()
	want := Obs{Candidates: 800, JudgeReject: 800, JudgeError: 800}
	if got != want {
		t.Fatalf("accumulated = %+v, want %+v", got, want)
	}
}

// Per-type thresholds participate in the real L3 decision (Issue #259
// 阶段 2): the same Result candidate that reuses under the global
// baseline is rejected when its type carries a stricter override.
func TestDecideL3PerTypeOverride(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	// Baseline: global defaults (nil Zones → zone.Default).
	base := &L3Decider{Index: idx, Root: root}
	res, err := base.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("baseline must reuse the Result candidate")
	}

	// Strict result override: unreachable thresholds for this type.
	strict := zone.Zones{TauHigh: 1.0, TauLow: 0.99, AbsHigh: 1e9, AbsLow: 1e9,
		ByType: map[string]zone.Zones{
			"result": {TauHigh: 1.0, TauLow: 0.99, AbsHigh: 1e9, AbsLow: 1e9},
		}}
	dec := &L3Decider{Index: idx, Root: root, Zones: &strict}
	res2, err := dec.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res2 != nil {
		t.Fatalf("strict result override must reject reuse, got %+v", res2)
	}
}

// stubAdapt returns a fixed learned TauLow per slice.
type stubAdapt struct{ tau float64 }

func (a stubAdapt) TauLow(sliceID string, global float64) float64 { return a.tau }

// The per-entry adaptive TauLow replaces the effective grey floor before
// classification (Issue #259 阶段 3): a candidate whose relative
// confidence sits in the grey band under the baseline falls to Miss once
// the entry's learned TauLow rises above it, while a learned value above
// TauHigh is clamped so the grey zone never collapses.
func TestDecideL3AdaptiveTauLow(t *testing.T) {
	// Two Result candidates: a perfect one (relConf 1.0, always a Hit) and
	// a weaker peer with relConf 0.70 (score 0.7 / resultTop1 1.0) — grey
	// under the default TauLow 0.55. The weaker one is listed first so the
	// loop reaches it before the clear Hit; the Obs counters reveal which
	// gate routed it.
	mk := func() []slice.Hit {
		return []slice.Hit{hitOf("weak", slice.Result, 0.7), hitOf("strong", slice.Result, 1.0)}
	}
	observe := func(tau float64) (Obs, *L3Result) {
		d := &L3Decider{Index: &fixedIndex{hits: mk()}, Root: t.TempDir(), Adapt: stubAdapt{tau: tau}}
		var acc ObsAccum
		d.Obs = &acc
		res, _ := d.DecideL3(context.Background(), Query{UserInput: "q", Scope: slice.Project})
		return acc.Snapshot(), res
	}
	// TauLow 0.50: the weak candidate (0.70) stays grey-routed.
	lo, resLo := observe(0.50)
	if lo.Grey != 1 || lo.RulesReject != 1 || resLo == nil || resLo.SliceID != "strong" {
		t.Fatalf("tau 0.50: grey=%d rules_reject=%d res=%+v, want grey-routed weak + strong reuse", lo.Grey, lo.RulesReject, resLo)
	}
	// TauLow 0.80 (clamped to TauHigh-0.05 = 0.75): 0.70 < 0.75 → Miss.
	hi, resHi := observe(0.80)
	if hi.Grey != 0 || hi.RulesReject != 1 || resHi == nil || resHi.SliceID != "strong" {
		t.Fatalf("tau 0.80: grey=%d rules_reject=%d res=%+v, want straight-Miss weak + strong reuse", hi.Grey, hi.RulesReject, resHi)
	}
}

