package agent

type prefetchedInjectResult struct {
	Text    string
	Targets []string
	Turn    int64
}

func (a *Agent) storePrefetch(next *prefetchedInjectResult) {
	if next == nil || next.Text == "" {
		return
	}
	if old := a.prefetchedInject.Swap(next); old != nil {
		a.recordPrefetch(false, old)
	}
}

func (a *Agent) takePrefetch(turn int64) *prefetchedInjectResult {
	got := a.prefetchedInject.Swap(nil)
	if got == nil {
		return nil
	}
	if got.Turn != turn {
		a.recordPrefetch(false, got)
		return nil
	}
	a.recordPrefetch(true, got)
	return got
}

func (a *Agent) wastePrefetch() {
	if got := a.prefetchedInject.Swap(nil); got != nil {
		a.recordPrefetch(false, got)
	}
}

func (a *Agent) recordPrefetch(hit bool, got *prefetchedInjectResult) {
	if a != nil && a.semantix != nil && got != nil {
		a.semantix.RecordPrefetch(hit, got.Targets, int(got.Turn))
	}
}
