package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"semantix/kernel/bm25"
	"semantix/kernel/fingerprint"
	"semantix/kernel/slice"
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
		ID:    "l3-p",
		Type:  slice.Prompt,
		Scope: slice.Project,
		Content: []byte("已复用的验证结果：修复 go 测试失败需要先跑 go vet"),
		Meta:  slice.SliceMeta{SourceSession: "s1", Deps: deps},
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

func TestDecideL3NoDepsIsEligible(t *testing.T) {
	idx := bm25.New()
	idx.Insert(&slice.Slice{
		ID:      "l3-nodeps",
		Type:    slice.Result,
		Scope:   slice.Project,
		Content: []byte("无依赖结果：格式化代码用 gofmt -w ."),
		Meta:    slice.SliceMeta{SourceSession: "s2"}, // no Deps
	})
	d := &L3Decider{Index: idx, Root: t.TempDir()}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "格式化代码", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.SliceID != "l3-nodeps" {
		t.Fatalf("slice without deps must be reusable, got %+v", res)
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
