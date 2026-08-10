package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/embed"
	"semantix/kernel/slice"
)

func TestSearchVectorRetriever(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"修复 go 测试失败", "配置 CI 流水线", "部署到服务器"} {
		sl := &slice.Slice{ID: "v-" + c[:4], Type: slice.Prompt, Scope: slice.Project, Content: []byte(c)}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	code := run([]string{"search", "--query", "修复测试失败", "--db", db, "--retriever", "vector", "--limit", "2"}, &out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("vector search code = %d, out:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "修复 go 测试失败") {
		t.Fatalf("vector search missed the repair slice:\n%s", out.String())
	}
	if strings.HasPrefix(out.String(), "1. score=0.000000") {
		t.Fatalf("vector scores not populated:\n%s", out.String())
	}
}

func TestSearchHybridRetriever(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store, _ := slice.NewFileStore(db)
	for _, c := range []string{"修复 go 测试失败", "配置 CI 流水线"} {
		sl := &slice.Slice{ID: "h-" + c[:4], Type: slice.Prompt, Scope: slice.Project, Content: []byte(c)}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	code := run([]string{"search", "--query", "修复测试失败", "--db", db, "--retriever", "hybrid", "--limit", "2"}, &out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("hybrid search code = %d, out:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "修复 go 测试失败") {
		t.Fatalf("hybrid search missed the repair slice:\n%s", out.String())
	}
}

func TestRRFFuseRanksSharedHitsHigher(t *testing.T) {
	mk := func(id string) *slice.Slice { return &slice.Slice{ID: id, Type: slice.Prompt, Scope: slice.Project, Content: []byte(id)} }
	a, b, c := mk("a"), mk("b"), mk("c")
	bm := []slice.Hit{{Slice: a, Score: 9}, {Slice: b, Score: 2}}
	vec := []embed.Hit{{ID: "b", Score: 0.9}, {ID: "c", Score: 0.8}}
	out := rrfFuse(bm, vec, []*slice.Slice{a, b, c}, 3)
	if len(out) != 3 {
		t.Fatalf("fused = %d, want 3", len(out))
	}
	// "b" ranks in both lists -> highest fused score.
	if out[0].Slice.ID != "b" {
		t.Fatalf("top fused = %q, want b (present in both rankings)", out[0].Slice.ID)
	}
}

type emptyStderr struct{}

func (emptyStderr) Write(p []byte) (int, error) { return len(p), nil }

var _ = bm25.New // keep import used if search tests reference it later
