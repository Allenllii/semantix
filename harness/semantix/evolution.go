package semantix

import (
	"encoding/json"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	kernelevent "semantix/kernel/event"
	"semantix/kernel/evolve"
)

// EvolutionTuner is the narrow, dependency-safe update surface implemented by
// sched.RuleDecider and prefetch.MatrixPrefetcher.
type EvolutionTuner interface {
	ApplyEvolution(float64) error
}

// EscapeEpsilon is the prefetch absorbing-state escape probability the
// harness wires into a MatrixPrefetcher so the evolve loop never starves
// itself of hit/waste signal (Issue #254).
const EscapeEpsilon = 0.1

// EvolutionLoop folds prefetch outcomes into the online engine and publishes
// one immutable parameter snapshot after every real change.
type EvolutionLoop struct {
	mu         sync.Mutex
	bus        kernelevent.Bus
	engine     evolve.Engine
	scheduler  EvolutionTuner
	prefetcher EvolutionTuner
	epoch      atomic.Uint64
	unsub      func()
}

func NewEvolutionLoop(bus kernelevent.Bus, engine evolve.Engine) *EvolutionLoop {
	l := &EvolutionLoop{bus: bus, engine: engine}
	if bus != nil && engine != nil {
		l.unsub = bus.Subscribe(l.observe)
	}
	return l
}

func (l *EvolutionLoop) Attach(scheduler, prefetcher EvolutionTuner) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.scheduler, l.prefetcher = scheduler, prefetcher
	l.mu.Unlock()
}

func (l *EvolutionLoop) Params() evolve.Params {
	if l == nil || l.engine == nil {
		return evolve.Params{}
	}
	return l.engine.Params()
}

func (l *EvolutionLoop) observe(e kernelevent.Event) {
	var signal string
	var targets []string
	switch e.Kind {
	case kernelevent.PrefetchHit:
		signal = "prefetch_hit"
		var p kernelevent.PrefetchHitPayload
		if json.Unmarshal(e.Data, &p) == nil {
			targets = p.Targets
		}
	case kernelevent.PrefetchWaste:
		signal = "prefetch_waste"
		var p kernelevent.PrefetchWastePayload
		if json.Unmarshal(e.Data, &p) == nil {
			targets = p.Targets
		}
	case kernelevent.SliceReject:
		// Issue #267 step 3: injection pollution is the last unproduced
		// signal source (PR #228 wired prefetch_hit/waste). targets stays
		// nil so the prefetch feedback loop below never sees rejections.
		signal = "inject_pollution"
	default:
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if feedback, ok := l.prefetcher.(interface {
		ObserveHit(string)
		ObserveWaste(string)
	}); ok {
		for _, target := range targets {
			if signal == "prefetch_hit" {
				feedback.ObserveHit(target)
			} else {
				feedback.ObserveWaste(target)
			}
		}
	}
	before := l.engine.Params()
	epoch := l.epoch.Add(1)
	if err := l.engine.RecordSignal(evolve.Signal{Name: signal, Value: 1, Epoch: epoch}); err != nil {
		return
	}
	after := l.engine.Params()
	if reflect.DeepEqual(before, after) {
		return
	}
	if l.scheduler != nil {
		// Behavior gate is driven by SuccessFloor; TauL2 now only controls
		// injection confidence (PR #228 decoupling, Issue #254).
		_ = l.scheduler.ApplyEvolution(after.SuccessFloor)
	}
	if l.prefetcher != nil {
		_ = l.prefetcher.ApplyEvolution(after.PrefetchConf)
	}
	if eps, ok := l.prefetcher.(interface{ SetEpsilon(float64) error }); ok {
		_ = eps.SetEpsilon(EscapeEpsilon)
	}
	params, err := json.Marshal(after)
	if err != nil {
		return
	}
	payload, err := json.Marshal(kernelevent.EvolutionTickPayload{ParamsJSON: params})
	if err != nil {
		return
	}
	l.bus.Emit(kernelevent.Event{
		Kind: kernelevent.EvolutionTick, SessionID: e.SessionID,
		Turn: e.Turn, At: time.Now().UTC(), Data: payload,
	})
}

func (l *EvolutionLoop) Close() {
	if l != nil && l.unsub != nil {
		l.unsub()
		l.unsub = nil
	}
}
