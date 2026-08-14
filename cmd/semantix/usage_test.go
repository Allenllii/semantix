package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
		{Turn: 1, TokensIn: 1000, TokensOut: 200, CacheHitToken: 600, SliceHits: 2},
		{Turn: 2, TokensIn: 800, TokensOut: 150, L3Reuse: true},
	} {
		if err := r.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if code := runUsage([]string{"--db", logPath}, &out, productionDependencies()); code != 0 {
		t.Fatalf("usage exit code = %d, want 0", code)
	}
	s := out.String()
	for _, want := range []string{
		"events\t2", "tokens_in\t1800", "cache_hit_tokens\t600",
		"l3_reuses\t1", "savings_usd\t", "savings_rate\t",
		// Iconic summary (Issue #152).
		"💰 节省成本", "📈 节省率", "🧠 L3 复用", "📦 命中切片", "slice_hits\t2", "█",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("output missing %q:\n%s", want, s)
		}
	}
}

func TestRunUsageMissingDB(t *testing.T) {
	var out bytes.Buffer
	if code := runUsage([]string{"--db", filepath.Join(t.TempDir(), "nope.jsonl")}, &out, productionDependencies()); code != 1 {
		t.Fatalf("missing db is a runtime/IO error, must exit 1, got %d", code)
	}
}

// TestRunUsageJSON verifies --json emits a jq-parseable envelope whose data
// appends the new slice_hits field while keeping the older fields.
func TestRunUsageJSON(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "usage.jsonl")
	r, err := usage.NewRecorder(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Append(usage.Event{Turn: 1, TokensIn: 1000, TokensOut: 200, CacheHitToken: 600, SliceHits: 4}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--json"}, &out, productionDependencies()); code != 0 {
		t.Fatalf("usage --json exit code = %d, want 0", code)
	}
	var env struct {
		OK      bool   `json:"ok"`
		Command string `json:"command"`
		Data    struct {
			Events    int `json:"events"`
			SliceHits int `json:"slice_hits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("--json output not parseable: %v\n%s", err, out.String())
	}
	if !env.OK || env.Command != "usage" {
		t.Fatalf("envelope = %+v", env)
	}
	if env.Data.SliceHits != 4 || env.Data.Events != 1 {
		t.Fatalf("data = %+v, want slice_hits=4 events=1", env.Data)
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
	if code := runUsage([]string{"--db", logPath, "--evolve-db", evolveDir}, &out, productionDependencies()); code != 0 {
		t.Fatalf("usage --evolve exit code = %d, want 0", code)
	}
	s := out.String()
	if !strings.Contains(s, "evolve_epoch\t1") {
		t.Fatalf("evolve output missing epoch:\n%s", s)
	}
	if !strings.Contains(s, "evolve_tau_l2\t") {
		t.Fatalf("evolve output missing tau:\n%s", s)
	}
	// State file persisted (0600 on POSIX; Windows ignores the perm bits).
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(evolveDir, "params.json"))
		if err != nil {
			t.Fatalf("evolve state not persisted: %v", err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("evolve state perms = %o, want 600", st.Mode().Perm())
		}
	}
	// Second run loads state (epoch advances).
	var out2 bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--evolve-db", evolveDir}, &out2, productionDependencies()); code != 0 {
		t.Fatalf("second usage --evolve exit code = %d", code)
	}
	if !strings.Contains(out2.String(), "evolve_epoch\t2") {
		t.Fatalf("second run must advance epoch:\n%s", out2.String())
	}
}
