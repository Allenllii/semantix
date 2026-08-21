// Package prefetch implements the M3 MVP speculative prefetcher (N12):
// during LLM wait time, fill the idle window with cheap read-only work that
// the next tool round is likely to need. Only read-only targets are ever
// proposed (prefetching must never cause side effects).
//
// Design (see 总体架构-流程树.md §N12 and Agent-Infra-架构设计.md §P4):
//   - transition matrix: tool-name bigrams observed from real rounds give
//     P(next | last); high-confidence successors become prefetch candidates;
//   - budget: each task carries a cost; Plan stops once the total budget is
//     exhausted (single prefetch ≤ 2k token equivalent by default);
//   - waste penalty: hit/waste feedback per candidate decays its confidence
//     (waste/hit > 3:1 auto-demotes), so the prefetcher learns when betting
//     is worth it — the same signal family the evolution engine consumes.
//
// The Prefetcher interface (Plan) is frozen since U1; two implementations
// ship side by side (see docs/Agile路线图.md H5):
//
//   - Planner (planner.go, Issue 62 MVP): offline — Learn() rebuilds the
//     transition matrix from the slice library's ToolPattern slices; a
//     whitelist makes it fail-closed (empty whitelist = no prefetch).
//   - MatrixPrefetcher (this file, N12 authority): online — Observe()
//     updates bigram counts per executed tool, hit/waste feedback decays
//     candidate confidence (waste/hit > 3:1 auto-demotes), and only tools
//     observed read-only are ever proposed.
//
// They are complementary (offline seed vs online learning); the harness
// picks one and wires it into sched.RuleDecider via AsPlanFunc.
package prefetch

import (
	"errors"
	"math"
	"math/rand"
	"sort"
	"sync"
)

// ApplyEvolution installs the transition-confidence weight used by the next
// Plan call. Learned transitions and the remaining budget/penalty knobs stay
// intact.
func (m *MatrixPrefetcher) ApplyEvolution(minConf float64) error {
	if math.IsNaN(minConf) || math.IsInf(minConf, 0) {
		return errors.New("prefetch: non-finite evolution confidence")
	}
	if minConf <= 0 {
		minConf = math.SmallestNonzeroFloat64
	}
	if minConf > 1 {
		minConf = 1
	}
	m.mu.Lock()
	m.cfg.MinConf = minConf
	m.mu.Unlock()
	return nil
}

// SetEpsilon enables or disables the absorbing-state escape probe at
// runtime (Issue #254). It is separate from ApplyEvolution because Epsilon
// is a closed-loop escape knob, not a per-snapshot confidence threshold.
func (m *MatrixPrefetcher) SetEpsilon(epsilon float64) error {
	if math.IsNaN(epsilon) || math.IsInf(epsilon, 0) {
		return errors.New("prefetch: non-finite epsilon rejected")
	}
	if epsilon < 0 || epsilon > 1 {
		return errors.New("prefetch: epsilon out of range [0,1]")
	}
	m.mu.Lock()
	m.cfg.Epsilon = epsilon
	m.mu.Unlock()
	return nil
}

// Config carries operator knobs; zero values select documented defaults.
type Config struct {
	// TopK caps the number of tasks returned per Plan (default 3).
	TopK int
	// MinConf is the minimum transition probability for a candidate to be
	// proposed (default 0.5, aligned with evolve.PrefetchConf).
	MinConf float64
	// MaxCost caps the total estimated cost of one Plan (default 2000,
	// i.e. ~2k tokens of assembly work per wait window).
	MaxCost int
	// BaseCost is the estimated cost of one slice-assembly task
	// (default 512; assembly re-reads a few slices).
	BaseCost int
	// WasteHitLimit: candidates whose waste/hit EWMA ratio exceeds this are
	// demoted (default 3.0 — N12 "waste/hit > 3:1 自动降权").
	WasteHitLimit float64
	// Decay is the EWMA smoothing for hit/waste rates (default 0.2).
	Decay float64
	// Epsilon is the absorbing-state escape probability (default 0 =
	// disabled). When every candidate falls below MinConf and the plan
	// would be empty, Plan probes the highest-probability excluded candidate
	// with probability Epsilon to keep a trickle of hit/waste signal flowing
	// (Issue #254). Applicable only when there is any learned transition; an
	// empty transition table still yields an empty plan.
	Epsilon float64
	// MaxLoad is the slot-occupancy ratio at which speculative work yields
	// to normal execution (default 0.8).
	MaxLoad float64
}

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{
		TopK:          3,
		MinConf:       0.5,
		MaxCost:       2000,
		BaseCost:      512,
		WasteHitLimit: 3.0,
		Decay:         0.2,
		Epsilon:       0, // disabled by default; enabled by evolve wiring
		MaxLoad:       DefaultMaxLoad,
	}
}

