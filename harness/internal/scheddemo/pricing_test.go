package scheddemo

import "testing"

// The flash tier must be strictly cheaper than pro for an identical round, so a
// scheduler that routes cheap rounds to flash produces a real, positive saving.
func TestRoundCostUSDFlashCheaperThanPro(t *testing.T) {
	const in, out = 1000, 4000
	flash := RoundCostUSD("flash", in, out)
	pro := RoundCostUSD("pro", in, out)
	if !(flash > 0 && pro > 0) {
		t.Fatalf("costs must be positive: flash=%g pro=%g", flash, pro)
	}
	if flash >= pro {
		t.Fatalf("flash (%g) must be cheaper than pro (%g)", flash, pro)
	}
}

// An empty/unknown tier is the OFF baseline (no kernel tiering decision); it
// must bill at the capable (pro) tier, the realistic static provisioning, so
// the OFF arm never looks artificially cheap.
func TestRoundCostUSDUnknownTierBillsAtPro(t *testing.T) {
	const in, out = 1000, 4000
	if got, want := RoundCostUSD("", in, out), RoundCostUSD("pro", in, out); got != want {
		t.Fatalf("empty tier billed %g, want pro rate %g", got, want)
	}
}
