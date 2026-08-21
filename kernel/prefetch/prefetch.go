package prefetch

// PrefetchTask is one speculative read-only prefetch unit.
// Locality declares whether a prefetch task crosses a process boundary
// (Issue #273 speculative-privacy red line). Only LocalityLocal tasks are
// ever executed; egress (and undeclared) tasks are rejected by the Runner
// — Ghost Tool Calls (arXiv:2606.02483) shows a read-only whitelist does
// not stop intent leakage to an external observer.
const (
	LocalityLocal  = "local"  // local resources: index, slice store, local files
	LocalityEgress = "egress" // cross-process: MCP, network, any external service
)

type PrefetchTask struct {
	Kind string // "slice-assembly" | "embedding" | "file" (low priority)
	Key  string // identifying key (e.g. slice id, file path)
	Cost int    // estimated token/IO cost for budgeting
	// Locality declares the task execution boundary; empty is treated as
	// LocalityEgress by the Runner (fail-closed, matching the empty-whitelist
	// semantics).
	Locality string
}

// Prefetcher plans speculative prefetches during LLM wait time (MVP in M3).
type Prefetcher interface {
	// Plan returns prefetch tasks given the last tool names (transition
	// matrix + path patterns). Must only propose read-only work.
	Plan(lastToolNames []string) ([]PrefetchTask, error)
}
