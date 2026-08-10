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
