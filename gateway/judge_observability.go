package gateway

import (
	"context"
	"log"
	"sync"

	"semantix/kernel/cache"
	"semantix/kernel/usage"
)

// Grey-zone judge observability (Issue #242 gap 1).
//
// Why both a log line and a usage-event field, rather than one of them:
//
//   - The log line is for the operator mid-incident. "Why did t10 not hit?"
//     must be answerable by grepping one stream, without joining JSONL
//     records, and it must survive even when no usage log is configured.
//   - The usage-event field is for cost. The judge is a separate upstream
//     model call that does not traverse the New API channel, so nothing
//     else meters it — which is exactly why the GW4 acceptance's 52.7%
//     savings figure carries an unquantifiable caveat. usage.Summarize is
//     the only place this project turns the log into a savings number, so a
//     cost that figure must be reconciled against has to be a field there.
//     A log line cannot be summed; a grep is not an accounting ledger.
//
// The two carry the same facts; neither is derived from the other, so a
// deployment that ships logs and usage to different places keeps both
// questions answerable.

// judgeCollector accumulates the grey-zone judge decisions taken while
// serving one request, so they can be attached to that turn's usage event.
// One instance per request; the mutex guards against a future concurrent
// candidate evaluation inside the decider.
type judgeCollector struct {
	mu        sync.Mutex
	decisions []usage.JudgeDecision
}

// judgeCollectorKey is the private context key for the per-request
// collector. The decider's OnJudge hook is process-wide, so the context is
// what routes an observation back to the request that caused it.
type judgeCollectorKey struct{}

func withJudgeCollector(ctx context.Context, c *judgeCollector) context.Context {
	return context.WithValue(ctx, judgeCollectorKey{}, c)
}

func judgeCollectorFrom(ctx context.Context) *judgeCollector {
	c, _ := ctx.Value(judgeCollectorKey{}).(*judgeCollector)
	return c
}

func (c *judgeCollector) add(d usage.JudgeDecision) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.decisions = append(c.decisions, d)
}

// drain returns the collected decisions (nil when none), leaving the
// collector empty so a decision is never attributed to two events.
func (c *judgeCollector) drain() []usage.JudgeDecision {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.decisions
	c.decisions = nil
	return out
}

// observeJudge is the cache.L3Decider.OnJudge hook: it logs one structured
// line and records the decision on the in-flight turn. Both are
// best-effort and never affect the reuse verdict — the decision has already
// been made by the time this runs.
func (g *Gateway) observeJudge(ctx context.Context, obs cache.JudgeObservation) {
	d := usage.JudgeDecision{
		SliceID:       obs.SliceID,
		RelConfidence: obs.RelConfidence,
		Verdict:       obs.Verdict,
		Reason:        obs.Reason,
		Called:        obs.Called,
		FailClosed:    obs.FailClosed,
		Error:         obs.Err,
		LatencyMs:     obs.Latency.Milliseconds(),
		// byte/4, the same estimator the gateway uses for its synthetic
		// usage chunks — never a tokenizer count.
		PromptTokens: int64(obs.PromptBytes / 4),
	}
	log.Printf("gateway: l3 judge slice=%s rel=%.3f verdict=%s called=%t fail_closed=%t latency_ms=%d prompt_tokens=%d reason=%q err=%q",
		d.SliceID, d.RelConfidence, d.Verdict, d.Called, d.FailClosed, d.LatencyMs, d.PromptTokens, d.Reason, d.Error)
	// Judge rejections are negative evidence for the per-entry adaptive
	// engine (Issue #259 阶段 3): the judge declined to reuse this slice
	// from its grey band — the entry should tighten so fewer of its
	// candidates consume judge calls / risk false reuses.
	if g.adapt != nil && obs.Verdict == cache.JudgeDeclined && obs.SliceID != "" {
		g.adapt.Observe(obs.SliceID, true, g.zones.TauLow)
	}
	if c := judgeCollectorFrom(ctx); c != nil {
		c.add(d)
	}
}
