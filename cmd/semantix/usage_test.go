package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/usage"
)

func TestRunUsageSummary(t *testing.T) {
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
	if code := runUsage([]string{"--db", logPath}, &out); code != 0 {
		t.Fatalf("usage exit code = %d, want 0", code)
	}
	s := out.String()
	for _, want := range []string{
		"events\t2", "tokens_in\t1800", "cache_hit_tokens\t600",
		"l3_reuses\t1", "savings_usd\t", "savings_rate\t",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("output missing %q:\n%s", want, s)
		}
	}
}

func TestRunUsageMissingDB(t *testing.T) {
	var out bytes.Buffer
	if code := runUsage([]string{"--db", filepath.Join(t.TempDir(), "nope.jsonl")}, &out); code != 2 {
		t.Fatalf("missing db must exit 2, got %d", code)
	}
}

func TestRunUsageWithEvolve(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "usage.jsonl")
	r, err := usage.NewRecorder(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Append(usage.Event{Turn: 1, TokensIn: 1000, TokensOut: 200, CacheHitToken: 900}); err != nil {
		t.Fatal(err)
	}
	evolveDir := filepath.Join(dir, "evolve")
	var out bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--evolve-db", evolveDir}, &out); code != 0 {
		t.Fatalf("usage --evolve exit code = %d, want 0", code)
	}
	s := out.String()
	if !strings.Contains(s, "evolve_epoch\t1") {
		t.Fatalf("evolve output missing epoch:\n%s", s)
	}
	if !strings.Contains(s, "evolve_tau_l2\t") {
		t.Fatalf("evolve output missing tau:\n%s", s)
	}
	// State file persisted (0600).
	st, err := os.Stat(filepath.Join(evolveDir, "params.json"))
	if err != nil {
		t.Fatalf("evolve state not persisted: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("evolve state perms = %o, want 600", st.Mode().Perm())
	}
	// Second run loads state (epoch advances).
	var out2 bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--evolve-db", evolveDir}, &out2); code != 0 {
		t.Fatalf("second usage --evolve exit code = %d", code)
	}
	if !strings.Contains(out2.String(), "evolve_epoch\t2") {
		t.Fatalf("second run must advance epoch:\n%s", out2.String())
	}
}
