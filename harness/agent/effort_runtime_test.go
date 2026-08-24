package agent

import (
	"context"
	"testing"

	"semantix/harness/event"
	"semantix/harness/provider"
	"semantix/harness/tool"
)

// TestSetSessionEffortDoesNotSwapTheProvider guards the whole point of the
// setter. Changing effort by rebuilding is exactly what the CLI does today, so
// this failure mode is not hypothetical: a later "implementation" that reached
// for a rebuild would still satisfy every behavioural test in this package
// while discarding the provider, its pricing, and any fork-capture wrapper
// installed mid-run.
func TestSetSessionEffortDoesNotSwapTheProvider(t *testing.T) {
	prov := &effortHookProvider{turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	before := a.svc.prov

	a.SetSessionEffort(effortPtr("high"))
	if a.svc.prov != before {
		t.Fatal("SetSessionEffort replaced the provider")
	}
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if a.svc.prov != before {
		t.Fatal("the run after SetSessionEffort replaced the provider")
	}
	if got := prov.requests[0].EffortOverride; got != "high" {
		t.Fatalf("override = %q, want high — the level must reach that same provider", got)
	}
}

// TestSetSessionEffortConcurrentWithARun: the dial is a UI-goroutine action and
// the rounds read it from the run goroutine, so the two genuinely overlap.
// Under -race the race detector is the assertion; without it this still
// exercises the path and checks that the value never lands somewhere incoherent.
func TestSetSessionEffortConcurrentWithARun(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(NewAskTool())
	prov := &effortHookProvider{turns: [][]provider.Chunk{
		{toolCallChunk("c1", "ask", `{"questions":["q":1]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		levels := []*string{effortPtr("low"), effortPtr("high"), effortPtr(""), nil}
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			a.SetSessionEffort(levels[i%len(levels)])
		}
	}()

	err := a.Run(context.Background(), "go")
	close(stop)
	<-done
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Whatever the racing writer left behind must still be one of the values it
	// actually wrote — a torn read would show up here as something else.
	if level, ok := a.SessionEffort(); ok && level != "" && level != "low" && level != "high" {
		t.Fatalf("session effort settled on %q, which was never written", level)
	}
	for i, req := range prov.requests {
		switch req.EffortOverride {
		case "", "low", "high":
		default:
			t.Fatalf("round %d carried %q, which was never written", i, req.EffortOverride)
		}
	}
}
