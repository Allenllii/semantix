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
	// Replay semantics (Issue #220): the same log yields the same state —
	// rerunning must NOT advance the epoch, only new events do.
	var out2 bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--evolve-db", evolveDir}, &out2, productionDependencies()); code != 0 {
		t.Fatalf("second usage --evolve exit code = %d", code)
	}
	if !strings.Contains(out2.String(), "evolve_epoch\t1") {
		t.Fatalf("replay over the same log must be idempotent:\n%s", out2.String())
	}
	if err := r.Append(usage.Event{Turn: 2, TokensIn: 1000, TokensOut: 100, CacheHitToken: 500}); err != nil {
		t.Fatal(err)
	}
	var out3 bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--evolve-db", evolveDir}, &out3, productionDependencies()); code != 0 {
		t.Fatalf("third usage --evolve exit code = %d", code)
	}
	if !strings.Contains(out3.String(), "evolve_epoch\t2") {
		t.Fatalf("a new event must advance the epoch:\n%s", out3.String())
	}
}

// TestFeedEvolveAdjustsTauAfterHistory proves the loop is no longer inert
// (Issue #220): with enough high-hit turns the engine leaves the cold-start
// window (FreezeEpochs) and actually moves TauL2 off its default.
func TestFeedEvolveAdjustsTauAfterHistory(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "usage.jsonl")
	r, err := usage.NewRecorder(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 65; i++ {
		if err := r.Append(usage.Event{Turn: uint64(i), TokensIn: 1000, TokensOut: 100, CacheHitToken: 900}); err != nil {
			t.Fatal(err)
		}
	}
	evolveDir := filepath.Join(dir, "evolve")
	var out bytes.Buffer
	if err := feedEvolve(evolveDir, logPath, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "evolve_epoch\t65") {
		t.Fatalf("epoch must count consumed turns:\n%s", s)
	}
	// 65 signals at 0.9 hit ratio: hit EWMA ≥ HitTarget with zero pollution
	// after the 60-epoch cold start → exactly one -TauStep relaxation
	// (0.55 → 0.50), then the freeze window blocks further movement.
	if !strings.Contains(s, "evolve_tau_l2\t0.500") {
		t.Fatalf("tau must relax after sustained hits:\n%s", s)
	}
	// The persisted snapshot carries the adjusted value for consumers.
	st, err := loadEvolveState(evolveDir)
	if err != nil || st == nil {
		t.Fatalf("loadEvolveState: st=%v err=%v", st, err)
	}
	if diff := st.Params.TauL2 - 0.50; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("persisted TauL2 = %v, want 0.50", st.Params.TauL2)
	}
	// Deterministic replay: a second pass reproduces the identical output.
	var again bytes.Buffer
	if err := feedEvolve(evolveDir, logPath, &again); err != nil {
		t.Fatal(err)
	}
	if again.String() != s {
		t.Fatalf("replay must be deterministic:\nfirst:\n%s\nsecond:\n%s", s, again.String())
	}
}

