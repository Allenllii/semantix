package inject

import (
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// zone seeds calibrated against zone.Default(): the full query matches the
// long seed as Hit; the short "fix failing" shares enough tokens to land in
// grey (rel ≈ 0.62 ∈ [0.55, 0.8)); the rice seed shares nothing (Miss).
func seedZoneSlices(t *testing.T) (idx *bm25.Index) {
	t.Helper()
	idx = bm25.New()
	store := newTestStore(t, t.TempDir()+"/db.jsonl")
	seed(t, idx, store,
		"fix failing go test after refactor",        // hit
		"fix failing go",                            // grey candidate
		"how to cook rice in a rice cooker tonight", // miss
	)
	return idx
}

// TestInjectorDropsGreyByDefault pins the fail-closed default: grey slices
// never enter the block without AllowGrey (Krites §3.1).
func TestInjectorDropsGreyByDefault(t *testing.T) {
	idx := seedZoneSlices(t)
	z := zone.Default()
	inj, err := (&Injector{Index: idx, Scope: slice.Project, K: 5, Zones: &z}).Build("fix failing go test")
	if err != nil {
		t.Fatal(err)
	}
	if inj.GreyIncluded != 0 {
		t.Fatalf("default mode must never include grey, got GreyIncluded=%d", inj.GreyIncluded)
	}
	if strings.Contains(inj.Text, "(grey, unverified)") {
		t.Fatalf("default mode must not carry the grey marker:\n%s", inj.Text)
	}
	if !strings.Contains(inj.Text, "fix failing go test after refactor") {
		t.Fatalf("hit slice must still be injected:\n%s", inj.Text)
	}
}

// TestInjectorAuditModeIncludesGreyWithMarker: AllowGrey admits grey slices
// under a distinct unverified header and counts them (W3 audit mode).
func TestInjectorAuditModeIncludesGreyWithMarker(t *testing.T) {
	idx := seedZoneSlices(t)
	z := zone.Default()
	inj, err := (&Injector{Index: idx, Scope: slice.Project, K: 5, Zones: &z, AllowGrey: true}).Build("fix failing go test")
	if err != nil {
		t.Fatal(err)
	}
	if inj.GreyIncluded == 0 {
		t.Fatal("audit mode must admit grey slices, got GreyIncluded=0")
	}
	if !strings.Contains(inj.Text, "--- slice seed-b (grey, unverified) ---") {
		t.Fatalf("audit block must mark the grey slice:\n%s", inj.Text)
	}
	if !strings.HasPrefix(inj.Text, "[semantix-reuse]") {
		t.Fatal("audit block must stay inside the reuse markers")
	}
	// Determinism: same query, same bytes (injection stability contract).
	inj2, err := (&Injector{Index: idx, Scope: slice.Project, K: 5, Zones: &z, AllowGrey: true}).Build("fix failing go test")
	if err != nil {
		t.Fatal(err)
	}
	if inj2.Text != inj.Text {
		t.Fatal("audit-mode injection must be deterministic for identical retrieval")
	}
}
