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

// U30: search 文本输出带 zone 图标 + 来源会话 + 命中摘要行（vibe-coder 可读）。
// s1 与 query 全匹配（必为 🟢 hit）；s2/s3 部分匹配、分数非零，保证三行都进
// 结果，跨会话复用（3 个来源会话）可感知。
func TestSearchHitVisualization(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		id, sess, content string
	}{
		{"s1", "sess-a", "修复 go 测试失败"},
		{"s2", "sess-b", "修复 go 测试超时"},
		{"s3", "sess-c", "修复 go 测试崩溃"},
	} {
		sl := &slice.Slice{ID: s.id, Type: slice.Prompt, Scope: slice.Project, Content: []byte(s.content),
			Meta: slice.SliceMeta{SourceSession: s.sess}}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	code := run([]string{"search", "--query", "修复 go 测试失败", "--db", db, "--limit", "3", "--retriever", "bm25"},
		&out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("search code = %d, out:\n%s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{"🟢", "zone=hit", "from:sess-a", "from:sess-b", "from:sess-c", "🎯 ", "hits in 3 sessions"} {
		if !strings.Contains(got, want) {
			t.Errorf("search output missing %q:\n%s", want, got)
		}
	}
}

type emptyStderr struct{}

func (emptyStderr) Write(p []byte) (int, error) { return len(p), nil }

var _ = bm25.New // keep import used if search tests reference it later
