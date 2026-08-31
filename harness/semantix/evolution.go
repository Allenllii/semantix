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
	var value float64
	var targets []string
	var probeTargets []string
	switch e.Kind {
	case kernelevent.PrefetchHit:
		// Magnitude semantics (research plan W5 "带幅度信号"): a hit that
		// arrived before consumption (LeadMs ≥ 0) is worth a full sample; a
		// late hit was still consumed but paid the cold path — half credit.
		signal = "prefetch_hit"
		value = 1
		var p kernelevent.PrefetchHitPayload
		if json.Unmarshal(e.Data, &p) == nil {
			targets = p.Targets
			probeTargets = p.ProbeTargets
			if p.LeadMs < 0 {
				value = 0.5
			}
		}
	case kernelevent.PrefetchWaste:
		signal = "prefetch_waste"
		value = 1
		var p kernelevent.PrefetchWastePayload
		if json.Unmarshal(e.Data, &p) == nil {
			targets = p.Targets
			probeTargets = p.ProbeTargets
		}
	case kernelevent.SliceReject:
		// Issue #267 step 3: injection pollution is the last unproduced
		// signal source (PR #228 wired prefetch_hit/waste). targets stays
		// nil so the prefetch feedback loop below never sees rejections.
		signal = "inject_pollution"
		value = 1
	case kernelevent.Usage:
		// New signal source (research plan W5 "扩事件类型"): the provider
		// prefix-cache hit ratio of one model call folds into the hit EWMA
		// as a true magnitude in [0,1] — not a constant 1 — so L1 quality
		// participates in the same tuning loop as L2/L3 evidence.
		var p kernelevent.UsagePayload
		if json.Unmarshal(e.Data, &p) != nil || p.CacheHit+p.CacheMiss == 0 {
			return
		}
		l.record("cache_hit", float64(p.CacheHit)/float64(p.CacheHit+p.CacheMiss), e)
		return
	default:
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if feedback, ok := l.prefetcher.(interface {
		ObserveHit(string)
		ObserveWaste(string)
	}); ok {
		feedbackTargets := targets
		if len(probeTargets) > 0 {
			feedbackTargets = probeTargets
		}
		for _, target := range feedbackTargets {
			if signal == "prefetch_hit" {
				feedback.ObserveHit(target)
			} else {
				feedback.ObserveWaste(target)
			}
		}
	}
	// Probe outcomes are exploration cost: retain local per-target learning
	// and event/usage observability, but do not treat them as evidence for
	// tightening or relaxing the global exploitation threshold (Issue #302).
	if len(probeTargets) > 0 {
		return
	}
	l.recordLocked(signal, value, e)
}

// record folds one magnitude signal into the engine and publishes an
// EvolutionTick when the folded evidence changed the parameter snapshot.
func (l *EvolutionLoop) record(signal string, value float64, e kernelevent.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recordLocked(signal, value, e)
}

// recordLocked is record with l.mu already held.
func (l *EvolutionLoop) recordLocked(signal string, value float64, e kernelevent.Event) {
	before := l.engine.Params()
	epoch := l.epoch.Add(1)
	if err := l.engine.RecordSignal(evolve.Signal{Name: signal, Value: value, Epoch: epoch}); err != nil {
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
