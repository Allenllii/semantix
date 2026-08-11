package cache

import (
	"context"
	"os"
	"path/filepath"

	"semantix/kernel/fingerprint"
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
//	1. retrieval: Result-typed slice, zone Hit
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
	if len(hits) == 0 {
		return nil, nil // no candidate → no reuse
	}
	top1 := hits[0].Score

	for _, h := range hits {
		s := h.Slice
		if s.Type != slice.Result {
			continue // only Result slices carry reusable outcomes
		}
		if z.Classify(h.Score, top1) != zone.Hit {
			continue // grey/miss: not clearly the same task
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
	// Key-set consistency: Mtimes must not exist without Deps (an
	// inconsistent library entry is rejected rather than half-verified).
	if len(s.Meta.Mtimes) > 0 && len(s.Meta.Mtimes) != len(s.Meta.Deps) {
		return false
	}
	// Stage 1: mtime fast-fail (cheap stat, no content read). Symlinked
	// deps are rejected outright: verification must never follow links
	// outside the dependency root.
	for p, want := range s.Meta.Mtimes {
		if !filepath.IsLocal(p) {
			return false
		}
		fi, err := os.Lstat(filepath.Join(d.Root, p))
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if fi.ModTime().Unix() != want {
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

func (d *L3Decider) k() int {
	if d.K > 0 {
		return d.K
	}
	return 3
}
