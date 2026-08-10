package prefetch

// PrefetchTask is one speculative read-only prefetch unit.
type PrefetchTask struct {
	Kind string // "slice-assembly" | "embedding" | "file" (low priority)
	Key  string // identifying key (e.g. slice id, file path)
	Cost int    // estimated token/IO cost for budgeting
}

// Prefetcher plans speculative prefetches during LLM wait time (MVP in M3).
type Prefetcher interface {
	// Plan returns prefetch tasks given the last tool names (transition
	// matrix + path patterns). Must only propose read-only work.
	Plan(lastToolNames []string) ([]PrefetchTask, error)
}
