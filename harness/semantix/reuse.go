package semantix

import (
	"strings"
)

// ReuseSummary is the per-turn reuse panel payload (U33/H4a): what this turn
// found in the kernel slice store and what it cost to reuse. A zero value
// (kernel unavailable, no hits) hides the panel — zero noise.
type ReuseSummary struct {
	// Hits is the number of matched slices the kernel lookup returned.
	Hits int `json:"hits"`
	// SavingsUSD is this turn's share of the kernel-reported cumulative
	// cost savings (delta between the last two usage snapshots). Zero when
	// no usage log exists yet (no gateway in the loop / kernel down).
	SavingsUSD float64 `json:"savings_usd,omitempty"`
	// Sources lists the top source sessions the hits came from, most
	// frequent first, at most 3. Empty when the kernel does not expose
	// source_session (soft degrade — the panel drops the 🗂 segment).
	Sources []string `json:"sources,omitempty"`
}

// Line renders the compact one-line notice text for non-panel sinks
// (run_output, desktop notice list). The TUI renders a richer panel from the
// structured JSON instead.
func (s ReuseSummary) Line() string {
	if s.Hits <= 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("📦 ")
	b.WriteString(itoa(s.Hits))
	b.WriteString(" slice")
	if s.Hits > 1 {
		b.WriteString("s")
	}
	if s.SavingsUSD > 0 {
		b.WriteString(" · 💰 $")
		b.WriteString(formatUSD(s.SavingsUSD))
		b.WriteString(" saved")
	}
	if len(s.Sources) > 0 {
		b.WriteString(" · 🗂 from: ")
		b.WriteString(strings.Join(s.Sources, ", "))
	}
	return b.String()
}

// topSources returns the top-3 source sessions by hit frequency, most
// frequent first. Empty session ids are skipped (U30 fields may be absent on
// legacy slices — the panel drops the 🗂 segment then).
func topSources(sessions []string) []string {
	order := make([]string, 0, 3)
	counts := make(map[string]int, 3)
	for _, s := range sessions {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if counts[s] == 0 {
			order = append(order, s)
		}
		counts[s]++
	}
	// insertion sort: n ≤ 50 hits, at most 3 kept — overkill-free.
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && counts[order[j]] > counts[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	if len(order) > 3 {
		order = order[:3]
	}
	return order
}

// formatUSD renders a USD amount without a fixed trailing zero tail
// (0.0042 → "0.0042", 0.12 → "0.12").
func formatUSD(v float64) string {
	s := strings.TrimRight(strings.TrimRight(usdFixed(v), "0"), ".")
	if s == "-0" || s == "" {
		return "0"
	}
	return s
}

// usdFixed renders v with up to 4 decimals using integer math (dtoa-free).
func usdFixed(v float64) string {
	neg := false
	if v < 0 {
		neg = true
		v = -v
	}
	n := int64(v*10000 + 0.5)
	intPart := n / 10000
	fracPart := n % 10000
	out := itoa64(intPart)
	if fracPart > 0 {
		frac := itoa64(fracPart)
		for len(frac) < 4 {
			frac = "0" + frac
		}
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
