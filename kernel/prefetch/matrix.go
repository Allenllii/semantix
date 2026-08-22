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
//   - waste penalty: hit/waste feedback folds into a per-candidate
//     Beta-Binomial posterior with time decay; a candidate whose posterior
//     mean drops below 1/(1+WasteHitLimit) auto-demotes, so the prefetcher
//     learns when betting is worth it — the same signal family the
//     evolution engine consumes (Issue #272).
//
// The Prefetcher interface (Plan) is frozen since U1; two implementations
// ship side by side (see docs/Agile路线图.md H5):
//
//   - Planner (planner.go, Issue 62 MVP): offline — Learn() rebuilds the
//     transition matrix from the slice library's ToolPattern slices; a
//     whitelist makes it fail-closed (empty whitelist = no prefetch).
//   - MatrixPrefetcher (this file, N12 authority): online — Observe()
//     updates bigram counts per executed tool, hit/waste feedback updates a
//     per-candidate Beta posterior (waste/hit weight ratio > WasteHitLimit
//     auto-demotes), and only tools observed read-only are ever proposed.
//     Stats() exposes the Markov evaluation triple — coverage / accuracy /
//     timeliness (timeliness via harness-emitted LeadMs) — plus the
//     posterior parameters (Issue #272).
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
	// WasteHitLimit: candidates whose discounted waste/hit weight ratio
	// exceeds this are demoted (default 3.0 — N12 "waste/hit > 3:1 自动降权";
	// equivalent to posterior mean μ < 1/(1+WasteHitLimit), Issue #272).
	WasteHitLimit float64
	// Decay is the per-feedback time discount applied to the Beta posterior
	// before each new sample (default 0.2; same forgetting as the former
	// hit/waste EWMA, Issue #272).
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
	// rnd is the escape-probe source: per-instance, seeded with a fixed
	// constant (not the auto-seeded global source) so Plan stays
	// bit-reproducible — the probe is an ε-trickle, not a security
	// primitive, so a fixed sequence is fine (Issue #272 c6).
	rnd *rand.Rand
	// Beta-Binomial posterior per candidate (Issue #272): hitW/wasteW are
	// time-discounted feedback weights (0 = no feedback); the posterior
	// parameters exposed by Stats are Alpha = hitW + betaPrior and
	// Beta = wasteW + betaPrior.
	hitW        map[string]float64
	wasteW      map[string]float64
	hitCount    map[string]int // per-key integer hit feedback count
	wasteCount  map[string]int // per-key integer waste feedback count
	totalHits   int
	totalWastes int
	// Markov coverage bookkeeping (Issue #272): the most recent Plan's
	// proposed key set, matched against the next Observe to decide whether
	// that read-only call was covered by prefetching.
	lastPrev      string
	lastProposed  map[string]bool
	readOnlyCalls int
	coveredCalls  int
	gate          gateCounter // load-aware outcome counters (Issue #273)

}

// betaPrior is the uniform prior strength α₀=β₀ for the per-candidate Beta
// posterior (Issue #272). It is a constant, not a knob: it only smooths
// small-sample estimates and its exact value is not part of any contract.
const betaPrior = 1.0

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
		cfg:        cfg,
		bigrams:    make(map[string]map[string]int),
		counts:     make(map[string]int),
		readOnly:   make(map[string]bool),
		rnd:        rand.New(rand.NewSource(1)),
		hitW:       make(map[string]float64),
		wasteW:     make(map[string]float64),
		hitCount:   make(map[string]int),
		wasteCount: make(map[string]int),
	}
}

