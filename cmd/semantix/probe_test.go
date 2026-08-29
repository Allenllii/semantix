package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeSessionPathsFromFlagAndDir(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "b.jsonl")
	b := filepath.Join(dir, "a.jsonl")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// --dir results are name-sorted regardless of directory order.
	paths, err := probeSessionPaths("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || filepath.Base(paths[0]) != "a.jsonl" {
		t.Fatalf("dir paths = %v, want a.jsonl first", paths)
	}
	// Explicit flag preserves the given (meaningful chronological) order.
	paths, err = probeSessionPaths(b+","+a, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != b {
		t.Fatalf("flag order not preserved: %v", paths)
	}
	// Neither flag set is a usage error.
	if _, err := probeSessionPaths("", ""); err == nil {
		t.Fatal("expected error with no sessions")
	}
}

func TestProbeUserQueriesTurnBoundaries(t *testing.T) {
	data := []byte("{\"role\":\"user\",\"content\":\"first task long enough\"}\n" +
		"{\"role\":\"assistant\",\"content\":\"ok\"}\n" +
		"{\"role\":\"user\",\"content\":\"short\"}\n" + // below minLen: skipped
		"{\"role\":\"user\",\"content\":\"second task long enough\"}\n" +
		"not json at all\n")
	queries := probeUserQueries(data, 10)
	if len(queries) != 2 || queries[0] != "first task long enough" || queries[1] != "second task long enough" {
		t.Fatalf("queries = %v, want the two long user turns", queries)
	}
}

func TestProbeToolQueries(t *testing.T) {
	data := []byte(
		`{"role":"user","content":"task"}` + "\n" +
			`{"role":"assistant","tool_calls":[{"id":"1","name":"grep"},{"id":"2","name":"readFile"}]}` + "\n" +
			`{"role":"user","content":"next"}` + "\n" +
			`{"role":"assistant","tool_calls":[{"id":"3","name":"editFile"}]}` + "\n") // single call: dropped
	queries := probeToolQueries(data)
	if len(queries) != 1 || queries[0] != "grep readFile" {
		t.Fatalf("tool queries = %v, want [grep readFile]", queries)
	}
}

func TestProbeUserQueriesCJKTokensKeptWhole(t *testing.T) {
	// Chinese content survives verbatim (no tokenization here).
	q := "修复 go 测试失败的具体步骤"
	data := []byte("{\"role\":\"user\",\"content\":\"" + q + "\"}\n")
	got := probeUserQueries(data, 8)
	if len(got) != 1 || got[0] != q {
		t.Fatalf("queries = %v, want [%q]", got, q)
	}
}
