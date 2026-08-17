package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"semantix/harness/semantix"
)

// reusePanelLines renders the per-turn semantix reuse panel (U33/H4a):
// 📦 hit slices · 💰 cost saved this turn · 🗂 top source sessions. Returns
// nil when the payload is missing, malformed, or reports zero hits — the
// panel is hidden entirely (zero noise, and a broken kernel never renders
// anything).
func reusePanelLines(detail string, width int) []string {
	if detail == "" {
		return nil
	}
	var s semantix.ReuseSummary
	if err := json.Unmarshal([]byte(detail), &s); err != nil || s.Hits <= 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  📦 %d slice", s.Hits)
	if s.Hits > 1 {
		b.WriteString("s")
	}
	b.WriteString(" reused")
	if s.SavingsUSD > 0 {
		fmt.Fprintf(&b, " · 💰 saved $%s", formatPanelUSD(s.SavingsUSD))
	}
	if len(s.Sources) > 0 {
		b.WriteString(" · 🗂 from: ")
		b.WriteString(strings.Join(s.Sources, ", "))
	}
	if s.TaskElapsedSeconds > 0 {
		if s.TaskComplete {
			b.WriteString(" · ✅ ")
			b.WriteString(formatTaskDuration(s.TaskElapsedSeconds))
		} else {
			b.WriteString(" · ⏳ ")
			b.WriteString(formatTaskDuration(s.TaskElapsedSeconds))
		}
	}
	return []string{wrapForViewport(b.String(), width, activeCLITheme.success)}
}

// formatPanelUSD renders a USD amount with up to 4 decimals, dropping a
// trailing zero tail ("0.0042", "0.12", "1.5").
func formatPanelUSD(v float64) string {
	s := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", v), "0"), ".")
	if s == "-0" || s == "" {
		return "0"
	}
	return s
}

// formatTaskDuration renders the U38 task elapsed seconds as a compact
// human form: "<60 → 42s", "≥60 → 1m23s", "≥3600 → 1h01m".
func formatTaskDuration(seconds float64) string {
	n := int64(seconds + 0.5)
	switch {
	case n < 60:
		return fmt.Sprintf("%ds", n)
	case n < 3600:
		return fmt.Sprintf("%dm%ds", n/60, n%60)
	default:
		return fmt.Sprintf("%dh%dm", n/3600, (n%3600)/60)
	}
}