// Observe records one tool transition prev -> next. readOnly marks whether
// next is a safe (read-only) prefetch target; only read-only successors are
// ever proposed by Plan. The harness should call this once per executed
// tool, chaining transitions within and across rounds. When the executed
// tool is read-only and was proposed by the most recent Plan, the call is
// counted as covered by prefetching (Markov coverage, Issue #272).
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
		m.readOnlyCalls++
		if prev == m.lastPrev && m.lastProposed[next] {
			m.coveredCalls++
		}
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
		m.recordProposal(last, nil)
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
			// ones so the probe never channels a known-bad bet. The key
			// tie-break keeps probe selection deterministic despite Go's
			// randomized map iteration (Issue #272 c6).
			if !m.demoted(next) && (probe == nil || prob > probe.prob || (prob == probe.prob && next < probe.key)) {
				probe = &cand{key: next, prob: prob}
			}
			continue
		}
		if m.demoted(next) {
			continue // waste/hit penalty
		}
		cands = append(cands, cand{key: next, prob: prob})
	}
	// Probability descending, key ascending on ties: sort.Slice is not
	// stable and transitions map iteration is randomized, so equal-probability
	// candidates need a deterministic order (Issue #272 c6).
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].prob != cands[j].prob {
			return cands[i].prob > cands[j].prob
		}
		return cands[i].key < cands[j].key
	})

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
		m.cfg.Epsilon > 0 && m.rnd.Float64() < m.cfg.Epsilon {
		if m.cfg.BaseCost <= m.cfg.MaxCost {
			tasks = append(tasks, PrefetchTask{
				Kind:     "slice-assembly",
				Key:      probe.key,
				Cost:     m.cfg.BaseCost,
				Locality: LocalityLocal, // in-process slice-library read (Issue #273)
			})
		}
	}
	m.recordProposal(last, tasks)
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

// recordProposal snapshots the proposed key set of the most recent Plan for
// the Markov coverage accounting in Observe (Issue #272). An empty or
// errored plan clears the previous proposal set so a stale one can never
// count as coverage. Caller holds m.mu.
func (m *MatrixPrefetcher) recordProposal(last string, tasks []PrefetchTask) {
	m.lastPrev = last
	m.lastProposed = make(map[string]bool, len(tasks))
	for _, t := range tasks {
		m.lastProposed[t.Key] = true
	}
}

// ObserveHit marks a planned prefetch as useful (the predicted tool was
// actually called). The hit folds into the candidate's Beta posterior:
// α ← (1−Decay)·α + 1, β ← (1−Decay)·β (Issue #272).
func (m *MatrixPrefetcher) ObserveHit(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hitW[key] = (1-m.cfg.Decay)*m.hitW[key] + 1
	m.wasteW[key] = (1 - m.cfg.Decay) * m.wasteW[key]
	m.hitCount[key]++
	m.totalHits++
}

// ObserveWaste marks a planned prefetch as wasted (the predicted tool was
// not called, or its result was unused). The waste folds into the
// candidate's Beta posterior:
// β ← (1−Decay)·β + 1, α ← (1−Decay)·α (Issue #272).
func (m *MatrixPrefetcher) ObserveWaste(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wasteW[key] = (1-m.cfg.Decay)*m.wasteW[key] + 1
	m.hitW[key] = (1 - m.cfg.Decay) * m.hitW[key]
	m.wasteCount[key]++
	m.totalWastes++
}

// Stats returns a snapshot of the transition matrix plus the Markov
// evaluation metrics (observability, Issue #272): global coverage /
// accuracy with their denominators, per-key Beta posterior parameters
// (Alpha/Beta include the uniform prior), posterior means, 90% Wilson
// lower bounds, and integer feedback counts.
type Stats struct {
	Transitions int            // total observed transitions
	Pairs       map[string]int // prev -> total transitions
	TopNext     map[string]int // prev -> next -> count (flattened key "prev→next")

	// Markov evaluation triple + count model (Issue #272). Coverage and
	// Accuracy are 0 when their denominator is 0; the denominators
	// (ReadOnlyCalls, TotalHits+TotalWastes) are exposed so consumers can
	// render N/A instead.
	ReadOnlyCalls int     // read-only tool calls observed
	CoveredCalls  int     // of those, covered by the most recent Plan proposal
	Coverage      float64 // CoveredCalls / ReadOnlyCalls
	TotalHits     int     // ObserveHit calls (feedback-level accuracy)
	TotalWastes   int     // ObserveWaste calls
	Accuracy      float64 // TotalHits / (TotalHits + TotalWastes)

	HitRate    map[string]float64 // per-key posterior mean μ
	Alpha      map[string]float64 // per-key posterior α (incl. prior)
	Beta       map[string]float64 // per-key posterior β (incl. prior)
	LowerBound map[string]float64 // per-key 90% Wilson lower bound (0 = no feedback)
	Hits       map[string]int     // per-key hit feedback count
	Wastes     map[string]int     // per-key waste feedback count
}

