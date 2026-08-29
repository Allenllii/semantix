package inject

import (
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// TestInjectorAuditModeRespectsBudget: a grey slice larger than the block
// budget must not be admitted just because AllowGrey is on — audit mode is
// not a budget bypass.
func TestInjectorAuditModeRespectsBudget(t *testing.T) {
	idx := bm25.New()
	store := newTestStore(t, t.TempDir()+"/db.jsonl")
	seed(t, idx, store,
		"fix failing go test after refactor", // hit, small
		"fix failing go",                     // grey, small
		"how to cook rice in a rice cooker tonight", // miss
	)
	// Grow the grey slice far past the budget.
	big := &slice.Slice{
		ID:      "seed-b",
		Type:    slice.Prompt,
		Scope:   slice.Project,
		Content: []byte("fix failing go " + strings.Repeat("padding ", 400)),
	}
	if err := store.Put(big); err != nil {
		t.Fatal(err)
	}
	if err := idx.Remove("seed-b"); err != nil {
		t.Fatal(err)
	}
	if err := idx.Insert(big); err != nil {
		t.Fatal(err)
	}

	z := zone.Default()
	inj, err := (&Injector{Index: idx, Scope: slice.Project, K: 5, Budget: 512, Zones: &z, AllowGrey: true}).Build("fix failing go test")
	if err != nil {
		t.Fatal(err)
	}
	if inj.GreyIncluded != 0 {
		t.Fatalf("oversized grey slice must not be admitted, GreyIncluded=%d", inj.GreyIncluded)
	}
	if inj.Bytes > 512+64 {
		t.Fatalf("block grew past budget: %d bytes", inj.Bytes)
	}
	// The hit slice is still there.
	if !strings.Contains(inj.Text, "fix failing go test after refactor") {
		t.Fatalf("hit slice lost:\n%s", inj.Text)
	}
}
