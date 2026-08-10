package evolve

// Signal is one feedback sample consumed by the evolution engine.
type Signal struct {
	Name  string // "cache_hit" | "inject_pollution" | "prefetch_waste" | "latency" | "cost" | "success"
	Value float64
	Epoch uint64
}

// Params is the tunable parameter snapshot (frozen semantics: injected set
// must not change within the freeze window — see architecture spec §6.2).
type Params struct {
	TauL2        float64
	TauL3        float64
	InjectCap    float64
	PrefetchConf float64
	FreezeEpochs uint64 // injection-set freeze duration
}

// Engine runs online EWMA tuning and offline retraining (MVP in M3; stub now).
type Engine interface {
	RecordSignal(s Signal) error
	Params() Params
	Apply(p Params) error
}
