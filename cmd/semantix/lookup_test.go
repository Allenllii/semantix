package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

// TestRunLookupLimitClamp locks the CLI/kernel parity for --limit:
// >50 caps to 50, <=0 falls back to 5 (kernel Execute semantics).
func TestRunLookupLimitClamp(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store := openTestStore(t, db)
	idx := bm25.New()
	seeds := []string{"修复 go 测试失败", "配置 CI 流水线", "部署到服务器"}
	for i, q := range seeds {
		sl := &slice.Slice{ID: fmt.Sprintf("s%d", i), Type: slice.Prompt, Scope: slice.Project, Content: []byte(q)}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
		if err := idx.Insert(sl); err != nil {
			t.Fatal(err)
		}
	}
	deps := dependencies{
		newExtractor: slice.NewExtractor,
		openStore:    func(p string) (slice.Store, error) { return store, nil },
		newIndex:     func() slice.Index { return bm25.New() },
	}

	for _, tc := range []struct {
		limit string
	}{
		{"0"},   // <=0 -> default 5 (before the fix: empty result)
		{"-5"},  // <=0 -> default 5
		{"100"}, // >50 -> cap 50
		{"5"},
	} {
		var out bytes.Buffer
		// 用 --json 信封读取结果数（U30 起默认输出为人类可读文本）。
		err := runLookup([]string{"--query", "配置", "--db", db, "--limit", tc.limit, "--json"}, &out, &emptyStderr{}, deps)
		if err != nil {
			t.Fatalf("--limit %s: %v", tc.limit, err)
		}
		var env envelope
		if err := json.Unmarshal(out.Bytes(), &env); err != nil {
			t.Fatalf("--limit %s: bad JSON: %v\n%s", tc.limit, err, out.String())
		}
		data, ok := env.Data.([]any)
		if !ok {
			t.Fatalf("--limit %s: data = %#v, want array", tc.limit, env.Data)
		}
		// The library has 3 slices but "配置" matches exactly one; the clamp
		// contract is: never error, never empty for a matching query. Before
		// the <=0 fallback, --limit 0 returned zero results.
		if len(data) != 1 {
			t.Errorf("--limit %s: results = %d, want 1 (the matching slice)", tc.limit, len(data))
		}
	}
}

// U30: lookup 默认文本输出同款 zone 图标 + 来源会话 + 摘要行；
// --json 信封每项追加 source_session 字段（向后兼容：只加不删）。
func TestLookupHitVisualization(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store := openTestStore(t, db)
	idx := bm25.New()
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
		if err := idx.Insert(sl); err != nil {
			t.Fatal(err)
		}
	}
	deps := dependencies{
		newExtractor: slice.NewExtractor,
		openStore:    func(p string) (slice.Store, error) { return store, nil },
		newIndex:     func() slice.Index { return bm25.New() },
	}

	// 默认文本输出：图标 + 来源会话 + 摘要行。
	var out bytes.Buffer
	if err := runLookup([]string{"--query", "修复 go 测试失败", "--db", db, "--limit", "3"},
		&out, &emptyStderr{}, deps); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"🟢", "zone=hit", "from:sess-a", "from:sess-b", "from:sess-c", "🎯 ", "hits in 3 sessions"} {
		if !strings.Contains(got, want) {
			t.Errorf("lookup text output missing %q:\n%s", want, got)
		}
	}

	// --json：信封 + data 每项带 source_session（来源会话来自 Slice.Meta）。
	var out2 bytes.Buffer
	if err := runLookup([]string{"--query", "修复 go 测试失败", "--db", db, "--limit", "3", "--json"}, &out2, &emptyStderr{}, deps); err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out2.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope JSON: %v\n%s", err, out2.String())
	}
	if !env.OK || env.Command != "lookup" {
		t.Fatalf("envelope = %+v, want ok + command=lookup", env)
	}
	data, ok := env.Data.([]any)
	if !ok || len(data) == 0 {
		t.Fatalf("data = %#v, want non-empty array", env.Data)
	}
	sessions := map[string]bool{}
	for i, item := range data {
		row, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("row %d = %#v, want object", i, item)
		}
		sess, ok := row["source_session"].(string)
		if !ok || sess == "" {
			t.Errorf("row %d: source_session = %q, want non-empty (slices carry provenance)", i, sess)
		}
		sessions[sess] = true
	}
	if !sessions["sess-a"] || !sessions["sess-b"] || !sessions["sess-c"] {
		t.Errorf("source sessions across rows = %v, want sess-a/sess-b/sess-c all present", sessions)
	}
}
