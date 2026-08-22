package prefetch

import (
	"time"

	"semantix/kernel/sched"
)

// AsPlanFunc adapts any Prefetcher to the scheduler's prefetch-plan shape
// (func(lastToolNames []string) []string) so sched.RuleDecider can hold a
// prefetcher without importing this package — the dependency direction stays
// sched → nothing. Each planned task is reduced to its Key; a nil prefetcher,
// an errored plan, or an empty plan all yield nil.
func AsPlanFunc(p Prefetcher) func(lastToolNames []string) []string {
	if p == nil {
		return func([]string) []string { return nil }
	}
	return func(lastToolNames []string) []string {
		tasks, err := p.Plan(lastToolNames)
		if err != nil || len(tasks) == 0 {
			return nil
		}
		keys := make([]string, 0, len(tasks))
		for _, t := range tasks {
			keys = append(keys, t.Key)
		}
		return keys
	}
}

// AsLoadAwarePlanFunc adapts a Prefetcher to the scheduler's additive
// load-aware callback. Old implementations still work through Plan; those
// implementing LoadAwarePrefetcher receive the full hint.
func AsLoadAwarePlanFunc(p Prefetcher) sched.LoadAwarePrefetchPlanFunc {
	return func(lastToolNames []string, hint sched.PrefetchLoadHint) sched.PrefetchPlanResult {
		if p == nil {
			return sched.PrefetchPlanResult{Reason: string(ReasonNoCandidate)}
		}
		var (
			decision PlanDecision
			err      error
		)
		if aware, ok := p.(LoadAwarePrefetcher); ok {
			decision, err = aware.PlanWithLoad(lastToolNames, LoadHint{
				ConcurrencyUsed:  hint.ConcurrencyUsed,
				ConcurrencyLimit: hint.ConcurrencyLimit,
				WaitWindow:       time.Duration(hint.WaitWindowMS) * time.Millisecond,
				TaskEstimate:     time.Duration(hint.TaskEstimateMS) * time.Millisecond,
			})
		} else {
			decision.Tasks, err = p.Plan(lastToolNames)
			decision.Reason = ReasonNoCandidate
			if len(decision.Tasks) > 0 {
				decision.Reason = ReasonCandidate
			}
		}
		if err != nil {
			return sched.PrefetchPlanResult{Reason: string(ReasonPlanError)}
		}
		var ids []string
		if len(decision.Tasks) > 0 {
			ids = make([]string, 0, len(decision.Tasks))
		}
		for _, task := range decision.Tasks {
			ids = append(ids, task.Key)
		}
		return sched.PrefetchPlanResult{IDs: ids, Reason: string(decision.Reason)}
	}
}
