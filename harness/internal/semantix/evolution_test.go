package semantix

import (
	"encoding/json"
	"testing"
	"time"

	kernelevent "semantix/kernel/event"
	"semantix/kernel/evolve"
)

type captureTuner struct{ values []float64 }

func (c *captureTuner) ApplyEvolution(v float64) error {
	c.values = append(c.values, v)
	return nil
}

func TestEvolutionLoopAppliesChangedSnapshotAndEmitsTick(t *testing.T) {
	bus := kernelevent.NewSyncBus()
	engine := evolve.New(evolve.Config{MinSamples: 2, FreezeEpochs: 1})
	schedTuner, prefetchTuner := &captureTuner{}, &captureTuner{}
	loop := NewEvolutionLoop(bus, engine)
	loop.Attach(schedTuner, prefetchTuner)
	var ticks []kernelevent.Event
	bus.Subscribe(func(e kernelevent.Event) {
		if e.Kind == kernelevent.EvolutionTick {
			ticks = append(ticks, e)
		}
	})
	payload, _ := json.Marshal(kernelevent.PrefetchWastePayload{Targets: []string{"slice-a"}})
	for turn := 1; turn <= 2; turn++ {
		bus.Emit(kernelevent.Event{Kind: kernelevent.PrefetchWaste, SessionID: "s1", Turn: turn, At: time.Now(), Data: payload})
	}
	if len(ticks) != 1 {
		t.Fatalf("ticks=%d, want 1", len(ticks))
	}
	if len(schedTuner.values) != 1 || len(prefetchTuner.values) != 1 {
		t.Fatalf("applies sched=%v prefetch=%v", schedTuner.values, prefetchTuner.values)
	}
	var tick kernelevent.EvolutionTickPayload
	if err := json.Unmarshal(ticks[0].Data, &tick); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(tick.ParamsJSON) || ticks[0].SessionID != "s1" || ticks[0].Turn != 2 {
		t.Fatalf("bad tick: %#v", ticks[0])
	}
	loop.Close()
}

func TestEvolutionLoopDoesNotTickWithoutParameterChange(t *testing.T) {
	bus := kernelevent.NewSyncBus()
	loop := NewEvolutionLoop(bus, evolve.New(evolve.Config{}))
	defer loop.Close()
	ticks := 0
	bus.Subscribe(func(e kernelevent.Event) {
		if e.Kind == kernelevent.EvolutionTick {
			ticks++
		}
	})
	bus.Emit(kernelevent.Event{Kind: kernelevent.PrefetchHit, SessionID: "s", Turn: 1})
	if ticks != 0 {
		t.Fatalf("ticks=%d, want 0 before warmup", ticks)
	}
}
