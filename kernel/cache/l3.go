package cache

import (
	"context"
	"os"
	"path/filepath"

	"semantix/kernel/fingerprint"
	"semantix/kernel/judge"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// L3Decider implements Decider with a fail-closed L3 path (Issue #59 / U16):
// a Result slice is reusable only when (a) retrieval finds it clearly
// relevant (zone Hit), (b) its dependency files still match the captured
// mtimes (fast path) and (c) the sha256 fingerprint still matches (authority
// path). Any failure in the chain rejects reuse — never returns stale data.
type L3Decider struct {
	Index slice.Index
	Store slice.Store // optional; used to re-read full slices by ID
	Root  string      // dependency root (project dir) for mtime/Verify
	Zones *zone.Zones // grey-zone classifier (nil → zone.Default)
	Judge judge.Judge // grey-zone LLM judge (nil → conservative reject, kernel/judge RuleGate)
	K     int         // top-k candidates (default 3)
}

// DecideL2 returns top-k hits filtered by the grey zone (hit-only enters the
// injection block; grey/miss are skipped — Krites §3.1).
func (d *L3Decider) DecideL2(ctx context.Context, q Query) ([]slice.Hit, error) {
	z := d.zones()
	k := d.k()
	hits, err := d.Index.Search(q.UserInput, k, q.Scope)
	if err != nil {
		return nil, err
	}
	top1 := 0.0
	if len(hits) > 0 {
		top1 = hits[0].Score
	}
	out := hits[:0]
	for _, h := range hits {
		if z.Classify(h.Score, top1) == zone.Hit {
			out = append(out, h)
		}
	}
	return out, nil
}

// DecideL3 returns a verified reusable result, or nil when any gate fails
// (fail-closed). Verification chain, cheapest first:
//
//	1. retrieval: Result-typed slice, zone Hit — classified against the
//	   best Result-typed candidate, not the raw top-1 hit (Issue #241)
//	2. mtime fast-fail: every captured dep's mtime unchanged
//	3. fingerprint authority: Verify reports zero changed paths
//
// A slice with no dependency capture (Deps nil) is eligible without
// verification — it depended on nothing, so nothing can go stale.
func (d *L3Decider) DecideL3(ctx context.Context, q Query) (*L3Result, error) {
	z := d.zones()
	k := d.k()
	hits, err := d.Index.Search(q.UserInput, k, q.Scope)
	if err != nil {
		return nil, err
	}
	// Type filter FIRST, then the denominator (Issue #241). zone.Classify
	// measures a candidate's relative confidence against the best candidate
	// in the same comparison set, so the set must be the one L3 actually
	// considers: Result-typed slices. Taking top1 from the raw hit list made
	// every Result compete against the byte-identical Prompt twin the
	// gateway stores for the same query — under BM25 that twin always ranks
	// first (measured 43–79 vs the Results' 26–47), which depressed every
	// Result's score/top1 for a reason unrelated to whether it is reusable
	// (GW4 acceptance §5.2: 8 of 10 repeated tasks landed in grey, 2 in
	// miss). The absolute floors (AbsHigh/AbsLow) still apply, and because
	// top1(Result) <= top1(all) they now bind SOONER, not later: on a
	// bounded (cosine) scale this change can only make the absolute guard
	// stricter. Candidate membership is unchanged — the same Search, the
	// same k, the same hits — only the denominator moves.
	cands := make([]slice.Hit, 0, len(hits))
	top1 := 0.0
	for _, h := range hits {
		if h.Slice == nil || h.Slice.Type != slice.Result {
			continue // only Result slices carry reusable outcomes
		}
		cands = append(cands, h)
		if h.Score > top1 {
			top1 = h.Score // max, not cands[0]: never assume Search sorted
		}
	}
	if len(cands) == 0 {
		return nil, nil // no reusable candidate → no reuse
	}

	for _, h := range cands {
		s := h.Slice
		switch z.Classify(h.Score, top1) {
		case zone.Hit:
			// clear hit: reuse after the remaining gates below
		case zone.Grey:
			// Ambiguous: reuse only when the judge confirms (spec §3.5
			// RuleGate.Chain; nil judge → conservative reject). Fingerprint
			// re-verification below still applies to grey-approved slices.
			if !d.judgeGrey(ctx, q, s) {
				continue
			}
		default: // zone.Miss
			continue // clearly not the same task
		}
		// Context/model isolation (Issue #133 gateway): a cached outcome
		// produced under a different conversation history or model must
		// never be served. When the query carries a context/model (gateway
		// always does), entries without a matching stamp — including
		// unstamped legacy slices — fail closed; empty query fields keep
		// the legacy CLI behavior.
		if q.ContextHash != "" && s.Meta.ContextHash != q.ContextHash {
			continue
		}
		if q.Model != "" && s.Meta.Model != q.Model {
			continue
		}
		if !d.verified(ctx, s) {
			continue // deps changed: stale, reject this candidate
		}
		return &L3Result{
			SliceID:  s.ID,
			Response: string(s.Content),
			CostUSD:  0, // usage accounting lands in U17
		}, nil
	}
	return nil, nil
}

// verified runs the two-stage dependency check; false is fail-closed.
func (d *L3Decider) verified(ctx context.Context, s *slice.Slice) bool {
	if len(s.Meta.Deps) == 0 {
		// No captured dependencies: reusable only under explicit opt-in
		// (extract --l3-safe). A shared/injected library must not be able
		// to mark dependency-free results reusable by omission.
		return s.Meta.L3Safe
	}
	// Every Mtimes key must exist in Deps: a half-covered entry is rejected
	// rather than partially verified (a key present in one map but not the
	// other would otherwise skip the Lstat guard or the mtime check).
	for p := range s.Meta.Mtimes {
		if _, ok := s.Meta.Deps[p]; !ok {
			return false
		}
	}
	// Uniform guard over ALL Deps keys (not just Mtimes-covered ones):
	//  1. IsLocal — never touch anything outside the dependency root
	//  2. Lstat — symlinked deps are rejected outright, verification must
	//     not follow links outside the root (MEDIUM fix, sa_20260811_123652)
	//  3. mtime fast-fail when a snapshot exists for this key
	for p := range s.Meta.Deps {
		if !filepath.IsLocal(p) {
			return false
		}
		fi, err := os.Lstat(filepath.Join(d.Root, p))
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if want, ok := s.Meta.Mtimes[p]; ok && fi.ModTime().Unix() != want {
			return false
		}
	}
	// Stage 2: sha256 authority (full content re-read).
	changed, err := fingerprint.Verify(d.Root, s.Meta.Deps)
	if err != nil || len(changed) > 0 {
		return false
	}
	return true
}

func (d *L3Decider) zones() zone.Zones {
	if d.Zones != nil {
		return *d.Zones
	}
	return zone.Default()
}

// judgeGrey runs the spec §3.5 grey-zone confirmation: RuleGate.Chain
// (fingerprint gate → rules → judge). The L3Decider already re-verifies
// dependency fingerprints after this (verified), so a judge "yes" still
// cannot surface stale data. A nil judge, a judge error, or a judge "no"
// all reject conservatively (fail-closed).
func (d *L3Decider) judgeGrey(ctx context.Context, q Query, s *slice.Slice) bool {
	if d.Judge == nil {
		return false
	}
	gate := judge.RuleGate{Judge: d.Judge}
	v, _, err := gate.Chain(ctx, judge.Candidate{
		Query:   q.UserInput,
		SliceID: s.ID,
		Content: string(s.Content),
		Scope:   s.Scope,
		Type:    s.Type,
		Zone:    zone.Grey,
		Deps:    s.Meta.Deps,
		RootDir: d.Root,
	})
	return err == nil && v == judge.Confirm
}

func (d *L3Decider) k() int {
	if d.K > 0 {
		return d.K
	}
	return 3
}
