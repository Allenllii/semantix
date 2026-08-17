package cli

import (
	"strings"
	"testing"
)

func TestReusePanelLines(t *testing.T) {
	detail := `{"hits":3,"savings_usd":0.0042,"sources":["boot-1","boot-2"]}`
	lines := reusePanelLines(detail, 100)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	for _, want := range []string{"📦 3 slices reused", "💰 saved $0.0042", "🗂 from: boot-1, boot-2"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("panel %q missing %q", lines[0], want)
		}
	}
}

func TestReusePanelHidesOnNoHits(t *testing.T) {
	cases := []string{
		"",                             // no payload
		`{"hits":0,"savings_usd":0.5}`, // no hits, even with savings
		"not-json",                     // malformed
		`{"hits":3}` + "\n" + `{"broken":`, // truncated
	}
	for _, c := range cases {
		if lines := reusePanelLines(c, 100); len(lines) != 0 {
			t.Errorf("detail %q → %d lines, want 0 (hidden)", c, len(lines))
		}
	}
}

func TestReusePanelOmitsZeroSegments(t *testing.T) {
	// No savings, no sources → only the hit count stays.
	lines := reusePanelLines(`{"hits":2}`, 100)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if !strings.Contains(lines[0], "📦 2 slices reused") {
		t.Errorf("panel %q missing hit count", lines[0])
	}
	if strings.Contains(lines[0], "💰") || strings.Contains(lines[0], "🗂") {
		t.Errorf("panel %q must omit zero savings/sources", lines[0])
	}
}

func TestReusePanelSingular(t *testing.T) {
	lines := reusePanelLines(`{"hits":1,"sources":["boot-1"]}`, 100)
	if len(lines) != 1 || !strings.Contains(lines[0], "1 slice reused") {
		t.Errorf("singular panel: %v", lines)
	}
}

func TestFormatPanelUSD(t *testing.T) {
	cases := []struct{ in float64; want string }{
		{0.0042, "0.0042"},
		{0.12, "0.12"},
		{1.5, "1.5"},
		{0, "0"},
		{0.00004, "0"}, // 4-decimal display cap rounds tiny deltas to 0
	}
	for _, c := range cases {
		if got := formatPanelUSD(c.in); got != c.want {
			t.Errorf("formatPanelUSD(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
