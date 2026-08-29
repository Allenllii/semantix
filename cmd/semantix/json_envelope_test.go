package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
	"semantix/kernel/usage"
)

// M2-U22: every command that supports --json wraps its payload in the unified
// envelope (§4.2): {ok, command, data, error, version}.

func TestLookupJSONEnvelope(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store := openTestStore(t, db)
	sl := &slice.Slice{ID: "s1", Type: slice.Prompt, Scope: slice.Project, Content: []byte("修复 go 测试失败")}
	if err := store.Put(sl); err != nil {
		t.Fatal(err)
	}
	deps := dependencies{
		newExtractor: slice.NewExtractor,
		openStore:    func(p string) (slice.Store, error) { return store, nil },
		newIndex:     func() slice.Index { return bm25.New() },
	}

	var out bytes.Buffer
	if err := runLookup([]string{"--query", "修复", "--db", db, "--json"}, &out, &emptyStderr{}, deps); err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope JSON: %v\n%s", err, out.String())
	}
	if !env.OK || env.Command != "lookup" || env.Error != nil {
		t.Fatalf("envelope = %+v, want ok + command=lookup + no error", env)
	}
	data, ok := env.Data.([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("data = %#v, want 1 result row", env.Data)
	}
	// U30: 每项追加 source_session 字段（向后兼容：只加不删）。
	row, ok := data[0].(map[string]any)
	if !ok {
		t.Fatalf("row = %#v, want object", data[0])
	}
	if _, ok := row["source_session"]; !ok {
		t.Fatalf("row missing source_session field: %#v", row)
	}

	// 无 --json 为 vibe-coder 可读文本（U30）：zone 图标 + 摘要行。
	// 机器可读形态统一走 --json 信封（H1 协议入口）。
	var out2 bytes.Buffer
	if err := runLookup([]string{"--query", "修复", "--db", db}, &out2, &emptyStderr{}, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.ContainsAny(out2.String(), "🟢🟡⚪") {
		t.Fatalf("default lookup output missing zone icon:\n%s", out2.String())
	}
	if !strings.Contains(out2.String(), "🎯 ") {
		t.Fatalf("default lookup output missing hits summary:\n%s", out2.String())
	}
}

func TestSearchJSONEnvelope(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store := openTestStore(t, db)
	sl := &slice.Slice{ID: "s1", Type: slice.Prompt, Scope: slice.Project, Content: []byte("配置 CI 流水线")}
	if err := store.Put(sl); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := run([]string{"search", "--query", "CI", "--db", db, "--json"}, &out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("search code = %d, out:\n%s", code, out.String())
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out.String())
	}
	if !env.OK || env.Command != "search" || env.Error != nil {
		t.Fatalf("envelope = %+v, want ok + command=search", env)
	}
	data, ok := env.Data.([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("data = %#v, want 1 result", env.Data)
	}
}

func TestUsageJSONEnvelope(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "usage.jsonl")
	r, err := usage.NewRecorder(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []usage.Event{
		{Turn: 1, TokensIn: 1000, TokensOut: 200, CacheHitToken: 600},
		{Turn: 2, TokensIn: 800, TokensOut: 150, L3Reuse: true},
	} {
		if err := r.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--json"}, &out, productionDependencies()); code != 0 {
		t.Fatalf("usage code = %d, out:\n%s", code, out.String())
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out.String())
	}
	if !env.OK || env.Command != "usage" || env.Error != nil {
		t.Fatalf("envelope = %+v, want ok + command=usage", env)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want object", env.Data)
	}
	if _, ok := data["savings_rate"]; !ok {
		t.Fatalf("data missing savings_rate: %#v", data)
	}
}

func TestVerifyJSONEnvelope(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "s1.jsonl", []string{"修复 go 测试失败", "加 CI 配置"})
	writeSession(t, dir, "s2.jsonl", []string{"修复 go 测试失败", "优化查询性能"})
	writeSession(t, dir, "s3.jsonl", []string{"修复 go 测试失败", "部署到服务器"})
	db := filepath.Join(t.TempDir(), "v.db")
	var out bytes.Buffer
	code := runVerify([]string{"--session", dir, "--db", db, "--holdout", "0.33", "--json"}, &out, productionDependencies())
	if code != 0 {
		t.Fatalf("verify code = %d, out:\n%s", code, out.String())
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out.String())
	}
	if !env.OK || env.Command != "verify" || env.Error != nil {
		t.Fatalf("envelope = %+v, want ok + command=verify", env)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want object", env.Data)
	}
	rows, ok := data["rows"].([]any)
	if !ok {
		t.Fatalf("rows = %#v, want array", data["rows"])
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (one per replay-session turn)", len(rows))
	}
}
