package prefetch

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
