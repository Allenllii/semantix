// Package inject implements the L2 semantic injection (U8, compose side):
// given a user turn and the slice store/index, it retrieves the most
// relevant previously-done slices and assembles a deterministic injection
// block for the harness compose step.
//
// Design invariants (see Agent-Infra-架构设计.md §4.2):
//   - canonical order: injection slices are sorted by ID so identical
//     retrievals produce byte-identical blocks (DeepSeek prefix-cache
//     friendly); never sorted by score.
//   - budget: only whole slices are dropped, in score order, until the block
//     fits the budget; the top slice is always kept when k >= 1.
//   - low-authority: the block is wrapped in markers so the harness can place
//     it after the system prefix / before the user message, and strip it on
//     user edit/rollback (SliceReject).
package inject

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// escapeMarker neutralizes block markers inside slice content so a stored
// slice cannot break out of (or fake) the [semantix-reuse] block — the
// injection stays a low-authority, structurally-closed region. Matching is
// case-insensitive so `[/SEMANTIX-REUSE]` variants cannot bypass either
// (HIGH fix, security review 2026-08-09; MEDIUM hardening 2026-08-10).
func escapeMarker(s string) string {
	s = replaceFold(s, "[/semantix-reuse]", "[\\/semantix-reuse]")
	return replaceFold(s, "[semantix-reuse]", "[\\semantix-reuse]")
}

// replaceFold replaces all case-insensitive occurrences of old with new.
// Rune-level folding: unicode.ToLower maps one rune to one rune, so byte
// offsets never drift (a byte-level ToLower of the whole string would
// misalign for fold-special runes like İ/ẞ and could corrupt output).
func replaceFold(s, old, new string) string {
	rs := []rune(s)
	ro := []rune(strings.ToLower(old))
	var b strings.Builder
	i := 0
	for i <= len(rs)-len(ro) {
		match := true
		for j := range ro {
			if unicode.ToLower(rs[i+j]) != ro[j] {
				match = false
				break
			}
		}
		if match {
			b.WriteString(new)
			i += len(ro)
		} else {
			b.WriteRune(rs[i])
			i++
		}
	}
	for ; i < len(rs); i++ {
		b.WriteRune(rs[i])
	}
	return b.String()
}

// DefaultBudget is the default maximum injection block size in bytes.
const DefaultBudget = 4096

// Injector retrieves and assembles reuse slices for one user turn.
type Injector struct {
	Index  slice.Index
	Store  slice.Store // optional; used to re-read full slices when the index
	// returns them anyway — kept for symmetry with future lazy indexes.
	Scope slice.Scope
	K     int // top-k slices to consider (default 5)
	// Budget caps the assembled block size in bytes (default DefaultBudget).
	Budget int
	// MinScore drops slices below this BM25 score (0 disables).
	MinScore float64
	// Zones, when non-nil, applies the grey-zone classifier: only clearly
	// reusable slices (zone.Hit) enter the block; grey/miss candidates are
	// skipped (Krites §3.1 — the grey zone must be verified, not injected).
	Zones *zone.Zones
}

// Injection is the assembled, deterministic reuse block.
type Injection struct {
	Slices  []*slice.Slice // canonical (ID-sorted) order
	Text    string         // marker-wrapped block to place in the compose step
	Bytes   int
	Dropped int // slices dropped by zone filter or budget (whole-slice truncation)
}

const (
	blockOpen  = "[semantix-reuse]\n"
	blockClose = "[/semantix-reuse]"
)

// Build retrieves top-k slices for query and assembles the injection block.
// It never errors on empty results — an empty Injection is valid.
func (in *Injector) Build(query string) (*Injection, error) {
	k := in.K
	if k <= 0 {
		k = 5
	}
	budget := in.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}

	hits, err := in.Index.Search(query, k, in.Scope)
	if err != nil {
		return nil, fmt.Errorf("inject: search: %w", err)
	}
	top1 := 0.0
	if len(hits) > 0 {
		top1 = hits[0].Score
	}

	var kept []*slice.Slice
	var dropped int
	var buf bytes.Buffer
	buf.WriteString(blockOpen)
	for _, h := range hits {
		if in.MinScore > 0 && h.Score < in.MinScore {
			continue
		}
		if in.Zones != nil {
			z := *in.Zones
			if h.Slice != nil {
				z = z.ForType(h.Slice.Type.String()) // Issue #259 阶段 2
			}
			if z.Classify(h.Score, top1) != zone.Hit {
				dropped++
				continue
			}
		}
		if buf.Len()+len(h.Slice.Content)+len(blockClose)+64 > budget && len(kept) > 0 {
			dropped++
			continue
		}
		fmt.Fprintf(&buf, "--- slice %s (score=%.2f) ---\n%s\n", h.Slice.ID, h.Score, h.Slice.Content)
		kept = append(kept, h.Slice)
	}

	// Canonical order: ID-sorted for byte-stable output (never score order).
	sort.Slice(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })

	// Rebuild the text in canonical order (the first pass wrote score order).
	buf.Reset()
	buf.WriteString(blockOpen)
	for _, sl := range kept {
		fmt.Fprintf(&buf, "--- slice %s ---\n%s\n", sl.ID, escapeMarker(string(sl.Content)))
	}
	buf.WriteString(blockClose)

	return &Injection{
		Slices:  kept,
		Text:    buf.String(),
		Bytes:   buf.Len(),
		Dropped: dropped,
	}, nil
}
