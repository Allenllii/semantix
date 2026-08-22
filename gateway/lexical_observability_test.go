package gateway

import (
	"testing"

	"semantix/kernel/cache"
)

func TestObserveLexicalGateCountsBlocks(t *testing.T) {
	g := &Gateway{}
	g.observeLexicalGate(t.Context(), cache.LexicalGateObservation{SliceID: "a", Score: 0.9, Lexical: 0, Blocked: true, Zone: "grey"})
	g.observeLexicalGate(t.Context(), cache.LexicalGateObservation{SliceID: "b", Score: 0.9, Lexical: 1, Blocked: false, Zone: "hit"})
	g.observeLexicalGate(t.Context(), cache.LexicalGateObservation{SliceID: "c", Score: 0.8, Lexical: 0, Blocked: true, Zone: "grey"})
	if got := g.lexicalBlocks.Load(); got != 2 {
		t.Fatalf("lexicalBlocks = %d, want 2 (only blocked candidates count)", got)
	}
}
