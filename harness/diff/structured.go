package diff

import (
	"regexp"
	"strconv"
	"strings"
)

// LineKind identifies the presentation role of a unified-diff row. The
// change kind (added/modified/deleted) remains metadata on Change; rows only
// describe what the server has already rendered.
type LineKind string

const (
	LineHunk    LineKind = "hunk"
	LineContext LineKind = "context"
	LineAdded   LineKind = "added"
	LineDeleted LineKind = "deleted"
	LineMeta    LineKind = "meta"
)

// Line is the authoritative, line-numbered form of one unified-diff row.
// Zero line numbers mean that the row does not belong to that side (for
// example, an added row has no old line number).
type Line struct {
	Kind    LineKind
	OldLine int
	NewLine int
	Text    string
}

var unifiedHunkRE = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// ParseUnified turns a unified diff into line-numbered rows. It is deliberately
// tolerant: malformed or non-unified input is kept as meta rows instead of
// being discarded. The parser never changes the original Diff string, which
// remains the exact value used by copy operations.
func ParseUnified(src string) []Line {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(src, "\n"), "\n")
	rows := make([]Line, 0, len(lines))
	oldLine, newLine := 0, 0
	for i, raw := range lines {
		raw = strings.TrimSuffix(raw, "\r")
		// The first two file labels are metadata, not code rows. Keep any
		// later ---/+++ content because it may be a legitimate changed line.
		if i < 2 && (strings.HasPrefix(raw, "--- ") || strings.HasPrefix(raw, "+++ ")) {
			rows = append(rows, Line{Kind: LineMeta, Text: raw})
			continue
		}
		if m := unifiedHunkRE.FindStringSubmatch(raw); m != nil {
			oldLine = atoi(m[1])
			newLine = atoi(m[3])
			rows = append(rows, Line{Kind: LineHunk, Text: raw})
			continue
		}
		if raw == "" {
			rows = append(rows, Line{Kind: LineMeta, Text: raw})
			continue
		}
		switch raw[0] {
		case '+':
			rows = append(rows, Line{Kind: LineAdded, NewLine: newLine, Text: raw[1:]})
			newLine++
		case '-':
			rows = append(rows, Line{Kind: LineDeleted, OldLine: oldLine, Text: raw[1:]})
			oldLine++
		case ' ':
			rows = append(rows, Line{Kind: LineContext, OldLine: oldLine, NewLine: newLine, Text: raw[1:]})
			oldLine++
			newLine++
		case '\\':
			rows = append(rows, Line{Kind: LineMeta, Text: raw})
		default:
			rows = append(rows, Line{Kind: LineMeta, Text: raw})
		}
	}
	return rows
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
