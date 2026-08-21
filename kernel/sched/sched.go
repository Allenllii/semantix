package sched

import (
	"context"

	"semantix/kernel/slice"
)

// RoundInput describes one tool round for scheduling decisions.
type RoundInput struct {
	Intent         string
	ToolCalls      []ToolCallInfo
	SliceHits      []slice.Hit
	BudgetUSD      float64
	Concurrency    int // available slots
	SuspendedTools []string
	Budget         BudgetState
	PrefetchLoad   PrefetchLoadHint
}

// PrefetchLoadHint is the scheduler-facing, dependency-neutral load snapshot.
// Millisecond fields are zero when the harness has no history yet.
type PrefetchLoadHint struct {
	ConcurrencyUsed  int
	ConcurrencyLimit int
	WaitWindowMS     int64
	TaskEstimateMS   int64
}

// BudgetState is the scheduler's read-only view of the resource catalog.
// The harness remains the authority for enforcement; the decider uses this
// snapshot only to issue an early BudgetAction hint.
type BudgetState struct {
	LimitUSD float64
	SpentUSD float64
	Window   string
}

// ToolCallInfo is a single call in the round.
type ToolCallInfo struct {
	CallID   string // matches event.ToolDispatchPayload.CallID
	Name     string
	ReadOnly bool
	Args     map[string]any
}

// RoundPlan is the scheduler's joint decision (MVP in M3; stub now).
type RoundPlan struct {
	ParallelGroups [][]string // groups of call IDs to run concurrently
	Tier           string     // "flash" | "pro"
	TierReason     string     `json:"tierReason,omitempty"` // stable explanation; does not affect execution
	InjectIDs      []string   // L2 slice IDs in canonical order
	PrefetchIDs    []string   // prefetch targets (may be empty)
	PrefetchReason string     `json:"prefetchReason,omitempty"`
	SuspendTools   []string   `json:"suspendTools,omitempty"`
	MaxParallel    int        `json:"maxParallel,omitempty"`  // 0 = harness default; >0 = forced cap
	BudgetAction   string     `json:"budgetAction,omitempty"` // one of BudgetAction* below
}

const (
	BudgetActionDegradeTier = "degrade_tier"
	// BudgetActionDegradeInject shrinks L2 injection instead of dropping it
	// (Issue #270 step 2): inject half the block budget, keep the harness
	// running. Sits between halt_prefetch (0.7) and degrade_tier (0.9).
	BudgetActionDegradeInject = "degrade_inject"
	BudgetActionHaltPrefetch  = "halt_prefetch"
	BudgetActionHardStop      = "hard_stop"
)

// Decider plans each tool round (MVP implementation in M3).
type Decider interface {
	DecideRound(ctx context.Context, in RoundInput) (RoundPlan, error)
}
