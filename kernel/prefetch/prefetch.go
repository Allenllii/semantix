package prefetch

import (
	"sync"
	"time"
)

// PrefetchTask is one speculative read-only prefetch unit.
type PrefetchTask struct {
	Kind string // "slice-assembly" | "embedding" | "file" (low priority)
	Key  string // identifying key (e.g. slice id, file path)
	Cost int    // estimated token/IO cost for budgeting
}

// Prefetcher plans speculative prefetches during LLM wait time (MVP in M3).
type Prefetcher interface {
	// Plan returns prefetch tasks given the last tool names (transition
	// matrix + path patterns). Must only propose read-only work.
	Plan(lastToolNames []string) ([]PrefetchTask, error)
}

// LoadHint is the caller's best-effort view of the resources available to
// speculative work. Zero fields mean unknown and never block a cold start.
type LoadHint struct {
	ConcurrencyUsed  int
	ConcurrencyLimit int
	WaitWindow       time.Duration
	TaskEstimate     time.Duration
}

// PlanReason is the stable explanation for a load-aware planning result.
type PlanReason string

const (
	ReasonCandidate      PlanReason = "candidate"
	ReasonNoCandidate    PlanReason = "no_candidate"
	ReasonLoadSaturated  PlanReason = "load_saturated"
	ReasonWindowTooShort PlanReason = "window_too_short"
	ReasonPlanError      PlanReason = "plan_error"
)

// PlanDecision pairs planned work with the reason the gate produced.
type PlanDecision struct {
	Tasks  []PrefetchTask
	Reason PlanReason
}

// LoadAwarePrefetcher is an optional extension. Prefetcher.Plan stays frozen;
// adapters use this interface when present and otherwise call Plan.
type LoadAwarePrefetcher interface {
	PlanWithLoad(lastToolNames []string, hint LoadHint) (PlanDecision, error)
}

// DefaultMaxLoad is the slot-occupancy threshold above which speculative work
// yields to normal execution.
const DefaultMaxLoad = 0.8

// EvaluateLoad returns a skip reason, or the empty string when prefetch may
// proceed. Unknown inputs fail open to preserve cold-start behavior.
func EvaluateLoad(hint LoadHint, maxLoad float64) PlanReason {
	if maxLoad <= 0 || maxLoad > 1 {
		maxLoad = DefaultMaxLoad
	}
	if hint.ConcurrencyUsed >= 0 && hint.ConcurrencyLimit > 0 &&
		float64(hint.ConcurrencyUsed)/float64(hint.ConcurrencyLimit) >= maxLoad {
		return ReasonLoadSaturated
	}
	if hint.WaitWindow > 0 && hint.TaskEstimate > 0 && hint.WaitWindow < hint.TaskEstimate {
		return ReasonWindowTooShort
	}
	return ""
}

// GateStats is a snapshot of load-aware planning outcomes.
type GateStats struct {
	Candidates    uint64
	NoCandidate   uint64
	LoadSkipped   uint64
	WindowSkipped uint64
}

type gateCounter struct {
	mu    sync.Mutex
	stats GateStats
}

func (c *gateCounter) observe(reason PlanReason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch reason {
	case ReasonCandidate:
		c.stats.Candidates++
	case ReasonNoCandidate:
		c.stats.NoCandidate++
	case ReasonLoadSaturated:
		c.stats.LoadSkipped++
	case ReasonWindowTooShort:
		c.stats.WindowSkipped++
	}
}

func (c *gateCounter) snapshot() GateStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}
