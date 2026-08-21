package gateway

import (
	"context"
	"log"

	"semantix/kernel/cache"
)

// Lexical-gate observability (Issue #260 step 4). The lexical support
// gate downgrades zone-Hit candidates with no term overlap to Grey; the
// only way to tell a real hit-rate drop from a gate over-blocking is to
// count the blocks. One structured log line per evaluated candidate plus
// a process-wide counter for the blocked ones.

// observeLexicalGate is the cache.L3Decider.OnLexicalGate hook: it logs
// one line per evaluated zone-Hit and increments the block counter. It
// never affects the reuse verdict — the decision is already made.
func (g *Gateway) observeLexicalGate(_ context.Context, obs cache.LexicalGateObservation) {
	log.Printf("gateway: l3 lexical slice=%s score=%.3f lexical=%.3f blocked=%t zone=%s",
		obs.SliceID, obs.Score, obs.Lexical, obs.Blocked, obs.Zone)
	if obs.Blocked {
		g.lexicalBlocks.Add(1)
	}
}
