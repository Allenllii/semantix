package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// helper: write a session JSONL file with the given user turns.
func writeSession(t *testing.T, dir, name string, users []string) string {
	t.Helper()
	var b strings.Builder
	for _, u := range users {
		b.WriteString(`{"role":"user","content":"` + u + `"}` + "\n")
		b.WriteString(`{"role":"assistant","content":"done"}` + "\n")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	// mtime controls train/replay split; bump each file by a minute.
	mt := time.Now().Add(time.Duration(len(dir))*time.Minute + time.Minute)
	_ = os.Chtimes(path, mt, mt)
	return path
}

func TestVerifyReplayProducesEvaluationTable(t *testing.T) {
	dir := t.TempDir()
	// 3 sessions: first two are "earlier" (train), last is replay.
	writeSession(t, dir, "s1.jsonl", []string{"修复 go 测试失败", "加 CI 配置"})
	writeSession(t, dir, "s2.jsonl", []string{"修复 go 测试失败", "优化查询性能"})
	writeSession(t, dir, "s3.jsonl", []string{"修复 go 测试失败", "部署到服务器"})

	var out bytes.Buffer
	db := filepath.Join(t.TempDir(), "v.db")
	code := runVerify([]string{"--session", dir, "--db", db, "--holdout", "0.33"}, &out, productionDependencies())
	if code != 0 {
		t.Fatalf("runVerify code = %d, want 0; out:\n%s", code, out.String())
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var rows int
	for _, l := range lines {
		if strings.HasPrefix(l, "#") || strings.HasPrefix(l, "session\t") {
			continue
		}
		rows++
		cols := strings.Split(l, "\t")
		if len(cols) != 5 {
			t.Fatalf("row %q: want 5 tab columns, got %d", l, len(cols))
		}
	}
	if rows != 2 {
		t.Fatalf("replay rows = %d, want 2 (one per replay-session turn)", rows)
	}
	// The replayed "修复 go 测试失败" turn must have a trained top-1 hit
	// (score > 0) — the exact same instruction exists in s1/s2.
	var foundScore bool
	for _, l := range lines {
		if strings.HasPrefix(l, "#") || strings.HasPrefix(l, "session\t") {
			continue
		}
		cols := strings.Split(l, "\t")
		if strings.Contains(cols[4], "修复 go 测试失败") {
			foundScore = cols[2] != "0.0000"
		}
	}
	if !foundScore {
		t.Fatalf("replayed '修复 go 测试失败' turn did not get a scored top-1 hit:\n%s", out.String())
	}
}

func TestVerifyRequiresTwoSessions(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "only.jsonl", []string{"x"})
	var out bytes.Buffer
	code := runVerify([]string{"--session", dir}, &out, productionDependencies())
	if code != 1 {
		t.Fatalf("runVerify with 1 session = code %d, want 1", code)
	}
	if !strings.Contains(out.String(), "need at least 2 session files") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestVerifyToleratesCorruptLines(t *testing.T) {
	dir := t.TempDir()
	p1 := writeSession(t, dir, "s1.jsonl", []string{"a", "b"})
	// corrupt one line in s1 (must be skipped, not fatal)
	raw, _ := os.ReadFile(p1)
	corrupt := string(raw) + "{not json}\n"
	_ = os.WriteFile(p1, []byte(corrupt), 0o600)
	writeSession(t, dir, "s2.jsonl", []string{"c"})

	var out bytes.Buffer
	code := runVerify([]string{"--session", dir}, &out, productionDependencies())
	if code != 0 {
		t.Fatalf("runVerify = code %d, want 0; out:\n%s", code, out.String())
	}
}
