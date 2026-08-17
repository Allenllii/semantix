package agent

import (
	"fmt"
	"strings"

	"semantix/harness/event"
	"semantix/harness/nilutil"
	"semantix/harness/provider"
	"semantix/kernel/sched"
)

// TierResolver resolves a scheduler tier to a concrete provider runtime.
// The returned context window and pricing become active with the provider.
type TierResolver func(tier string) (provider.Provider, *provider.Pricing, int, error)

func (a *Agent) applyScheduledTier(tier, budgetAction string) {
	tier = strings.TrimSpace(tier)
	if budgetAction == sched.BudgetActionDegradeTier {
		tier = "flash"
	}
	if tier == "" {
		return
	}
	if a.svc.tierResolver == nil {
		a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "sched tier=" + tier})
		return
	}
	if tier == a.modelRef {
		return
	}
	prov, pricing, contextWindow, err := a.svc.tierResolver(tier)
	if err != nil || nilutil.IsNil(prov) {
		if err == nil {
			err = fmt.Errorf("resolver returned nil provider")
		}
		a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
			Text: "scheduler tier switch failed; keeping current model", Detail: err.Error()})
		return
	}
	a.svc.prov = prov
	a.svc.pricing = pricing
	a.modelRef = tier
	// Provider/model changes invalidate tokenizer-derived admission estimates
	// and provider-specific output limits; rebuild them from the new runtime.
	a.sess.output.outputBudget = outputBudgetOf(prov)
	a.sess.output.activeReqShape.Store(nil)
	a.sess.output.promptCalibration.Store(nil)
	a.sess.output.contextUsage.Store(nil)
	if contextWindow > 0 {
		a.contextWindow = contextWindow
	}
	a.InvalidateProjection()
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "sched tier=" + tier})
}
