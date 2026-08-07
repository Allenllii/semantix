package cache

import (
	"context"

	"semantix/kernel/slice"
)

// Query is the semantic-cache lookup input.
type Query struct {
	SessionID   string
	UserInput   string
	Intent      string // from the intent classifier (later milestone)
	ContextHash string // stable hash of the current context fingerprint
	Scope       slice.Scope
}

// L3Result is a reusable cached outcome (verified, fail-closed).
type L3Result struct {
	SliceID  string
	Response string
	CostUSD  float64
}

// Decider makes L2/L3 decisions (MVP implementation in M1; constants below
// are the shared params file — U6+ must read, not modify).
type Decider interface {
	// DecideL2 returns slices eligible for stable injection (tau-filtered,
	// budget-truncated, canonical order by ID).
	DecideL2(ctx context.Context, q Query) ([]slice.Hit, error)
	// DecideL3 returns a verified reusable result or nil (fingerprint + whitelist).
	DecideL3(ctx context.Context, q Query) (*L3Result, error)
}

// Default parameters shared across the kernel (frozen by U1; see §4 rules).
const (
	TauL2      = 0.75 // similarity threshold for L2 injection (evolution-tuned)
	TauL3      = 0.90 // similarity threshold for L3 reuse (fail-closed high)
	InjectCap  = 0.15 // max injected bytes as fraction of prompt budget
	L3TTLHours = 24   // L3 results expire after this
)
