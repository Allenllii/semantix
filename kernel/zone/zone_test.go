package zone

import (
	"math"
	"testing"
)

func nan() float64      { return math.NaN() }
func inf() float64      { return math.Inf(1) }
func infNeg() float64   { return math.Inf(-1) }

func TestClassifyBoundaries(t *testing.T) {
	z := Default()
	cases := []struct {
		name             string
		score, top1      float64
		want             Zone
	}{
		{"top1 clear hit", 0.9, 0.9, Hit},
		{"relative hit", 0.75, 0.85, Hit},       // conf 0.88 >= 0.8, top1 >= 0.7
		{"relative grey", 0.6, 0.85, Grey},      // conf 0.71 in [0.55, 0.8)
		{"relative miss", 0.3, 0.85, Miss},      // conf 0.35 < 0.55
		{"absolute top1 floor miss", 0.9, 0.4, Miss}, // top1 < AbsLow
		{"top1 weak not hit despite conf=1", 0.55, 0.55, Grey}, // conf 1 but top1 < AbsHigh
		{"bm25 scale hit", 3.0, 3.07, Hit},      // conf ~0.98, abs guards don't bind
		{"bm25 scale grey", 1.9, 3.07, Grey},    // conf ~0.62
		{"bm25 scale miss", 0.8, 3.07, Miss},    // conf ~0.26
		{"negative score", -0.2, 0.9, Miss},
		{"zero top1", 0.5, 0, Miss},
		{"nan score", nan(), 0.9, Miss},          // all NaN comparisons false -> default Miss
		{"nan top1", 0.5, nan(), Miss},
		{"positive inf", inf(), 3.0, Miss},       // inf/inf -> NaN -> Miss
		{"negative inf top1", 0.5, infNeg(), Miss},
	}
	for _, c := range cases {
		if got := z.Classify(c.score, c.top1); got != c.want {
			t.Errorf("%s: Classify(%v, %v) = %v, want %v", c.name, c.score, c.top1, got, c.want)
		}
	}
}

func TestZonesString(t *testing.T) {
	if Miss.String() != "miss" || Grey.String() != "grey" || Hit.String() != "hit" {
		t.Fatalf("String() tags: %v %v %v", Miss, Grey, Hit)
	}
}

func TestCustomThresholds(t *testing.T) {
	z := Zones{TauHigh: 0.9, TauLow: 0.7, AbsHigh: 0.8, AbsLow: 0.5}
	if got := z.Classify(0.85, 0.95); got != Grey { // conf 0.89 < TauHigh 0.9
		t.Errorf("stricter TauHigh: got %v, want Grey", got)
	}
	if got := z.Classify(0.88, 0.92); got != Hit { // conf 0.956 >= 0.9, top1 0.92 >= 0.8 -> Hit
		t.Errorf("got %v, want Hit", got)
	}
}