// TestLoadEvolveState covers the consumer-side contract: missing file is a
// silent no-op, corrupt file is a visible error.
func TestLoadEvolveState(t *testing.T) {
	dir := t.TempDir()
	if st, err := loadEvolveState(dir); st != nil || err != nil {
		t.Fatalf("missing file: st=%v err=%v, want nil/nil", st, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "params.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadEvolveState(dir); err == nil {
		t.Fatal("corrupt file must surface an error")
	}
}

// TestRunUsageByProvider (GLM-P0-4, #292): per-provider rows render with
// exact-only hit rates, per-provider prices from [cost.providers.<name>]
// override the global pair, and the --json envelope grows a by_provider list.
func TestRunUsageByProvider(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "usage.jsonl")
	r, err := usage.NewRecorder(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []usage.Event{
		{Turn: 1, Provider: "tokenhub-glm", Exact: true, TokensIn: 1_000_000, TokensOut: 10, CacheHitToken: 500_000},
		{Turn: 2, Provider: "deepseek", TokensIn: 900, TokensOut: 10}, // estimated only
	} {
		if err := r.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(dir, "semantix.toml")
	if err := os.WriteFile(cfgPath, []byte(`
[cost.providers.tokenhub-glm]
input_price_usd = 1.12   # ¥8/M ≈ $1.12
cache_price_usd = 0.28   # ¥2/M ≈ $0.28
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEMANTIX_CONFIG", cfgPath)

	var out, errBuf bytes.Buffer
	if code := run([]string{"usage", "--db", logPath}, &out, &errBuf, productionDependencies()); code != 0 {
		t.Fatalf("usage exit = %d (stderr=%s)", code, errBuf.String())
	}
	s := out.String()
	if !strings.Contains(s, "tokenhub-glm") || !strings.Contains(s, "50.0%") {
		t.Fatalf("missing provider hit-rate row:\n%s", s)
	}
	// 500k hit tokens × ($1.12−$0.28)/M = $0.42 priced from the provider table.
	if !strings.Contains(s, "0.420000") {
		t.Fatalf("provider-priced savings missing (want $0.420000):\n%s", s)
	}
	// The estimated-only endpoint renders an em-dash rate, not 0%.
	if !strings.Contains(s, "deepseek") || !strings.Contains(s, "—") {
		t.Fatalf("estimated-only endpoint should show — :\n%s", s)
	}

	out.Reset()
	if code := run([]string{"usage", "--db", logPath, "--json"}, &out, &errBuf, productionDependencies()); code != 0 {
		t.Fatalf("usage --json exit = %d", code)
	}
	var env struct {
		Data struct {
			ByProvider []struct {
				Provider      string  `json:"provider"`
				ExactEvents   int     `json:"exact_events"`
				L1HitRate     float64 `json:"l1_hit_rate"`
				HitSavingsUSD float64 `json:"hit_savings_usd"`
			} `json:"by_provider"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if len(env.Data.ByProvider) != 2 {
		t.Fatalf("by_provider rows = %d, want 2", len(env.Data.ByProvider))
	}
	glm := env.Data.ByProvider[1] // sorted: deepseek, tokenhub-glm
	if glm.Provider != "tokenhub-glm" || glm.ExactEvents != 1 || glm.L1HitRate != 0.5 {
		t.Fatalf("glm row = %+v", glm)
	}
	if glm.HitSavingsUSD < 0.4199 || glm.HitSavingsUSD > 0.4201 {
		t.Fatalf("glm savings = %v, want ≈0.42", glm.HitSavingsUSD)
	}
}

// TestFeedEvolveNegativeSignalTightensTau covers the Issue #262 negative
// evidence channel: judge rejections and suspected false hits feed the
// pollution EWMA, so the offline replay drives TauL2 in both directions —
// a polluted history must end stricter than a clean one.
func TestFeedEvolveNegativeSignalTightensTau(t *testing.T) {
	dir := t.TempDir()
	writeLog := func(name string, polluted bool) string {
		path := filepath.Join(dir, name)
		r, err := usage.NewRecorder(path)
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 65; i++ {
			ev := usage.Event{Turn: uint64(i), TokensIn: 1000, TokensOut: 100, CacheHitToken: 900}
			if polluted {
				// L3 activity every turn: two grey candidates, one judge
				// rejection (pollution evidence) — never reused.
				ev.L3GreyCandidates = 2
				ev.L3JudgeReject = 1
			}
			if err := r.Append(ev); err != nil {
				t.Fatal(err)
			}
		}
		return path
	}
	clean := writeLog("clean.jsonl", false)
	polluted := writeLog("polluted.jsonl", true)

	var cleanOut, pollutedOut bytes.Buffer
	if err := feedEvolve(filepath.Join(dir, "evolve-clean"), clean, &cleanOut); err != nil {
		t.Fatal(err)
	}
	if err := feedEvolve(filepath.Join(dir, "evolve-polluted"), polluted, &pollutedOut); err != nil {
		t.Fatal(err)
	}
	// Clean history: hit EWMA high, pollution zero → one relaxation
	// (0.55 - 0.05 = 0.50), same as before the negative channel existed.
	if !strings.Contains(cleanOut.String(), "evolve_tau_l2\t0.500") {
		t.Fatalf("clean history must relax TauL2 to 0.500:\n%s", cleanOut.String())
	}
	// Polluted history: judge rejections push the pollution EWMA past
	// PollutionRiseAt → one tightening (0.55 + 0.05 = 0.60).
	if !strings.Contains(pollutedOut.String(), "evolve_tau_l2\t0.600") {
		t.Fatalf("polluted history must tighten TauL2 to 0.600:\n%s", pollutedOut.String())
	}
}

// TestFeedEvolveFalseHitRaisesPollution covers the suspected-false-hit
// signal: a retry that bypassed L3 counts as pollution even without any
// judge activity.
func TestFeedEvolveFalseHitRaisesPollution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fh.jsonl")
	r, err := usage.NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 65; i++ {
		if err := r.Append(usage.Event{Turn: uint64(i), TokensIn: 1000, TokensOut: 100, L3FalseHit: true}); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := feedEvolve(filepath.Join(dir, "evolve"), path, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "evolve_tau_l2\t0.600") {
		t.Fatalf("false hits alone must tighten TauL2 to 0.600:\n%s", out.String())
	}
}
