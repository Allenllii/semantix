package adapt

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// smallCfg returns a config with tiny windows so tests exercise one
// adjustment per observation (FreezeEpochs 1: the next observation may
// adjust again).
func smallCfg() Config {
	return Config{
		ErrorBound:   0.05,
		Step:         0.05,
		MinSamples:   1,
		MinHits:      1,
		FreezeEpochs: 1,
		Alpha:        0.1,
		MinTau:       0.30,
		MaxTau:       0.80,
	}
}

// closeEnough compares floats with a tolerance for 0.05-step arithmetic.
func closeEnough(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Cold start: before the sample minimum, no adjustment happens and TauLow
// returns the global value.
func TestColdStartUsesGlobal(t *testing.T) {
	e := New(Config{}, filepath.Join(t.TempDir(), "adapt.json")) // defaults: MinSamples 20
	if got := e.TauLow("s1", 0.55); got != 0.55 {
		t.Fatalf("cold-start TauLow = %v, want global 0.55", got)
	}
	e.Observe("s1", false, 0.55) // one sample, below MinSamples
	if got := e.TauLow("s1", 0.55); got != 0.55 {
		t.Fatalf("below-MinSamples TauLow = %v, want global 0.55", got)
	}
	if e.Adjustments() != 0 {
		t.Fatalf("Adjustments = %d, want 0", e.Adjustments())
	}
}

// Negative evidence above the error bound tightens the entry's TauLow by
// one step, seeded from the value currently in effect.
func TestNegativeEvidenceTightens(t *testing.T) {
	cfg := smallCfg()
	cfg.FreezeEpochs = 10 // hold the freeze so a second observe cannot relax
	e := New(cfg, filepath.Join(t.TempDir(), "adapt.json"))
	e.Observe("s1", true, 0.55)
	// NegEWMA after one negative = 0.1 > 0.05 → tighten: 0.55 + 0.05.
	if got := e.TauLow("s1", 0.55); !closeEnough(got, 0.60) {
		t.Fatalf("TauLow = %v, want tightened 0.60", got)
	}
	// A second entry stays on the global value (per-entry isolation).
	if got := e.TauLow("s2", 0.55); got != 0.55 {
		t.Fatalf("s2 TauLow = %v, want global 0.55", got)
	}
}

// A clean streak below ErrorBound/2 relaxes the entry's TauLow until the
// MinTau floor. Note the EWMA tail: early clean observations still sit
// above the error bound and keep tightening (same decay discipline as
// kernel/evolve) until the bound is crossed, then relaxation takes over.
func TestCleanStreakRelaxes(t *testing.T) {
	e := New(smallCfg(), filepath.Join(t.TempDir(), "adapt.json"))
	e.Observe("s1", true, 0.55) // tighten → 0.60
	for i := 0; i < 40; i++ {
		e.Observe("s1", false, 0.60) // clean streak decays NegEWMA
	}
	// 40 clean observations: NegEWMA ≈ 0.1*0.9^40 ≈ 0.0015 < 0.025 → relax
	// all the way down until MinTau clamps.
	if got := e.TauLow("s1", 0.55); !closeEnough(got, e.cfg.MinTau) {
		t.Fatalf("TauLow = %v, want MinTau 0.30 after a long clean streak", got)
	}
}

// Tau is clamped to [MinTau, MaxTau] regardless of evidence. A tighten
// that lands above MaxTau still applies immediately (safety direction)
// even though the clamped value sits below the global threshold.
func TestTauClamped(t *testing.T) {
	cfg := smallCfg()
	cfg.MaxTau = 0.40
	e := New(cfg, filepath.Join(t.TempDir(), "adapt.json"))
	e.Observe("s1", true, 0.55) // would be 0.60 → clamped 0.40
	if got := e.TauLow("s1", 0.55); !closeEnough(got, 0.40) {
		t.Fatalf("TauLow = %v, want clamped MaxTau 0.40", got)
	}
}

// Freeze window: after an adjustment, further observations cannot adjust
// again until the freeze expires.
func TestFreezeWindow(t *testing.T) {
	e := New(smallCfg(), filepath.Join(t.TempDir(), "adapt.json"))
	e.Observe("s1", true, 0.55) // adjust → 0.60, frozen 1
	e.Observe("s1", true, 0.60) // frozen → no second adjustment
	if got := e.TauLow("s1", 0.55); !closeEnough(got, 0.60) {
		t.Fatalf("TauLow = %v, want 0.60 (frozen)", got)
	}
	if e.Adjustments() != 1 {
		t.Fatalf("Adjustments = %d, want 1 (freeze must block)", e.Adjustments())
	}
}

// MinHits gates the GENEROUS direction of a learned value: relaxing below
// the value in effect only applies once the entry has enough reuses
// (trust gate for low-traffic slices). The conservative direction
// (tightening) applies immediately — negative evidence must never be
// gated behind traffic volume.
func TestMinHitsGatesLearnedTau(t *testing.T) {
	// Tightening side: one negative observation learns 0.60; Pos=0 but the
	// value applies immediately (safety direction).
	e := New(smallCfg(), filepath.Join(t.TempDir(), "adapt.json"))
	e.Observe("s1", true, 0.55)
	if got := e.TauLow("s1", 0.55); !closeEnough(got, 0.60) {
		t.Fatalf("tightened TauLow = %v, want 0.60 (no MinHits gate on safety direction)", got)
	}

	// Relaxing side: with a high error bound a clean observation learns a
	// value BELOW the one in effect; it must stay gated until Pos >=
	// MinHits.
	cfg := smallCfg()
	cfg.MinHits = 3
	cfg.ErrorBound = 1.0 // NegEWMA=0 is far below ErrorBound/2 → relax
	e2 := New(cfg, filepath.Join(t.TempDir(), "adapt.json"))
	e2.Observe("s1", false, 0.55) // learn 0.50, Pos=1
	if got := e2.TauLow("s1", 0.55); got != 0.55 {
		t.Fatalf("relaxed TauLow = %v, want global 0.55 (Pos=1 < MinHits 3)", got)
	}
	e2.Observe("s1", false, 0.55)
	e2.Observe("s1", false, 0.55) // Pos=3
	if got := e2.TauLow("s1", 0.55); got >= 0.55 {
		t.Fatalf("relaxed TauLow = %v, want learned value < 0.55 once Pos >= MinHits", got)
	}
}

// State round-trips through the JSON file: a fresh engine loads the same
// entries, tau values and counters.
func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adapt.json")
	e := New(smallCfg(), path)
	e.Observe("s1", true, 0.55) // learn + persist (adjustment writes)
	e.Observe("s2", false, 0.55)

	e2 := New(smallCfg(), path)
	if got := e2.TauLow("s1", 0.55); !closeEnough(got, 0.60) {
		t.Fatalf("reloaded s1 TauLow = %v, want 0.60", got)
	}
	// s2 learned a relaxed value (0.50) that is in effect at MinHits=1.
	if got := e2.TauLow("s2", 0.55); !closeEnough(got, 0.50) {
		t.Fatalf("reloaded s2 TauLow = %v, want learned 0.50", got)
	}
	// An entry that was never observed stays on the global value.
	if got := e2.TauLow("s3", 0.55); got != 0.55 {
		t.Fatalf("reloaded s3 TauLow = %v, want global", got)
	}
	snap := e2.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot = %d entries, want 2", len(snap))
	}
	if snap[0].SliceID != "s1" || snap[1].SliceID != "s2" {
		t.Fatalf("Snapshot order = %+v, want s1,s2 sorted", snap)
	}
}

