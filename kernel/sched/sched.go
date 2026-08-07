package sched

import (
	"context"

	"semantix/kernel/slice"
)

// RoundInput describes one tool round for scheduling decisions.
type RoundInput struct {
	Intent      string
	ToolCalls   []ToolCallInfo
	SliceHits   []slice.Hit
	BudgetUSD   float64
	Concurrency int // available slots
}

// ToolCallInfo is a single call in the round.
type ToolCallInfo struct {
	Name     string
	ReadOnly bool
	Args     map[string]any
}

// RoundPlan is the scheduler's joint decision (MVP in M3; stub now).
type RoundPlan struct {
	ParallelGroups [][]string // groups of call IDs to run concurrently
	Tier           string     // "flash" | "pro"
	InjectIDs      []string   // L2 slice IDs in canonical order
	PrefetchIDs    []string   // prefetch targets (may be empty)
}

// Decider plans each tool round (MVP implementation in M3).
type Decider interface {
	DecideRound(ctx context.Context, in RoundInput) (RoundPlan, error)
}
