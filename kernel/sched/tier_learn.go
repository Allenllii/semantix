package sched

import (
	"strings"
	"sync"
)

// Tier learning (research plan W5: "tier 规则学习化"). The hard rules in
// decideTier stay as the safety floor — intent=mutation and writer presence
// always go pro — but the *complexity* rule ("more than ComplexTools calls in
// a round needs the strong model") is replaced by a feedback-driven decision
// once enough rounds have been observed: a complex-shaped round only needs
// the pro tier when the cheap tier demonstrably fails on such rounds.
//
// Evidence comes from ObserveTier: one entry per executed round carrying the
// tier the round actually ran on, whether it succeeded, and the round's
// measured token cost. Two per-tier EWMAs (success rate, token cost) are
// maintained for the complex-shape class specifically, because that is the
// only rule the learned policy is allowed to override.

// TierObservation is one executed round's outcome, fed back by the harness
// after the round's model call completes.
type TierObservation struct {
	// Tier is the tier the round actually ran on ("flash"/"pro" or whatever
	// names the Config uses).
	Tier string
	// Intent is the frozen turn intent name ("" when unknown).
	Intent string
	// Writers is the number of writer-capable calls in the round.
	Writers int
	// Calls is the total number of tool calls in the round.
	Calls int
	// Success reports whether the round completed without a failed /
	// blocked call.
	Success bool
	// Tokens is the round's measured input+output token cost (0 when the
	// caller could not measure it; such samples update the success EWMA
	// but not the cost EWMA).
	Tokens int64
}

// tierClassBucket is the learned-model's unit of evidence: complex-shaped
// rounds (no writers, not a mutation intent, more than ComplexTools calls)
// run on one of the two tiers and produce one observation each.
type tierClassBucket struct {
	samples     int
	successEWMA float64
	hasSuccess  bool
	costSamples int
	costEWMA    float64
	hasCost     bool
}

// tierLearner accumulates TierObservations and answers the only question the
// learned policy may ask: for a complex-shaped round, does the cheap tier
// have enough evidence, and did it hold up?
type tierLearner struct {
	mu         sync.Mutex
	minSamples int
	alpha      float64
	byTier     map[string]*tierClassBucket
	learnedUse int // rounds where the learned policy overrode the hard rule
	fallbacks  int // rounds where evidence was insufficient (hard rule kept)
}

func newTierLearner(minSamples int) *tierLearner {
	if minSamples <= 0 {
		minSamples = 10
	}
	return &tierLearner{
		minSamples: minSamples,
		alpha:      0.2,
		byTier:     map[string]*tierClassBucket{},
	}
}

func (l *tierLearner) observe(o TierObservation) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.byTier[o.Tier]
	if b == nil {
		b = &tierClassBucket{}
		l.byTier[o.Tier] = b
	}
	b.samples++
	sv := 0.0
	if o.Success {
		sv = 1
	}
	if !b.hasSuccess {
		b.successEWMA = sv
		b.hasSuccess = true
	} else {
		b.successEWMA = l.alpha*sv + (1-l.alpha)*b.successEWMA
	}
	if o.Tokens > 0 {
		b.costSamples++
		if !b.hasCost {
			b.costEWMA = float64(o.Tokens)
			b.hasCost = true
		} else {
			b.costEWMA = l.alpha*float64(o.Tokens) + (1-l.alpha)*b.costEWMA
		}
	}
}

// complexDecision is the learned answer for one complex-shaped round.
type complexDecision struct {
	tier   string
	reason string
	ok     bool // false → keep the hard rule (TierReason "complex:N")
}

// decideComplex applies the learned policy. Rules, evaluated in order and
// always returning a stable, auditable reason:
//  1. insufficient evidence on the cheap tier → hard rule ("complex:N");
//  2. cheap tier holds (success ≥ floor) and its measured cost is not
//     worse than pro's → stay cheap ("learned:complex-cheap-ok");
//  3. cheap tier fails or is not cheaper → pro ("learned:complex-needs-pro").
//
// pro-tier evidence is advisory only: when the cheap tier fails, pro is
// chosen regardless of its own success rate (fail toward capability).
func (l *tierLearner) decideComplex(cfg Config) complexDecision {
	l.mu.Lock()
	defer l.mu.Unlock()
	cheap := l.byTier[cfg.DefaultTier]
	if cheap == nil || cheap.samples < l.minSamples || !cheap.hasSuccess {
		l.fallbacks++
		return complexDecision{ok: false}
	}
	pro := l.byTier[cfg.ProTier]
	switch {
	case cheap.successEWMA >= cfg.SuccessFloor && (!pro.hasCost || !cheap.hasCost || cheap.costEWMA <= pro.costEWMA):
		l.learnedUse++
		return complexDecision{tier: cfg.DefaultTier, reason: "learned:complex-cheap-ok", ok: true}
	default:
		l.learnedUse++
		return complexDecision{tier: cfg.ProTier, reason: "learned:complex-needs-pro", ok: true}
	}
}

// TierLearnedStats snapshots the learner for observability (usage/dashboards).
type TierLearnedStats struct {
	Samples    int                `json:"samples"`     // complex-shape samples per tier
	LearnedUse int                `json:"learned_use"` // rounds decided by the learned policy
	Fallbacks  int                `json:"fallbacks"`   // rounds kept on the hard rule
	ByTier     map[string]float64 `json:"byTier"`      // per-tier success EWMA
}

func (l *tierLearner) stats() TierLearnedStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := TierLearnedStats{
		LearnedUse: l.learnedUse,
		Fallbacks:  l.fallbacks,
		ByTier:     map[string]float64{},
	}
	for tier, b := range l.byTier {
		out.Samples += b.samples
		if b.hasSuccess {
			out.ByTier[tier] = b.successEWMA
		}
	}
	return out
}

// isComplexShape reports whether a round is governed by the complexity rule
// (the only rule the learned policy may override): no writers, no mutation
// intent, more than ComplexTools calls. decideTier's first two rules are
// safety floors and never learned.
func isComplexShape(intent string, calls []ToolCallInfo, cfg Config) bool {
	if len(calls) <= cfg.ComplexTools {
		return false
	}
	for _, c := range calls {
		if !c.ReadOnly {
			return false
		}
	}
	i := strings.ToLower(strings.TrimSpace(intent))
	return i != "mutation" && i != "persistent_action"
}