// MatrixPrefetcher learns tool-transition patterns and plans speculative
// read-only prefetch tasks. Safe for concurrent use.
type MatrixPrefetcher struct {
	mu       sync.Mutex
	cfg      Config
	bigrams  map[string]map[string]int // prev -> next -> count
	counts   map[string]int            // prev -> total transitions
	readOnly map[string]bool           // next -> observed read-only (safe to prefetch)
	hit      map[string]float64        // key -> EWMA hit rate
	waste    map[string]float64        // key -> EWMA waste rate
	gate     gateCounter
}

// NewMatrixPrefetcher builds a MatrixPrefetcher with cfg (zero → Defaults).
func NewMatrixPrefetcher(cfg Config) *MatrixPrefetcher {
	def := Defaults()
	if cfg.TopK <= 0 {
		cfg.TopK = def.TopK
	}
	if cfg.MinConf <= 0 || cfg.MinConf > 1 {
		cfg.MinConf = def.MinConf
	}
	if cfg.MaxCost <= 0 {
		cfg.MaxCost = def.MaxCost
	}
	if cfg.BaseCost <= 0 {
		cfg.BaseCost = def.BaseCost
	}
	if cfg.WasteHitLimit <= 0 {
		cfg.WasteHitLimit = def.WasteHitLimit
	}
	if cfg.Decay <= 0 || cfg.Decay > 1 {
		cfg.Decay = def.Decay
	}
	if cfg.Epsilon < 0 || cfg.Epsilon > 1 {
		cfg.Epsilon = def.Epsilon
	}
	if cfg.MaxLoad <= 0 || cfg.MaxLoad > 1 {
		cfg.MaxLoad = def.MaxLoad
	}
	return &MatrixPrefetcher{
		cfg:      cfg,
		bigrams:  make(map[string]map[string]int),
		counts:   make(map[string]int),
		readOnly: make(map[string]bool),
		hit:      make(map[string]float64),
		waste:    make(map[string]float64),
	}
}

// Observe records one tool transition prev -> next. readOnly marks whether
// next is a safe (read-only) prefetch target; only read-only successors are
// ever proposed by Plan. The harness should call this once per executed
// tool, chaining transitions within and across rounds.
func (m *MatrixPrefetcher) Observe(prev, next string, readOnly bool) {
	if prev == "" || next == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.bigrams[prev]; !ok {
		m.bigrams[prev] = make(map[string]int)
	}
	m.bigrams[prev][next]++
	m.counts[prev]++
	if readOnly {
		m.readOnly[next] = true
	}
}

