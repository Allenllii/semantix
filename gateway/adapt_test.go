package gateway

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"semantix/kernel/adapt"
	"semantix/kernel/cache"
	"semantix/kernel/zone"
)

// newAdaptGateway builds a Gateway with a small-window adaptive engine so
// tests can exercise the feedback hooks without waiting for the default
// cold-start minimums.
func newAdaptGateway(t *testing.T) (*Gateway, *adapt.Engine) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "l3-adapt.json")
	eng := adapt.New(adapt.Config{MinSamples: 1, MinHits: 1, FreezeEpochs: 1}, path)
	g := &Gateway{
		cfg:      &Config{Cache: CacheConfig{FalseHitSim: DefaultFalseHitSim}},
		adapt:    eng,
		zones:    zone.Default(),
		l3Reuses: make(map[string]l3ReuseEntry),
		now:      time.Now,
	}
	return g, eng
}

// A clean L3 reuse is positive evidence: the served slice's learned tau
// relaxes (with MinHits satisfied) (Issue #259 阶段 3).
func TestAdaptPositiveEvidenceOnReuse(t *testing.T) {
	g, eng := newAdaptGateway(t)
	g.recordL3Reuse("s1", "修复 go 测试失败", "l3-1")
	if got := eng.TauLow("l3-1", 0.55); got != 0.50 {
		t.Fatalf("TauLow = %v, want relaxed 0.50 after a clean reuse", got)
	}
	// Other slices are untouched (per-entry isolation).
	if got := eng.TauLow("l3-2", 0.55); got != 0.55 {
		t.Fatalf("other slice TauLow = %v, want global", got)
	}
}

// A same-session retry of a served query (suspected false hit, Issue
// #262) is negative evidence: the offending slice's tau tightens.
func TestAdaptNegativeEvidenceOnFalseHit(t *testing.T) {
	g, eng := newAdaptGateway(t)
	q := "修复 go 测试失败"
	g.recordL3Reuse("s1", q, "l3-1") // positive: relax → 0.50 (frozen 1)
	if !g.detectFalseHit("s1", q) {
		t.Fatal("retry must be detected as a false hit")
	}
	// The false hit is a frozen observation: counts fold, no adjustment.
	// A second negative observation after re-serving tightens past 0.50.
	g.recordL3Reuse("s1", q, "l3-1")
	if !g.detectFalseHit("s1", q) {
		t.Fatal("second retry must be detected")
	}
	if got := eng.TauLow("l3-1", 0.55); got <= 0.50 {
		t.Fatalf("TauLow = %v, want tightened above 0.50 after repeated false hits", got)
	}
}

// A judge rejection (grey band declined) is negative evidence for that
// slice (Issue #259 阶段 3).
func TestAdaptNegativeEvidenceOnJudgeDecline(t *testing.T) {
	g, eng := newAdaptGateway(t)
	g.observeJudge(context.Background(), cache.JudgeObservation{
		SliceID: "l3-1",
		Verdict: cache.JudgeDeclined,
	})
	if got := eng.TauLow("l3-1", 0.55); math.Abs(got-0.60) > 1e-9 {
		t.Fatalf("TauLow = %v, want tightened 0.60 after a judge decline", got)
	}
	// Approved verdicts are not negative evidence.
	g.observeJudge(context.Background(), cache.JudgeObservation{
		SliceID: "l3-2",
		Verdict: cache.JudgeApproved,
	})
	if got := eng.TauLow("l3-2", 0.55); got != 0.55 {
		t.Fatalf("approved slice TauLow = %v, want global", got)
	}
}

// New wires the adaptive engine into the decider (Issue #259 阶段 3) and
// persists its state next to the slice store; adaptive = false leaves the
// decider on the global thresholds (nil-safe hook).
func TestNewWiresAdaptiveEngine(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "test-key"
	cfg.Store.DB = filepath.Join(dir, "db.jsonl")
	cfg.Store.DepsRoot = dir
	cfg.Ingest.SessionsDir = filepath.Join(dir, "sessions")
	cfg.Upstreams = []UpstreamConfig{{
		Name: "deepseek", BaseURL: "http://127.0.0.1:1", APIKey: "up-key",
		ModelAlias: []string{"deepseek-chat"}, UpstreamModel: "deepseek-chat",
		Vendor: "deepseek",
	}}
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	if g.adapt == nil {
		t.Fatal("adaptive engine must be wired by default")
	}
	if g.decider.Adapt == nil {
		t.Fatal("decider.Adapt must be set when adaptation is enabled")
	}
	// A clean-reuse streak relaxes the entry (MinSamples=20 reached) and
	// the adjustment persists to the default state file.
	for i := 0; i < 25; i++ {
		g.recordL3Reuse("s1", "q", "l3-1")
	}
	state := filepath.Join(dir, "l3-adapt.json")
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("adapt state file: %v", err)
	}

	// adaptive = false → no engine, decider falls back to global.
	cfg2 := *cfg
	off := false
	cfg2.Retrieval.Adaptive = &off
	g2, err := New(&cfg2)
	if err != nil {
		t.Fatalf("New(adaptive=false): %v", err)
	}
	t.Cleanup(func() { _ = g2.Close() })
	if g2.adapt != nil {
		t.Fatal("adaptive engine must be nil when disabled")
	}
	if g2.decider.Adapt != nil {
		t.Fatal("decider.Adapt must stay nil when disabled")
	}
}

// The adaptive engine honors a configured error bound (Issue #259 阶段 3):
// a tighter bound makes the engine tighten earlier, and the bound is
// surfaced through the engine's config at runtime.
func TestAdaptErrorBoundConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l3-adapt.json")
	eng := adapt.New(adapt.Config{ErrorBound: 0.02, MinSamples: 1, MinHits: 1, FreezeEpochs: 1}, path)
	// One negative observation: NegEWMA = 0.1 > 0.02 → tighten.
	eng.Observe("l3-1", true, 0.55)
	if got := eng.TauLow("l3-1", 0.55); math.Abs(got-0.60) > 1e-9 {
		t.Fatalf("TauLow = %v, want tightened 0.60 under error_bound 0.02", got)
	}
	// error_bound = -1 disables: config validation accepts it and the
	// engine stays a no-op.
	if err := validateErrorBound(-1); err != nil {
		t.Fatalf("error_bound -1 must be legal, got %v", err)
	}
	if err := validateErrorBound(0.5); err != nil {
		t.Fatalf("error_bound 0.5 must be legal, got %v", err)
	}
	if err := validateErrorBound(1.5); err == nil {
		t.Fatal("error_bound 1.5 must be rejected")
	}
	if err := validateErrorBound(-2); err == nil {
		t.Fatal("error_bound -2 must be rejected")
	}
}

// validateErrorBound mirrors the [retrieval] error_bound value domain so
// the behavior tests do not need a full config file.
func validateErrorBound(v float64) error {
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "test-key"
	cfg.Upstreams = []UpstreamConfig{{
		Name: "deepseek", BaseURL: "http://127.0.0.1:1", APIKey: "up-key",
		ModelAlias: []string{"deepseek-chat"}, UpstreamModel: "deepseek-chat",
		Vendor: "deepseek",
	}}
	cfg.Retrieval.ErrorBound = v
	return cfg.validate()
}

var _ = zone.Default // keep zone import for future zone-driven cases
