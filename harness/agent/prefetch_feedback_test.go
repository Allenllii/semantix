package agent

import (
	"testing"

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