// Stats returns a snapshot of the transition matrix (observability).
func (m *MatrixPrefetcher) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Stats{
		Transitions:   m.countsTotal(),
		Pairs:         cloneIntMap(m.counts),
		TopNext:       cloneNested(m.bigrams),
		ReadOnlyCalls: m.readOnlyCalls,
		CoveredCalls:  m.coveredCalls,
		TotalHits:     m.totalHits,
		TotalWastes:   m.totalWastes,
		HitRate:       make(map[string]float64),
		Alpha:         make(map[string]float64),
		Beta:          make(map[string]float64),
		LowerBound:    make(map[string]float64),
		Hits:          cloneIntMap(m.hitCount),
		Wastes:        cloneIntMap(m.wasteCount),
	}
	if m.readOnlyCalls > 0 {
		s.Coverage = float64(m.coveredCalls) / float64(m.readOnlyCalls)
	}
	if m.totalHits+m.totalWastes > 0 {
		s.Accuracy = float64(m.totalHits) / float64(m.totalHits+m.totalWastes)
	}
	for key, hw := range m.hitW {
		ww := m.wasteW[key]
		s.HitRate[key] = posteriorMean(hw, ww)
		s.Alpha[key] = hw + betaPrior
		s.Beta[key] = ww + betaPrior
		if n := hw + ww; n > 0 {
			s.LowerBound[key] = wilsonLower(s.HitRate[key], n)
		}
	}
	return s
}

// demoted reports whether a candidate's discounted waste/hit weight ratio
// exceeds the limit (posterior mean μ < 1/(1+WasteHitLimit)). Caller holds
// m.mu. A candidate with no feedback (prior state) is never demoted.
func (m *MatrixPrefetcher) demoted(key string) bool {
	hw, ww := m.hitW[key], m.wasteW[key]
	if hw+ww == 0 {
		return false
	}
	return posteriorMean(hw, ww) < 1/(1+m.cfg.WasteHitLimit)
}

// posteriorMean is the Beta posterior mean of the hit propensity given
// time-discounted feedback weights hw (hits) and ww (wastes) plus the
// uniform prior betaPrior.
func posteriorMean(hw, ww float64) float64 {
	return (hw + betaPrior) / (hw + ww + 2*betaPrior)
}

// wilsonLower is the 90% (z=1.645) one-sided Wilson lower bound for the
// hit propensity with discounted sample size n and observed mean mu. It
// tolerates fractional n (time-discounted weights), unlike a Beta quantile
// requiring integer counts (Issue #272).
func wilsonLower(mu, n float64) float64 {
	if n <= 0 {
		return 0
	}
	const z = 1.645
	denom := 1 + z*z/n
	center := mu + z*z/(2*n)
	margin := z * math.Sqrt(mu*(1-mu)/n+z*z/(4*n*n))
	return (center - margin) / denom
}

func (m *MatrixPrefetcher) countsTotal() int {
	total := 0
	for _, c := range m.counts {
		total += c
	}
	return total
}

func cloneIntMap(src map[string]int) map[string]int {
	dst := make(map[string]int, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneNested(src map[string]map[string]int) map[string]int {
	dst := make(map[string]int)
	for prev, nexts := range src {
		for next, cnt := range nexts {
			dst[prev+"→"+next] = cnt
		}
	}
	return dst
}
