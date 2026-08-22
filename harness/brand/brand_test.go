package brand

import (
	"strings"
	"testing"
)

// The product display name feeds window titles, transcript headers, and
// document metadata; keep it a single clean word so those surfaces never
// need to trim or escape it.
func TestNameIsCleanDisplayToken(t *testing.T) {
	if Name != "Semantix" {
		t.Fatalf("Name = %q, want %q", Name, "Semantix")
	}
	if strings.ContainsAny(Name, " \t\r\n") {
		t.Fatalf("Name %q must not contain whitespace", Name)
	}
}

// The full rebrand (docs/specs/semantix-full-rebrand.md) owns every product
// surface, so no brand constant may carry the upstream "Reasonix" name —
// upstream attribution lives only in LICENSE.reasonix.md and ATTRIBUTION.md.
func TestBrandDoesNotLeakUpstreamName(t *testing.T) {
	for _, s := range []string{Vendor, Copyright, Tagline} {
		if strings.TrimSpace(s) == "" {
			t.Fatal("brand constants must be non-empty")
		}
		if strings.Contains(s, "Reasonix") {
			t.Fatalf("brand constant %q still carries the upstream name", s)
		}
	}
}
