package agent

import (
	"context"
	"testing"

	"semantix/harness/internal/event"
	"semantix/harness/internal/tool"
)

// TestBeginRunTurnClearsPrefetchedInject guards the fix for the stale
// cross-turn injection defect: prefetchedInject lives on Agent, not
// turnRuntime, so beginRunTurn's `a.turn = turnRuntime{}` reset alone never
// touched it. Before the fix, a turn that warmed a [semantix-reuse] block
// (via startInjectWarm) left it in place for buildSamplingRequest's fallback
// to pick up on a LATER turn whose own synchronous injection missed —
// prepending stale, unrelated memory into that turn's system prompt. A fresh
// turn must always start with no prefetched block.
func TestBeginRunTurnClearsPrefetchedInject(t *testing.T) {
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	stale := "[semantix-reuse] turn-1 stale block"
	a.prefetchedInject.Store(&prefetchedInjectResult{Text: stale, Turn: a.semantixTurn.Load()})

	a.beginRunTurn(context.Background(), "turn 2: unrelated input")

	if pb := a.prefetchedInject.Load(); pb != nil {
		t.Fatalf("beginRunTurn left a stale prefetchedInject block set: %q", pb.Text)
	}
}
