// Package event defines the kernel event contract: kinds, payloads, wire format
// and the in-process bus. It is the single source of truth for what the kernel
// observes and emits; adapters (Reasonix sink bridge, MCP) translate external
// harness events into this contract.
package event

import (
	"encoding/json"
	"time"
)

// Kind tags an Event. The payload type for each Kind is documented next to it;
// wire stability: json field names are part of the contract and must not change.
type Kind int

const (
	// TurnStarted marks the start of one user turn in a session.
	TurnStarted Kind = iota
	// Usage reports model-call token usage incl. cache hit/miss detail.
	Usage
	// ToolDispatch reports one tool call being dispatched.
	ToolDispatch
	// ToolResult reports one tool call outcome (ok, latency, error).
	ToolResult
	// ToolRoundEnd summarizes one tool batch (dispatched/succeeded counts).
	ToolRoundEnd
	// SliceHit reports a semantic cache hit (L1/L2/L3).
	SliceHit
	// SliceInject reports an L2 stable slice injection.
	SliceInject
	// SliceReject reports injection pollution (user edited/rolled back a slice).
	SliceReject
	// PrefetchHit reports a speculative prefetch that was used.
	PrefetchHit
	// PrefetchWaste reports a speculative prefetch that was not used.
	PrefetchWaste
	// Compact reports a context compaction (snip/prune/summary).
	Compact
	// EvolutionTick reports an evolution parameter update snapshot.
	EvolutionTick

	// KindCount is the sentinel for validation and tests.
	KindCount
)

// Event is the wire unit emitted on the bus.
type Event struct {
	Kind      Kind            `json:"kind"`
	SessionID string          `json:"session_id"`
	Turn      int             `json:"turn,omitempty"`
	At        time.Time       `json:"at"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// --- Payloads (one per Kind; field names are wire-stable) ---

// TurnStartedPayload carries no extra fields; SessionID/Turn live on Event.
type TurnStartedPayload struct{}

// UsagePayload reports model-call token usage including cache breakdown.
type UsagePayload struct {
	PromptTokens  int     `json:"prompt_tokens"`
	CacheHit      int     `json:"cache_hit"`
	CacheMiss     int     `json:"cache_miss"`
	CompletionTok int     `json:"completion_tokens"`
	CostUSD       float64 `json:"cost_usd"`
}

// ToolDispatchPayload describes one dispatched tool call.
type ToolDispatchPayload struct {
	CallID   string          `json:"call_id"`
	Name     string          `json:"name"`
	Args     json.RawMessage `json:"args,omitempty"`
	ReadOnly bool            `json:"read_only,omitempty"`
}

// ToolResultPayload describes one tool call outcome.
type ToolResultPayload struct {
	CallID  string        `json:"call_id"`
	OK      bool          `json:"ok"`
	Latency time.Duration `json:"latency_ns"`
	ErrMsg  string        `json:"err_msg,omitempty"`
}

// ToolRoundEndPayload summarizes one tool batch.
type ToolRoundEndPayload struct {
	Dispatched int `json:"dispatched"`
	Succeeded  int `json:"succeeded"`
}

// SliceHitPayload reports a semantic cache hit. Layer is "L1"|"L2"|"L3".
type SliceHitPayload struct {
	Layer    string    `json:"layer"`
	SliceIDs []string  `json:"slice_ids"`
	Scores   []float64 `json:"scores,omitempty"`
}

// SliceInjectPayload reports an L2 injection; SliceIDs are in canonical order.
type SliceInjectPayload struct {
	SliceIDs []string `json:"slice_ids"`
	Bytes    int      `json:"bytes"`
}

// SliceRejectPayload reports injection pollution.
type SliceRejectPayload struct {
	SliceID string `json:"slice_id"`
	Reason  string `json:"reason,omitempty"`
}

// PrefetchHitPayload reports a prefetch that was used.
type PrefetchHitPayload struct {
	Targets []string `json:"targets"`
}

// PrefetchWastePayload reports a prefetch that was not used.
type PrefetchWastePayload struct {
	Targets []string `json:"targets"`
}

// CompactPayload reports a context compaction. Trigger is "snip"|"prune"|"summary".
type CompactPayload struct {
	Trigger string `json:"trigger"`
	Before  int    `json:"before_tokens"`
	After   int    `json:"after_tokens"`
}

// EvolutionTickPayload carries the evolution parameter snapshot.
type EvolutionTickPayload struct {
	ParamsJSON json.RawMessage `json:"params"`
}
