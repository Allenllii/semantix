package agent

import (
	"encoding/json"
	"testing"
	"time"

	"semantix/harness/semantix"
	kernelevent "semantix/kernel/event"
)

func TestPrefetchResultSettlesExactlyOnceAsHit(t *testing.T) {
	b := semantix.NewBridge(semantix.Config{Enabled: true})
	defer b.Close()
	var hits, wastes int
	b.Events().Subscribe(func(e kernelevent.Event) {
		switch e.Kind {
		case kernelevent.PrefetchHit:
			hits++
		case kernelevent.PrefetchWaste:
			wastes++
		}
	})
	a := &Agent{semantix: b}
	a.storePrefetch(&prefetchedInjectResult{Text: "block", Targets: []string{"s1"}, Turn: 1})
	if got := a.takePrefetch(1); got == nil || got.Text != "block" {
		t.Fatalf("take=%#v", got)
	}
	if got := a.takePrefetch(1); got != nil {
		t.Fatalf("second take=%#v", got)
	}
	if hits != 1 || wastes != 0 {
		t.Fatalf("hits=%d wastes=%d", hits, wastes)
	}
}

func TestPrefetchReplacementAndExpirySettleAsWaste(t *testing.T) {
	b := semantix.NewBridge(semantix.Config{Enabled: true})
	defer b.Close()
	var hits, wastes int
	b.Events().Subscribe(func(e kernelevent.Event) {
		switch e.Kind {
		case kernelevent.PrefetchHit:
			hits++
		case kernelevent.PrefetchWaste:
			wastes++
		}
	})
	a := &Agent{semantix: b}
	a.storePrefetch(&prefetchedInjectResult{Text: "old", Targets: []string{"s1"}, Turn: 1})
	a.storePrefetch(&prefetchedInjectResult{Text: "new", Targets: []string{"s2"}, Turn: 1})
	if got := a.takePrefetch(2); got != nil {
		t.Fatalf("expired take=%#v", got)
	}
	if hits != 0 || wastes != 2 {
		t.Fatalf("hits=%d wastes=%d", hits, wastes)
	}
}

func TestPrefetchGateControlsWarmWithoutCreatingFeedback(t *testing.T) {
	b := semantix.NewBridge(semantix.Config{Enabled: true})
	defer b.Close()
	var hits, wastes int
	b.Events().Subscribe(func(e kernelevent.Event) {
		switch e.Kind {
		case kernelevent.PrefetchHit:
			hits++
		case kernelevent.PrefetchWaste:
			wastes++
		}
	})
	a := &Agent{semantix: b}
	a.semantixTurn.Store(3)

	if !a.prefetchAllowed() {
		t.Fatal("cold start must preserve legacy allow behavior")
	}
	a.armPrefetch("load_saturated")
	if a.prefetchAllowed() {
		t.Fatal("load skip must block the warm")
	}
	if hits != 0 || wastes != 0 {
		t.Fatalf("a skipped warm is not feedback: hits=%d wastes=%d", hits, wastes)
	}
	a.armPrefetch("candidate")
	if !a.prefetchAllowed() {
		t.Fatal("candidate plan must allow the warm")
	}
	a.armPrefetch("no_candidate")
	if !a.prefetchAllowed() {
		t.Fatal("no-candidate plan must preserve legacy fail-open warm")
	}
	a.semantixTurn.Add(1)
	if !a.prefetchAllowed() {
		t.Fatal("a stale prior-turn gate must not block a new turn")
	}
}

// TestRecordPrefetchLeadTime pins the Markov timeliness pipeline (Issue
// #272, c5): warm-up completion is stamped on store, the consumption
// decision carries lead_ms = consume − warm-up-completion on the hit event
// (positive = finished in time).
func TestRecordPrefetchLeadTime(t *testing.T) {
	b := semantix.NewBridge(semantix.Config{Enabled: true})
	defer b.Close()
	var leadMs int64 = -1
	b.Events().Subscribe(func(e kernelevent.Event) {
		if e.Kind != kernelevent.PrefetchHit {
			return
		}
		var p kernelevent.PrefetchHitPayload
		if json.Unmarshal(e.Data, &p) == nil {
			leadMs = p.LeadMs
		}
	})
	a := &Agent{semantix: b}
	warmAt := time.Now().Add(-time.Second) // warmed one second before consumption
	a.storePrefetch(&prefetchedInjectResult{Text: "block", Targets: []string{"s1"}, Turn: 1, WarmAt: warmAt})
	if got := a.takePrefetch(1); got == nil {
		t.Fatal("take must consume the warmed block")
	}
	if leadMs < 900 || leadMs > 3000 {
		t.Fatalf("lead_ms=%d, want ≈1000 (positive, consumed after warm-up)", leadMs)
	}
}
