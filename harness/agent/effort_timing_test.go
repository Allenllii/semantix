package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"semantix/harness/event"
	"semantix/harness/provider"
	"semantix/harness/tool"
)

// effortHookProvider records every Stream call's request and runs a hook right
// after it, which is the only way to ask the timing question honestly: the
// level has to change *between* the round already frozen and the one still to
// be built, exactly as it would if a user typed the command mid-turn.
type effortHookProvider struct {
	turns    [][]provider.Chunk
	call     int
	requests []provider.Request
	after    func(call int)
}

func (p *effortHookProvider) Name() string { return "hook" }

func (p *effortHookProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	i := min(p.call, len(p.turns)-1)
	p.call++
	ch := make(chan provider.Chunk, len(p.turns[i]))
	for _, c := range p.turns[i] {
		ch <- c
	}
	close(ch)
	if p.after != nil {
		p.after(len(p.requests) - 1)
	}
	return ch, nil
}

// TestSessionEffortReachesTheNextRoundNotTheCurrentOne: a round's request is
// built once and frozen, so the earliest a new level can bite is the next
// round's rebuild. Asserting both halves — round 1 unchanged, round 2 changed —
// is what makes this a contract rather than a hope.
func TestSessionEffortReachesTheNextRoundNotTheCurrentOne(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(NewAskTool())
	prov := &effortHookProvider{turns: [][]provider.Chunk{
		{toolCallChunk("c1", "ask", `{"questions":["q":1]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	prov.after = func(call int) {
		if call == 0 {
			a.SetSessionEffort(effortPtr("high"))
		}
	}

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.requests) < 2 {
		t.Fatalf("requests = %d, want the tool round and the follow-up", len(prov.requests))
	}
	if got := prov.requests[0].EffortOverride; got != "" {
		t.Errorf("round 1 override = %q, want empty — it was frozen before the level was set", got)
	}
	if got := prov.requests[1].EffortOverride; got != "high" {
		t.Errorf("round 2 override = %q, want high", got)
	}
}

// TestSessionEffortOnASingleRoundTurnLandsOnTheNextTurn pins the documented
// limitation rather than leaving it to be discovered: a turn that answers with
// no tool calls has no next round, so the level waits for the next turn.
func TestSessionEffortOnASingleRoundTurnLandsOnTheNextTurn(t *testing.T) {
	prov := &effortHookProvider{turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	prov.after = func(call int) {
		if call == 0 {
			a.SetSessionEffort(effortPtr("high"))
		}
	}

	if err := a.Run(context.Background(), "first"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("first turn requests = %d, want 1 (no tool calls, no second round)", len(prov.requests))
	}
	if got := prov.requests[0].EffortOverride; got != "" {
		t.Errorf("the only round of turn 1 = %q, want empty", got)
	}

	if err := a.Run(context.Background(), "second"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(prov.requests) < 2 {
		t.Fatalf("requests after turn 2 = %d, want at least 2", len(prov.requests))
	}
	if got := prov.requests[1].EffortOverride; got != "high" {
		t.Errorf("turn 2's first round = %q, want high", got)
	}
}

// TestRetriedRoundReplaysTheFrozenEffort: an in-flight round is never
// re-levelled. The retry must replay the exact payload, override included —
// changing depth halfway through a round would make the retry a different
// request wearing the first one's identity.
func TestRetriedRoundReplaysTheFrozenEffort(t *testing.T) {
	interrupted := &provider.StreamInterruptedError{
		Err: errors.New("read stream: unexpected EOF"), Reason: provider.StreamInterruptPrematureEOF,
	}
	prov := &effortHookProvider{turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "partial "}, {Type: provider.ChunkError, Err: interrupted}},
		{{Type: provider.ChunkText, Text: "continued"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, echoRegistry(), NewSession(""), Options{}, event.Discard)
	a.SetSessionEffort(effortPtr("low"))
	prov.after = func(call int) {
		if call == 0 {
			// A level set during the retry window must not reach the replay.
			a.SetSessionEffort(effortPtr("max"))
		}
	}

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run should recover the interrupted stream, got %v", err)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("requests = %d, want the attempt and its replay", len(prov.requests))
	}
	if got := prov.requests[1].EffortOverride; got != "low" {
		t.Errorf("replay override = %q, want the frozen low", got)
	}
	if !reflect.DeepEqual(prov.requests[0], prov.requests[1]) {
		t.Errorf("replay is not the frozen payload:\n first %+v\nreplay %+v",
			prov.requests[0], prov.requests[1])
	}
}