// Plan implements the frozen Prefetcher interface: given the last tool
// names, propose the most likely read-only successors within budget.
// Unknown last tools (no history) yield an empty plan, never an error.
func (m *MatrixPrefetcher) Plan(lastToolNames []string) ([]PrefetchTask, error) {
	if len(lastToolNames) == 0 {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	last := lastToolNames[len(lastToolNames)-1]
	transitions := m.bigrams[last]
	total := m.counts[last]
	if total == 0 || len(transitions) == 0 {
		return nil, nil
	}

	type cand struct {
		key  string
		prob float64
	}
	var cands []cand
	var probe *cand // highest-probability candidate excluded only by MinConf (#254)
	for next, cnt := range transitions {
		if !m.readOnly[next] {
			continue // never prefetch a writer
		}
		prob := float64(cnt) / float64(total)
		if prob < m.cfg.MinConf {
			// absorb into the escape probe when it is the best (but
			// still-threshold-eligible) excluded candidate; skip demoted
			// ones so the probe never channels a known-bad bet
			if !m.demoted(next) && (probe == nil || prob > probe.prob) {
				probe = &cand{key: next, prob: prob}
			}
			continue
		}
		if m.demoted(next) {
			continue // waste/hit penalty
		}
		cands = append(cands, cand{key: next, prob: prob})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].prob > cands[j].prob })

	var tasks []PrefetchTask
	cost := 0
	for _, c := range cands {
		if len(tasks) >= m.cfg.TopK {
			break
		}
		if cost+m.cfg.BaseCost > m.cfg.MaxCost {
			break
		}
		tasks = append(tasks, PrefetchTask{
			Kind: "slice-assembly",
			Key:  c.key,
			Cost: m.cfg.BaseCost,
		})
		cost += m.cfg.BaseCost
	}
	// Absorbing-state escape: if all candidates were excluded by MinConf and
	// nothing ran, probe the best excluded candidate with probability Epsilon
	// to keep a trickle of hit/waste signal flowing back into the evolve loop
	// so PrefetchConf can recover (Issue #254).
	if len(tasks) == 0 && probe != nil && m.cfg.TopK > 0 &&
		m.cfg.Epsilon > 0 && rand.Float64() < m.cfg.Epsilon {
		if m.cfg.BaseCost <= m.cfg.MaxCost {
			tasks = append(tasks, PrefetchTask{
				Kind: "slice-assembly",
				Key:  probe.key,
				Cost: m.cfg.BaseCost,
			})
		}
	}
	return tasks, nil
}

// PlanWithLoad implements LoadAwarePrefetcher without changing Plan's frozen
// behavior. Load is checked before transition lookup so a skip performs no
// speculative ranking work.
func (m *MatrixPrefetcher) PlanWithLoad(lastToolNames []string, hint LoadHint) (PlanDecision, error) {
	m.mu.Lock()
	maxLoad := m.cfg.MaxLoad
	m.mu.Unlock()
	if reason := EvaluateLoad(hint, maxLoad); reason != "" {
		m.gate.observe(reason)
		return PlanDecision{Reason: reason}, nil
	}
	tasks, err := m.Plan(lastToolNames)
	if err != nil {
		return PlanDecision{}, err
	}
	reason := ReasonNoCandidate
	if len(tasks) > 0 {
		reason = ReasonCandidate
	}
	m.gate.observe(reason)
	return PlanDecision{Tasks: tasks, Reason: reason}, nil
}

// GateStats returns load-aware outcome counters. Calls through the frozen Plan
// method are intentionally excluded because they carry no load decision.
func (m *MatrixPrefetcher) GateStats() GateStats { return m.gate.snapshot() }

// ObserveHit marks a planned prefetch as useful (the predicted tool was
// actually called).
func (m *MatrixPrefetcher) ObserveHit(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hit[key] = ewma(m.hit[key], 1, m.cfg.Decay)
	m.waste[key] = ewma(m.waste[key], 0, m.cfg.Decay)
}

// ObserveWaste marks a planned prefetch as wasted (the predicted tool was
// not called, or its result was unused).
func (m *MatrixPrefetcher) ObserveWaste(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hit[key] = ewma(m.hit[key], 0, m.cfg.Decay)
	m.waste[key] = ewma(m.waste[key], 1, m.cfg.Decay)
}

// Stats returns a snapshot of the transition matrix (observability).
type Stats struct {
	Transitions int            // total observed transitions
	Pairs       map[string]int // prev -> total transitions
	TopNext     map[string]int // prev -> next -> count (flattened key "prev→next")
}

// demoted reports whether a candidate's waste/hit ratio exceeds the limit.
// Caller holds m.mu. A candidate with zero waste is never demoted.
func (m *MatrixPrefetcher) demoted(key string) bool {
	w, h := m.waste[key], m.hit[key]
	if w == 0 {
		return false
	}
	if h == 0 {
		return true // all waste, no hits: demote
	}
	return w/h > m.cfg.WasteHitLimit
}

// ewma folds one binary sample into a rate.
func ewma(prev, sample, alpha float64) float64 {
	return alpha*sample + (1-alpha)*prev
}
