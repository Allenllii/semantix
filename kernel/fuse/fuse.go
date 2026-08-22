// Package fuse implements hybrid retrieval fusion (Issue #274): the BM25
// and vector routes are merged under a configurable strategy — Weighted
// (the historical score average) or RRF (Reciprocal Rank Fusion,
// SIGIR'09). It is the single source of truth for hybrid fusion: the
// gateway hybridIndex and the search CLI both call Fuse.
//
// Score-scale contract (spec §4): both strategies emit scores on the
// bounded [0,1] scale so the zone classifier's absolute floors
// (AbsHigh/AbsLow) keep their meaning. Weighted normalizes each route
// against its own top-1; RRF scales the reciprocal-rank sum by (k+1)/2 —
// a linear map that preserves ordering and the relative-confidence axis
// (score/top1) while keeping the absolute floors meaningful.
package fuse

import (
	"sort"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

// Strategy selects the fusion algorithm.
type Strategy int

const (
	// Weighted averages the two routes after normalizing each to [0,1]
	// against its own top-1 (the historical gateway fuseHits behavior).
	// BM25Weight configures the BM25 route share; 1 → pure BM25 order,
	// 0 → pure vector order.
	Weighted Strategy = iota
	// RRF sums reciprocal ranks 1/(RrfK+rank) over both routes and scales
	// the sum by (RrfK+1)/2 into [0,1]: two routes at rank 0 → 1.0, one
	// route at rank 0 → 0.5 (spec §4.2).
	RRF
)

// Defaults (spec §5): the RRF constant follows the SIGIR'09 convention;
// the weighted share is the historical 50/50.
const (
	DefaultRrfK       = 60
	DefaultBM25Weight = 0.5
)

// Config is the fusion configuration. The zero value resolves to the
// defaults below.
type Config struct {
	// Strategy selects Weighted (default) or RRF.
	Strategy Strategy
	// RrfK is the RRF constant; 0 → DefaultRrfK.
	RrfK int
	// BM25Weight is the BM25 route share in Weighted mode; nil →
	// DefaultBM25Weight, an explicit 0 selects the pure vector route.
	// It has no effect in RRF mode (rank-based fusion has no weight axis).
	BM25Weight *float64
}

func (c Config) rrfK() int {
	if c.RrfK <= 0 {
		return DefaultRrfK
	}
	return c.RrfK
}

func (c Config) bm25Weight() float64 {
	if c.BM25Weight == nil {
		return DefaultBM25Weight
	}
	return *c.BM25Weight
}

// Fuse merges two route hit lists (bm = BM25 route, vec = vector route,
// each ordered by descending score) into at most k hits ordered by fused
// score. query feeds the RRF-mode lexical support (spec §4.4); it is
// ignored in Weighted mode.
//
// Lexical semantics per strategy: Weighted → the BM25 route's normalized
// contribution (unweighted, so the lexical gate stays independent of the
// fusion weight); RRF → query-token coverage (QueryCoverage), decoupled
// from rank so the 1/(k+r) slow decay cannot silence the lexical gate.
// Every emitted hit carries LexicalValid=true.
func Fuse(bm, vec []slice.Hit, query string, k int, cfg Config) []slice.Hit {
	if k <= 0 {
		return []slice.Hit{}
	}
	var out []slice.Hit
	switch cfg.Strategy {
	case RRF:
		out = fuseRRF(bm, vec, query, k, cfg)
	default:
		out = fuseWeighted(bm, vec, k, cfg)
	}
	if len(out) > k {
		out = out[:k]
	}
	return out
}

// normalize maps a route's scores to [0,1] relative to its own top-1
// (a route with no results or a non-positive top-1 contributes nothing).
func normalize(hits []slice.Hit) map[string]float64 {
	m := map[string]float64{}
	if len(hits) == 0 {
		return m
	}
	top1 := hits[0].Score
	if top1 <= 0 {
		return m
	}
	for _, h := range hits {
		if h.Score > 0 {
			m[h.Slice.ID] = h.Score / top1
		}
	}
	return m
}

// fuseWeighted implements the historical gateway behavior (fuseHits) with
// a configurable BM25 share: score = w·bmNorm + (1-w)·vecNorm ∈ [0,1].
func fuseWeighted(bm, vec []slice.Hit, k int, cfg Config) []slice.Hit {
	w := cfg.bm25Weight()
	bmN := normalize(bm)
	vecN := normalize(vec)
	if len(bmN) == 0 && len(vecN) == 0 {
		return []slice.Hit{}
	}

	merged := map[string]*slice.Slice{}
	for _, h := range bm {
		if _, ok := merged[h.Slice.ID]; !ok {
			merged[h.Slice.ID] = h.Slice
		}
	}
	for _, h := range vec {
		if _, ok := merged[h.Slice.ID]; !ok {
			merged[h.Slice.ID] = h.Slice
		}
	}
	out := make([]slice.Hit, 0, len(merged))
	for id, s := range merged {
		var score float64
		if v, ok := bmN[id]; ok {
			score += w * v
		}
		if v, ok := vecN[id]; ok {
			score += (1 - w) * v
		}
		if score <= 0 {
			continue // no signal from any contributing route
		}
		// Lexical support = the BM25 route's normalized contribution,
		// unweighted: the lexical gate measures "did BM25 support this
		// candidate", independent of the fusion weight (spec §4.4).
		lx := 0.0
		if v, ok := bmN[id]; ok {
			lx = v
		}
		out = append(out, slice.Hit{Slice: s, Score: score, Lexical: lx, LexicalValid: true})
	}
	orderHits(out)
	return out
}

// fuseRRF sums reciprocal ranks and scales into [0,1] (spec §4.2):
//
//	s' = (Σ_r 1/(k+rank)) · (k+1)/2
//
// Two routes at rank 0 → 1.0; one route at rank 0 → 0.5. The map is
// linear, so ordering and score/top1 are unchanged.
func fuseRRF(bm, vec []slice.Hit, query string, k int, cfg Config) []slice.Hit {
	kk := cfg.rrfK()
	scale := float64(kk+1) / 2
	merged := map[string]*slice.Slice{}
	score := map[string]float64{}
	addRoute := func(hits []slice.Hit) {
		for i, h := range hits {
			id := h.Slice.ID
			if _, ok := merged[id]; !ok {
				merged[id] = h.Slice
			}
			// rank starts at 1 (RRF convention): a candidate's score is
			// 1/(k+rank). Both routes at rank 1 → 2/(k+1) → 1.0 after the
			// scale below; one route at rank 1 → 0.5.
			score[id] += 1.0 / (float64(kk) + float64(i) + 1)
		}
	}
	addRoute(bm)
	addRoute(vec)
	if len(score) == 0 {
		return []slice.Hit{}
	}

	out := make([]slice.Hit, 0, len(score))
	for id, s := range merged {
		fused := score[id] * scale
		if fused <= 0 {
			continue
		}
		// Lexical support = query-token coverage, decoupled from rank:
		// a pure-vector hit with no term overlap reads 0 and is blocked by
		// the lexical gate (Issue #260); BM25-route hits are measured by
		// actual overlap, not by the slow 1/(k+r) decay (spec §4.4).
		out = append(out, slice.Hit{
			Slice:        s,
			Score:        fused,
			Lexical:      bm25.QueryCoverage(query, string(s.Content)),
			LexicalValid: true,
		})
	}
	orderHits(out)
	return out
}

// orderHits sorts by descending fused score, tie-broken by ID.
func orderHits(out []slice.Hit) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Slice.ID < out[j].Slice.ID
		}
		return out[i].Score > out[j].Score
	})
}
