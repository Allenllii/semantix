// Package usage records per-turn LLM usage events and summarizes the cost
// savings produced by the semantic cache layers (Issue #60 / U17):
//
//   - L2 savings: injected tokens that would otherwise be re-sent at miss
//     price, charged at the cache-hit price difference
//   - L3 savings: a fully reused Result slice skips the backend call — the
//     entire turn cost is saved
//
// Pricing defaults follow the DeepSeek-chat example (cache hit ≈ 1/4 of
// miss; see docs/reports/cache-taxonomy.md) and are overridable per call.
package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Default pricing (USD per 1M tokens), DeepSeek-chat example prices.
const (
	DefaultCostMissPerMTok = 0.27
	DefaultCostHitPerMTok  = 0.07
)

// Event is one LLM turn's usage observation.
type Event struct {
	SessionID     string  `json:"session_id,omitempty"`
	Turn          uint64  `json:"turn"`
	TokensIn      int64   `json:"tokens_in"`
	TokensOut     int64   `json:"tokens_out"`
	CacheHitToken int64   `json:"cache_hit_tokens"`
	// L3Reuse marks a turn fully served by a verified L3 result (no backend
	// call at all) — its entire cost is saved.
	L3Reuse bool `json:"l3_reuse,omitempty"`
	// InjectedTokens is the L2 injection block size for this turn.
	InjectedTokens int64 `json:"injected_tokens,omitempty"`
	At             int64 `json:"at"` // unix seconds
}

// Recorder appends usage events to a JSONL file (0600, atomic rewrite via
// temp+rename, tolerant of trailing bad lines).
type Recorder struct {
	path string
}

// NewRecorder opens (creating if needed) the usage log at path.
func NewRecorder(path string) (*Recorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("usage: mkdir: %w", err)
	}
	// Touch the file so Summarize on an empty-but-created log yields zeros.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("usage: create: %w", err)
	}
	f.Close()
	return &Recorder{path: path}, nil
}

// Append writes one event (append-only; a corrupt tail never blocks writes).
func (r *Recorder) Append(e Event) error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("usage: append: %w", err)
	}
	defer f.Close()
	enc, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("usage: marshal: %w", err)
	}
	if _, err := f.Write(append(enc, '\n')); err != nil {
		return fmt.Errorf("usage: write: %w", err)
	}
	return nil
}

// Summary aggregates a usage log.
type Summary struct {
	Events          int     // total turns recorded
	TokensIn        int64   // total input tokens (including cache hits)
	TokensOut       int64   // total output tokens
	CacheHitTokens  int64   // tokens served from the provider prefix cache
	L3Reuses        int     // turns fully served by L3 reuse
	InjectedTokens  int64   // total L2 injected tokens
	CostPaidUSD     float64 // what the user actually paid
	CostNoCacheUSD  float64 // what it would cost without any cache
	SavingsUSD      float64 // CostNoCache - CostPaid
	SavingsRate     float64 // Savings / CostNoCache (0 when no cost)
}

// Summarize reads the log at path and computes the aggregate. Malformed
// lines are skipped and never fatal.
//
// Cost model: provider input tokens are billed at miss price except the
// prefix-cached portion at hit price; output tokens at miss price. A turn
// with L3Reuse=true consumed nothing (no backend call) — its would-be cost
// is excluded from CostPaidUSD and counted entirely as savings.
func Summarize(path string, costMiss, costHit float64) (*Summary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("usage: open: %w", err)
	}
	defer f.Close()

	s := &Summary{}
	var l3In, l3Out int64 // tokens of L3-reused turns (not billed)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			continue // tolerate bad lines
		}
		s.Events++
		s.TokensIn += e.TokensIn
		s.TokensOut += e.TokensOut
		s.CacheHitTokens += e.CacheHitToken
		s.InjectedTokens += e.InjectedTokens
		if e.L3Reuse {
			s.L3Reuses++
			l3In += e.TokensIn
			l3Out += e.TokensOut
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("usage: scan: %w", err)
	}

	billedIn := s.TokensIn - s.CacheHitTokens - l3In
	billedOut := s.TokensOut - l3Out
	paid := float64(billedIn)*costMiss/1e6 +
		float64(s.CacheHitTokens)*costHit/1e6 +
		float64(billedOut)*costMiss/1e6
	noCache := float64(s.TokensIn)*costMiss/1e6 + float64(s.TokensOut)*costMiss/1e6

	s.CostPaidUSD = paid
	s.CostNoCacheUSD = noCache
	s.SavingsUSD = noCache - paid
	if noCache > 0 {
		s.SavingsRate = s.SavingsUSD / noCache
	}
	return s, nil
}