// TestClassifyL3TwoAxis pins the Issue #241 verdict on the GW4 acceptance
// numbers (docs/reports/gateway-m1-acceptance.md §5.2): every repeated task
// stored a Prompt slice byte-identical to the query, which BM25 ranked
// first, so globalTop1 is the twin's score and resultTop1 the best Result's.
// With the raw-list denominator only t07 (rel 0.826) cleared TauHigh; the
// two-axis verdict must restore all ten genuinely reusable tasks to Hit —
// real BM25 puts rewritten same-task answers at ~0.52–0.83 of the twin and
// same-field-different-task documents near 0.16, so the AbsLow relevance
// floor (0.45) separates the clusters without killing the tail.
func TestClassifyL3TwoAxis(t *testing.T) {
	z := Default()
	cases := []struct {
		name                       string
		score, resultTop1, globalTop1 float64
		want                       Zone
	}{
		// GW4 replay: best Result measured against its own twin.
		{"t07", 47.03, 47.03, 56.97, Hit},  // relGlobal 0.826
		{"t04", 47.13, 47.13, 65.31, Hit},  // relGlobal 0.722
		{"t05", 36.66, 36.66, 50.83, Hit},  // relGlobal 0.721
		{"t10", 44.95, 44.95, 66.57, Hit},  // relGlobal 0.675
		{"t08", 25.90, 25.90, 43.01, Hit},  // relGlobal 0.602
		{"t02", 35.46, 35.46, 59.78, Hit},  // relGlobal 0.593
		{"t03", 34.62, 34.62, 59.33, Hit},  // relGlobal 0.584
		{"t01", 30.86, 30.86, 54.49, Hit},  // relGlobal 0.566
		{"t09", 42.94, 42.94, 79.26, Hit},  // relGlobal 0.542 >= AbsLow 0.45
		{"t06", 34.50, 34.50, 65.72, Hit},  // relGlobal 0.525 >= AbsLow 0.45

		// Non-best peers: prominence axis still discriminates.
		{"second peer prominent", 42.0, 47.03, 56.97, Hit},  // relSame 0.893 >= 0.8, relGlobal 0.737
		{"second peer grey", 35.0, 47.03, 56.97, Grey},      // relSame 0.744, relGlobal 0.615
		{"second peer weak grey", 30.0, 47.03, 56.97, Grey}, // relSame 0.638, relGlobal 0.527 >= AbsLow
		{"second peer off-floor miss", 22.0, 47.03, 56.97, Miss}, // relGlobal 0.386 < AbsLow

		// Absolute floors still bind on the scale anchor: a lone weak
		// BM25 hit (no twin to inflate globalTop1) cannot self-certify.
		{"lone weak bm25 grey", 0.5, 0.5, 0.5, Grey},   // rel 1.0 but globalTop1 < AbsHigh
		{"lone weak bm25 miss", 0.3, 0.3, 0.3, Miss},   // globalTop1 < AbsLow

		// Failure-safe guards.
		{"zero resultTop1", 0.5, 0, 1.0, Miss},
		{"negative globalTop1", 0.5, 0.5, -1, Miss},
		{"nan score", nan(), 0.5, 1.0, Miss},
		{"inf globalTop1", 0.5, 0.5, inf(), Miss},
	}
	for _, c := range cases {
		if got := z.ClassifyL3(c.score, c.resultTop1, c.globalTop1); got != c.want {
			t.Errorf("%s: ClassifyL3(%v, %v, %v) = %v, want %v",
				c.name, c.score, c.resultTop1, c.globalTop1, got, c.want)
		}
	}
}

// Per-type overrides resolve to the override snapshot when the type is
// configured and to the global baseline otherwise (Issue #259 阶段 2).
func TestForTypeOverrideAndFallback(t *testing.T) {
	base := Default()
	base.ByType = map[string]Zones{
		"result": {TauHigh: 0.85, TauLow: 0.60, AbsHigh: 0.75, AbsLow: 0.50},
	}
	if got := base.ForType("result"); got.TauHigh != 0.85 || got.TauLow != 0.60 {
		t.Fatalf("ForType(result) = %+v, want override", got)
	}
	// An override is a leaf snapshot: its own ByType must be cleared.
	if got := base.ForType("result"); got.ByType != nil {
		t.Fatalf("override must not carry nested ByType, got %+v", got.ByType)
	}
	if got := base.ForType("prompt"); got.TauHigh != base.TauHigh || got.TauLow != base.TauLow {
		t.Fatalf("ForType(prompt) = %+v, want global baseline %+v", got, base)
	}
	// Unknown type names fall back too (forward compat with new types).
	if got := base.ForType("unknown"); got.TauHigh != base.TauHigh {
		t.Fatalf("ForType(unknown) = %+v, want global baseline", got)
	}
}

// A zero ByType (nil map) keeps the historical behavior: every type sees
// the global thresholds, and ForType is a no-op identity.
func TestForTypeNilByTypeIdentity(t *testing.T) {
	z := Default()
	got := z.ForType("result")
	if got.TauHigh != z.TauHigh || got.TauLow != z.TauLow ||
		got.AbsHigh != z.AbsHigh || got.AbsLow != z.AbsLow {
		t.Fatalf("ForType with nil ByType must return the global thresholds, got %+v", got)
	}
}

// The per-type override participates in real classification, not just
// field lookup: a score that is a clear Hit under a relaxed override can
// be Grey under the stricter baseline (R slices answer users directly).
func TestForTypeChangesClassification(t *testing.T) {
	base := Default()
	base.ByType = map[string]Zones{
		"context": {TauHigh: 0.70, TauLow: 0.45, AbsHigh: 0.70, AbsLow: 0.45},
	}
	const score, top1 = 0.75, 1.0
	if got := base.Classify(score, top1); got != Grey {
		t.Fatalf("global classify = %v, want Grey", got)
	}
	if got := base.ForType("context").Classify(score, top1); got != Hit {
		t.Fatalf("context classify = %v, want Hit under relaxed override", got)
	}
	// Types without an override keep the stricter global verdict.
	if got := base.ForType("result").Classify(score, top1); got != Grey {
		t.Fatalf("result classify = %v, want Grey (no override)", got)
	}
}
