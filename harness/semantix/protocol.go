package semantix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// This file is the shared semantix protocol client: it turns `semantix
// usage --json` / `semantix lookup --json` envelopes into typed views. Both
// renderers — the TUI reuse panel (U33) and the desktop reuse panel (U34) —
// consume exactly this client so the two ends can never drift apart.
//
// The envelope shape is the CLI's §4.2 JSON contract ({ok, command, data}).
// Fields added by U28 (slice_hits) and U30 (source_session) are optional:
// older kernels simply leave them zero, so this client stays forward- and
// backward-compatible with the U28/U30 merge.
//
// Every interaction is soft (fail-open, architecture §10-8): a missing
// binary, timeout, or non-zero exit returns ErrUnavailable, never a panic
// and never a blocked caller.

// ErrUnavailable wraps every kernel unavailability (binary missing, timeout,
// non-zero exit, unparseable envelope). Callers degrade fail-open.
var ErrUnavailable = errors.New("semantix: kernel unavailable")

// envelope is the CLI's §4.2 JSON envelope (ok/command/data/error).
type envelope struct {
	OK      bool            `json:"ok"`
	Command string          `json:"command"`
	Data    json.RawMessage `json:"data"`
	Error   *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// UsageSummary is the aggregated reuse/cost summary of `semantix usage
// --json` (U17/U22; U28 adds slice_hits). Field names mirror the JSON
// contract so the summary can be forwarded verbatim to a renderer.
type UsageSummary struct {
	Events         int     `json:"events"`
	TokensIn       int64   `json:"tokens_in"`
	TokensOut      int64   `json:"tokens_out"`
	CacheHitTokens int64   `json:"cache_hit_tokens"`
	L3Reuses       int     `json:"l3_reuses"`
	InjectedTokens int64   `json:"injected_tokens"`
	SliceHits      int     `json:"slice_hits"`
	CostPaidUSD    float64 `json:"cost_paid_usd"`
	CostNoCacheUSD float64 `json:"cost_no_cache_usd"`
	SavingsUSD     float64 `json:"savings_usd"`
	SavingsRate    float64 `json:"savings_rate"`
}

// LookupHit is one row of the `semantix lookup --json` data array (U8/H1
// protocol; U30 adds source_session provenance). Older kernels leave
// SourceSession empty — renderers hide the provenance column then.
type LookupHit struct {
	ID            string  `json:"id"`
	Type          string  `json:"type"`
	Scope         string  `json:"scope"`
	Score         float64 `json:"score"`
	Zone          string  `json:"zone"`
	SourceSession string  `json:"source_session"`
	Content       string  `json:"content"`
}

// Usage queries `semantix usage --json` and parses the summary. dir is the
// child's working directory ("" inherits the process cwd) so the kernel
// resolves its cwd-relative defaults (.semantix/...) against the project,
// not the host process. dbPath overrides the usage log; "" keeps the CLI
// default (<dir>/.semantix/usage.jsonl).
func Usage(ctx context.Context, binary, dir, dbPath string) (UsageSummary, error) {
	args := []string{"usage", "--json"}
	if dbPath != "" {
		args = append(args, "--db", dbPath)
	}
	out, err := runCLI(ctx, binary, dir, args)
	if err != nil {
		// The CLI writes the §4.2 failure envelope to stdout even on
		// non-zero exits (usage-class errors); surface its message when
		// present instead of the bare exit status.
		if msg := envelopeMessage(out); msg != "" {
			return UsageSummary{}, fmt.Errorf("%w: usage: %s", ErrUnavailable, msg)
		}
		return UsageSummary{}, err
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return UsageSummary{}, fmt.Errorf("%w: usage envelope: %v", ErrUnavailable, err)
	}
	if !env.OK || env.Command != "usage" {
		return UsageSummary{}, fmt.Errorf("%w: usage: %s", ErrUnavailable, envelopeText(env))
	}
	var sum UsageSummary
	if err := json.Unmarshal(env.Data, &sum); err != nil {
		return UsageSummary{}, fmt.Errorf("%w: usage data: %v", ErrUnavailable, err)
	}
	return sum, nil
}

// Lookup queries `semantix lookup --json` and returns the hit rows. query
// "" returns no hits (nil, nil) — a lookup without a query is a no-op, not
// an error. limit <= 0 falls back to 5 and is capped at 50 (kernel parity);
// scope "" defaults to "project".
func Lookup(ctx context.Context, binary, dir, query string, limit int, scope string) ([]LookupHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}
	if scope == "" {
		scope = "project"
	}
	args := []string{"lookup", "--query", query, "--limit", itoa(limit), "--scope", scope, "--json"}
	out, err := runCLI(ctx, binary, dir, args)
	if err != nil {
		if msg := envelopeMessage(out); msg != "" {
			return nil, fmt.Errorf("%w: lookup: %s", ErrUnavailable, msg)
		}
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("%w: lookup envelope: %v", ErrUnavailable, err)
	}
	if !env.OK || env.Command != "lookup" {
		return nil, fmt.Errorf("%w: lookup: %s", ErrUnavailable, envelopeText(env))
	}
	var hits []LookupHit
	if err := json.Unmarshal(env.Data, &hits); err != nil {
		return nil, fmt.Errorf("%w: lookup data: %v", ErrUnavailable, err)
	}
	return hits, nil
}

// Version probes `semantix version --json` and reports whether the kernel
// binary answers at all. It is the availability check for paths where no
// data file exists yet (e.g. a fresh project without a usage log).
func Version(ctx context.Context, binary, dir string) error {
	out, err := runCLI(ctx, binary, dir, []string{"version", "--json"})
	if err != nil {
		if msg := envelopeMessage(out); msg != "" {
			return fmt.Errorf("%w: version: %s", ErrUnavailable, msg)
		}
		return err
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return fmt.Errorf("%w: version envelope: %v", ErrUnavailable, err)
	}
	if !env.OK || env.Command != "version" {
		return fmt.Errorf("%w: version: %s", ErrUnavailable, envelopeText(env))
	}
	return nil
}

// envelopeText returns a human-readable failure reason from an envelope,
// preferring the CLI's error payload.
func envelopeText(env envelope) string {
	if env.Error != nil && env.Error.Message != "" {
		return env.Error.Message
	}
	return "envelope not ok"
}

// envelopeMessage is a best-effort extractor of the CLI's §4.2 failure
// message from an invocation that exited non-zero (the CLI writes the
// envelope to stdout before exiting). Returns "" when the output is not an
// envelope (runtime errors print to stderr instead).
func envelopeMessage(out []byte) string {
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return ""
	}
	return envelopeText(env)
}

// runCLI executes one kernel CLI invocation with a 3s safety cap and returns
// its output (stdout; on failure the merged output so the failure envelope,
// if any, survives), or ErrUnavailable on any failure. dir sets the child's
// working directory ("" inherits the process cwd).
func runCLI(ctx context.Context, binary, dir string, args []string) ([]byte, error) {
	if binary == "" {
		binary = "semantix"
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return out, nil
}
