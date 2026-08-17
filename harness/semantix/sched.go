// Package semantix — fork-side scheduler (P3 MVP).
//
// The reasonix harness cannot import the semantix kernel module (separate
// binaries), so the rule-based scheduler lives here as a self-contained,
// deterministic implementation with in-memory behavior learning. It mirrors
// kernel/sched.RuleDecider (the authoritative implementation with full tests
// in the semantix repo); keep the two in sync when tuning rules.
//
// Design (总体架构-流程树.md §N04): contiguous read-only tools form parallel
// groups; writers/blacklisted tools run serially (provider order kept); a
// read-only tool whose recent success rate is below the floor is pulled out
// of parallel groups (behavior-learning candidate gate); any writer or a
// large round schedules to the pro tier.
package semantix

import "sync"

// SerialToolNames never join a parallel group (harness partitionToolCalls
// blacklist: complete_step, todo_write, wait, bash_output, use_capability,
// compress).
var SerialToolNames = map[string]struct{}{
	"complete_step":  {},
	"todo_write":     {},
	"wait":           {},
	"bash_output":    {},
	"use_capability": {},
	"compress":       {},
}

// ToolCallInfo is one tool call's scheduling view.
type ToolCallInfo struct {
	CallID   string
	Name     string
	ReadOnly bool
}

// RoundPlan is the scheduler's joint decision for one tool round.
type RoundPlan struct {
	// ParallelGroups are groups of call IDs to run concurrently; every
	// call appears exactly once, in provider order.
	ParallelGroups [][]string
	// Tier is "flash" (default) or "pro".
	Tier string
}

// SchedConfig carries operator knobs; zero values select defaults.
type SchedConfig struct {
	MaxParallel  int     // parallel fan-out cap per group (default 8)
	MinSamples   int     // behavior samples before the gate applies (default 5)
	SuccessFloor float64 // read-only tool below this success rate runs serially (default 0.7)
	ComplexTools int     // calls above this → pro tier (default 3)
	DefaultTier  string  // "flash"
	ProTier      string  // "pro"
}

// Scheduler is the concrete P3 MVP scheduler. Safe for concurrent use.
type Scheduler struct {
	mu    sync.Mutex
	cfg   SchedConfig
	stats map[string]*toolStat
}

type toolStat struct {
	runs     int
	failures int
}

// NewScheduler builds a Scheduler with cfg (zero values → defaults).
func NewScheduler(cfg SchedConfig) *Scheduler {
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = 8
	}
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = 5
	}
	if cfg.SuccessFloor <= 0 || cfg.SuccessFloor > 1 {
		cfg.SuccessFloor = 0.7
	}
	if cfg.ComplexTools <= 0 {
		cfg.ComplexTools = 3
	}
	if cfg.DefaultTier == "" {
		cfg.DefaultTier = "flash"
	}
	if cfg.ProTier == "" {
		cfg.ProTier = "pro"
	}
	return &Scheduler{cfg: cfg, stats: make(map[string]*toolStat)}
}

// DecideRound plans one tool round.
func (s *Scheduler) DecideRound(calls []ToolCallInfo) RoundPlan {
	s.mu.Lock()
	defer s.mu.Unlock()
	return RoundPlan{
		ParallelGroups: s.partition(calls),
		Tier:           decideTier(calls, s.cfg),
	}
}

// Observe feeds one executed tool back into the behavior statistics.
func (s *Scheduler) Observe(name string, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stats[name]
	if st == nil {
		st = &toolStat{}
		s.stats[name] = st
	}
	st.runs++
	if !success {
		st.failures++
	}
}

// partition splits calls into parallel groups and serial singles, preserving
// provider order. Called with s.mu held.
func (s *Scheduler) partition(calls []ToolCallInfo) [][]string {
	var groups [][]string
	for i := 0; i < len(calls); {
		if s.parallelisable(calls[i]) {
			start := i
			i++
			for i < len(calls) && s.parallelisable(calls[i]) {
				i++
			}
			cap := s.cfg.MaxParallel
			for g := start; g < i; g += cap {
				end := g + cap
				if end > i {
					end = i
				}
				groups = append(groups, ids(calls[g:end]))
			}
			continue
		}
		groups = append(groups, []string{calls[i].CallID})
		i++
	}
	return groups
}

// parallelisable reports whether a call may join a parallel group. Called
// with s.mu held.
func (s *Scheduler) parallelisable(c ToolCallInfo) bool {
	if !c.ReadOnly {
		return false
	}
	if _, black := SerialToolNames[c.Name]; black {
		return false
	}
	st := s.stats[c.Name]
	if st == nil || st.runs < s.cfg.MinSamples {
		return true // no evidence: default to the static rule
	}
	return float64(st.runs-st.failures)/float64(st.runs) >= s.cfg.SuccessFloor
}

// decideTier maps a round to a model tier.
func decideTier(calls []ToolCallInfo, cfg SchedConfig) string {
	for _, c := range calls {
		if !c.ReadOnly {
			return cfg.ProTier
		}
	}
	if len(calls) > cfg.ComplexTools {
		return cfg.ProTier
	}
	return cfg.DefaultTier
}

// ids extracts CallIDs preserving order.
func ids(calls []ToolCallInfo) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.CallID
	}
	return out
}