// A corrupt state file must not block the engine: log and start cold.
func TestCorruptStateStartsCold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adapt.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(smallCfg(), path)
	if got := e.TauLow("s1", 0.55); got != 0.55 {
		t.Fatalf("corrupt-state TauLow = %v, want global", got)
	}
	e.Observe("s1", true, 0.55) // must still work
	if got := e.TauLow("s1", 0.55); !closeEnough(got, 0.60) {
		t.Fatalf("post-corrupt observe TauLow = %v, want 0.60", got)
	}
}

// ErrorBound < 0 disables adaptation: observations are no-ops and TauLow
// always returns the global value.
func TestDisabledEngine(t *testing.T) {
	cfg := smallCfg()
	cfg.ErrorBound = -1
	e := New(cfg, filepath.Join(t.TempDir(), "adapt.json"))
	e.Observe("s1", true, 0.55)
	if got := e.TauLow("s1", 0.55); got != 0.55 {
		t.Fatalf("disabled TauLow = %v, want global", got)
	}
	if e.Adjustments() != 0 {
		t.Fatalf("disabled Adjustments = %d, want 0", e.Adjustments())
	}
}

// DefaultAdaptDB places the state next to the slice store.
func TestDefaultAdaptDB(t *testing.T) {
	got := DefaultAdaptDB(filepath.Join("data", "gw.jsonl"))
	want := filepath.Join("data", "l3-adapt.json")
	if got != want {
		t.Fatalf("DefaultAdaptDB = %q, want %q", got, want)
	}
}

// A relax-then-tighten sequence resets the Relaxed gate: after tightening,
// the learned value applies immediately even with Pos below MinHits —
// negative evidence must never be gated behind traffic volume (review
// finding, Issue #259 阶段 3).
func TestTightenAfterRelaxResetsGate(t *testing.T) {
	cfg := smallCfg()
	cfg.MinHits = 5 // generous gate: a relaxed value would stay hidden
	e := New(cfg, filepath.Join(t.TempDir(), "adapt.json"))
	// Relax first (high error bound → clean observation loosens).
	e.Observe("s1", false, 0.55) // learn 0.50, Relaxed=true, Pos=1
	if got := e.TauLow("s1", 0.55); got != 0.55 {
		t.Fatalf("relaxed value must be gated at Pos=1 < MinHits 5, got %v", got)
	}
	// Then a negative observation (frozen window passes in one step).
	e.Observe("s1", true, 0.55) // frozen: counts fold, no adjust
	e.Observe("s1", true, 0.55) // NegEWMA > bound → tighten to 0.55, Relaxed=false
	// Query with a different global (0.60): the learned 0.55 must apply
	// immediately — a gated value would return the global 0.60.
	if got := e.TauLow("s1", 0.60); !closeEnough(got, 0.55) {
		t.Fatalf("tightened value must apply immediately after relax→tighten, got %v (want 0.55)", got)
	}
}
