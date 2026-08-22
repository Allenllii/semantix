package cache

import (
	"context"
	"time"

	"semantix/kernel/judge"
)

// Verdict values carried by JudgeObservation.Verdict (Issue #242 gap 1).
// They are deliberately plain strings: the observation crosses a package
// boundary into whatever the host wants to persist (a log line, a usage
// event), and kernel/cache must not learn about either.
const (
	// JudgeApproved: the judge was asked and said the cached answer is
	// reusable. One billable judge call.
	JudgeApproved = "approved"
	// JudgeDeclined: the judge was asked and said no. One billable judge
	// call — this is a real verdict, not a fallback.
	JudgeDeclined = "declined"
	// JudgeFailClosed: the judge call errored or timed out and the grey
	// candidate was rejected conservatively. The call may still have been
	// billed upstream, but the rejection carries no semantic information.
	JudgeFailClosed = "fail_closed"
	// JudgeSkipped: the candidate was rejected by the fingerprint gate or
	// the rule gate before the judge was reached. No judge cost.
	JudgeSkipped = "skipped"
)

// JudgeObservation is one grey-zone verification outcome, emitted through
// L3Decider.OnJudge (Issue #242 gap 1).
//
// It exists because a grey candidate that is not reused is otherwise
// indistinguishable on disk from a candidate whose judge call failed —
// and because the judge is a separate upstream model call that no usage
// event accounted for, making its cost unmeasurable. Every field here is
// either "why was this not reused" or "what did asking cost".
//
// The type stays on the standard library plus kernel/judge so the L3
// decider keeps its architecture contract (docs/specs/
// h2h3-resource-orchestration.md §6): no event bus, no gateway, no harness.
type JudgeObservation struct {
	// SliceID is the candidate that entered the grey zone.
	SliceID string
	// RelConfidence is the score/top1 ratio that classified the candidate
	// as grey — the number that explains why the judge was consulted.
	RelConfidence float64
	// Verdict is one of JudgeApproved / JudgeDeclined / JudgeFailClosed /
	// JudgeSkipped.
	Verdict string
	// Reason is judge.RuleGate.Chain's own human-readable explanation.
	Reason string
	// Called reports whether the LLM judge was actually invoked. Only a
	// called judge costs money; this is the unit of judge cost accounting.
	Called bool
	// FailClosed reports that the outcome came from an error or timeout
	// rather than from a verdict. This is the distinction the GW4
	// acceptance could not make.
	FailClosed bool
	// Err is the judge error text when FailClosed (empty otherwise).
	Err string
	// Latency is how long the judge call took (zero when not called).
	Latency time.Duration
	// PromptBytes estimates the judge input size (query + candidate
	// content) so a token/cost estimate can be derived downstream without
	// kernel/cache knowing any pricing.
	PromptBytes int
}

// timedJudge wraps a Judge to record whether it was reached and how long it
// took. One instance serves exactly one RuleGate.Chain call, which invokes
// Confirm at most once synchronously, so no locking is needed.
type timedJudge struct {
	inner   judge.Judge
	called  bool
	latency time.Duration
}

func (t *timedJudge) Confirm(ctx context.Context, c judge.Candidate) (bool, error) {
	t.called = true
	start := time.Now()
	ok, err := t.inner.Confirm(ctx, c)
	t.latency = time.Since(start)
	return ok, err
}

// relConfidence is the grey-zone ratio zone.Classify keys on, guarded
// against a non-positive top1 (which classifies as Miss and never reaches
// the judge, but must not produce Inf/NaN if it ever did).
func relConfidence(score, top1 float64) float64 {
	if top1 <= 0 {
		return 0
	}
	return score / top1
}
// LexicalGateObservation is one zone-Hit candidate the lexical support
// gate evaluated (Issue #260), emitted through L3Decider.OnLexicalGate.
// It lets the host count gate blocks and measure the hit-rate loss the
// gate introduces, without kernel/cache learning any sink.
type LexicalGateObservation struct {
	// SliceID is the candidate the gate evaluated.
	SliceID string
	// Score is the fused retrieval score that classified the candidate.
	Score float64
	// Lexical is the reported lexical-support score (BM25 route / coverage).
	Lexical float64
	// Blocked reports whether the gate downgraded this Hit to Grey.
	Blocked bool
	// Zone is the zone after the gate decision ("hit" or "grey").
	Zone string
}
