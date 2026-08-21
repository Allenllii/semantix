package prefetch

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"semantix/kernel/slice"
)

// Planner is the Issue 62 MVP: it learns a T-Slice transition matrix from the
// slice library's ToolPattern slices and plans read-only prefetch tasks for
// the next predicted tool. Contract with the frozen interface:
//
//	- PrefetchTask.Kind is always "slice-assembly" (see spec §2.3): the matrix
//	  only carries tool names, never arguments, so file-path guessing is
//	  explicitly out of scope; the predicted tool name becomes the task Key
//	  and the executor retrieves candidate slices by it.
//	- fail-closed: a predicted target that is not in WhiteList never enters
//	  the plan; an empty WhiteList yields an always-empty plan.
//	- budget: candidates (probability-descending, tie-broken by name) are
//	  accumulated until the per-round Budget (token) or MaxTasks cap is hit.
type Planner struct {
	// Store is the ToolPattern slice source for Learn (required).
	Store slice.Store
	// WhiteList lists the only tools allowed to appear in a plan. Empty means
	// fail-closed: Plan returns no tasks. A predicted non-white-listed tool is
	// always dropped before any budget consideration.
	WhiteList []string
	// Budget is the per-round token budget; <=0 uses DefaultBudget.
	Budget int
	// MaxTasks caps tasks per round; <=0 uses DefaultMaxTasks.
	MaxTasks int
	// MaxLoad is the slot-occupancy ratio at which speculative work yields;
	// <=0 uses DefaultMaxLoad.
	MaxLoad float64

	mu     sync.RWMutex
	matrix *transitionMatrix // set by Learn; nil until then
	gate   gateCounter
}

// Defaults for the MVP budget control.
const (
	// DefaultBudget is the default per-round prefetch budget in estimated
	// tokens.
	DefaultBudget = 1000
	// DefaultMaxTasks caps tasks per round.
	DefaultMaxTasks = 3
	// DefaultTaskCost is the estimated token cost of one slice-assembly task.
	DefaultTaskCost = 200
)

// Learn rebuilds the transition matrix from the ToolPattern slices in
// p.Store (scope Project). It is idempotent: calling it twice with the same
// store yields the same matrix.
func (p *Planner) Learn() error {
	if p.Store == nil {
		return fmt.Errorf("prefetch: Planner.Store is required")
	}
	patterns, err := p.Store.List(slice.Project)
	if err != nil {
		return fmt.Errorf("prefetch: learn: list slices: %w", err)
	}
	m := newTransitionMatrix()
	for _, s := range patterns {
		if s == nil || s.Type != slice.ToolPattern {
			continue
		}
		m.addSeq(strings.Fields(string(s.Content)))
	}
	p.mu.Lock()
	p.matrix = m
	p.mu.Unlock()
	return nil
}

// Plan implements the frozen Prefetcher interface. It returns no tasks (and
// no error) when the matrix is not learned, when no usable last tool name is
// given, or when the whitelist is empty — never a guess.
func (p *Planner) Plan(lastToolNames []string) ([]PrefetchTask, error) {
	p.mu.RLock()
	m := p.matrix
	p.mu.RUnlock()
	if m == nil {
		return nil, nil // not learned: no guesses
	}
	last := lastToolName(lastToolNames)
	if last == "" {
		return nil, nil
	}
	white := whitelistSet(p.WhiteList)
	if len(white) == 0 {
		return nil, nil // fail-closed: empty whitelist means no prefetch
	}

	budget := p.budget()
	maxTasks := p.maxTasks()
	var tasks []PrefetchTask
	spent := 0
	for _, c := range m.next(last) {
		if _, ok := white[c.Name]; !ok {
			continue // fail-closed: non-read-only tools never enter the plan
		}
		cost := DefaultTaskCost
		if spent+cost > budget {
			break // budget truncation; candidates arrive probability-descending
		}
		tasks = append(tasks, PrefetchTask{Kind: "slice-assembly", Key: c.Name, Cost: cost})
		spent += cost
		if len(tasks) >= maxTasks {
			break
		}
	}
	return tasks, nil
}

// PlanWithLoad implements the optional load-aware extension while preserving
// the frozen Plan method for existing callers.
func (p *Planner) PlanWithLoad(lastToolNames []string, hint LoadHint) (PlanDecision, error) {
	maxLoad := p.MaxLoad
	if maxLoad <= 0 || maxLoad > 1 {
		maxLoad = DefaultMaxLoad
	}
	if reason := EvaluateLoad(hint, maxLoad); reason != "" {
		p.gate.observe(reason)
		return PlanDecision{Reason: reason}, nil
	}
	tasks, err := p.Plan(lastToolNames)
	if err != nil {
		return PlanDecision{}, err
	}
	reason := ReasonNoCandidate
	if len(tasks) > 0 {
		reason = ReasonCandidate
	}
	p.gate.observe(reason)
	return PlanDecision{Tasks: tasks, Reason: reason}, nil
}

// GateStats returns load-aware outcome counters.
func (p *Planner) GateStats() GateStats { return p.gate.snapshot() }

func (p *Planner) budget() int {
	if p.Budget <= 0 {
		return DefaultBudget
	}
	return p.Budget
}

func (p *Planner) maxTasks() int {
	if p.MaxTasks <= 0 {
		return DefaultMaxTasks
	}
	return p.MaxTasks
}

// lastToolName returns the last non-blank tool name in the sequence, or "".
func lastToolName(names []string) string {
	for i := len(names) - 1; i >= 0; i-- {
		if n := strings.TrimSpace(names[i]); n != "" {
			return n
		}
	}
	return ""
}

// whitelistSet normalizes WhiteList into a set; blanks are dropped.
func whitelistSet(list []string) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, n := range list {
		if n = strings.TrimSpace(n); n != "" {
			out[n] = true
		}
	}
	return out
}

// candidate is one possible next tool with its learned probability.
type candidate struct {
	Name string
	Prob float64
}

// transitionMatrix counts 2-grams (from -> to) over tool-name sequences.
type transitionMatrix struct {
	counts map[string]map[string]int
	totals map[string]int
}

func newTransitionMatrix() *transitionMatrix {
	return &transitionMatrix{
		counts: make(map[string]map[string]int),
		totals: make(map[string]int),
	}
}

// addSeq learns the adjacent pairs of one tool-name sequence.
func (m *transitionMatrix) addSeq(seq []string) {
	for i := 0; i+1 < len(seq); i++ {
		from, to := seq[i], seq[i+1]
		if from == "" || to == "" {
			continue
		}
		if m.counts[from] == nil {
			m.counts[from] = make(map[string]int)
		}
		m.counts[from][to]++
		m.totals[from]++
	}
}

// prob returns P(to|from); 0 for an unseen transition.
func (m *transitionMatrix) prob(from, to string) float64 {
	total := m.totals[from]
	if total == 0 {
		return 0
	}
	return float64(m.counts[from][to]) / float64(total)
}

// next returns the successors of from sorted by probability descending, ties
// broken by name ascending (deterministic output).
func (m *transitionMatrix) next(from string) []candidate {
	var out []candidate
	for to := range m.counts[from] {
		out = append(out, candidate{Name: to, Prob: m.prob(from, to)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Prob != out[j].Prob {
			return out[i].Prob > out[j].Prob
		}
		return out[i].Name < out[j].Name
	})
	return out
}
