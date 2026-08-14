package main

import (
	"fmt"
	"io"
)

// zoneIcon maps a grey-zone verdict to its vibe-coder readable icon (U30):
// 🟢 clear hit, 🟡 grey (verify before reuse), ⚪ miss. Unknown verdicts
// fall back to ⚪ (failure-safe, mirrors zone.Classify).
func zoneIcon(z string) string {
	switch z {
	case "hit":
		return "🟢"
	case "grey":
		return "🟡"
	default:
		return "⚪"
	}
}

// hitRow is the minimal shape the hits-summary line needs: one row per
// displayed result, carrying its zone verdict and source session.
type hitRow struct {
	Zone          string
	SourceSession string
}

// writeHitsSummary prints the cross-session reuse summary line
// (e.g. "🎯 3/10 hits in 2 sessions") after a text hit list. The "hits"
// count is clear-hit (🟢) results; sessions counts distinct non-empty
// source sessions. Nothing is printed for an empty result list.
func writeHitsSummary(w io.Writer, rows []hitRow) {
	if len(rows) == 0 {
		return
	}
	hits := 0
	sessions := map[string]struct{}{}
	for _, r := range rows {
		if r.Zone == "hit" {
			hits++
		}
		if r.SourceSession != "" {
			sessions[r.SourceSession] = struct{}{}
		}
	}
	fmt.Fprintf(w, "🎯 %d/%d hits in %d sessions\n", hits, len(rows), len(sessions))
}

// formatHitLine renders one human-readable hit line (U30): position, zone
// icon, score/zone/id/scope key-values, and the source session annotation
// "from:<session_id>" (omitted when unknown).
func formatHitLine(n int, icon, score, zone, id, scope, session string) string {
	line := fmt.Sprintf("%d. %s score=%s zone=%s id=%s scope=%s", n, icon, score, zone, id, scope)
	if session != "" {
		line += " from:" + session
	}
	return line
}
