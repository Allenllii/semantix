// Package semantix bridges the reasonix harness to the semantix kernel:
// session events are mirrored to kernel-compatible session JSONL, and kernel
// retrieval (lookup/inject) is exposed to the agent via subprocess calls.
package semantix

import (
	"context"
	"sync"

	"semantix/harness/event"
)

// Config is the kernel wiring configuration for one build. It mirrors
// config.SemantixConfig without importing internal/config (keeps this
// package a leaf dependency).
type Config struct {
	// Enabled mirrors session events to the kernel's session JSONL sink.
	Enabled bool
	// Binary is the kernel CLI path; empty defaults to "semantix" on PATH.
	Binary string
	// Inject appends the [semantix-reuse] block to the system prompt region.
	Inject bool
	// Budget caps the L2 injection block size in bytes (default 4096).
	Budget int
	// SessionsDir is where the session JSONL mirror is written; empty uses
	// <controller session dir>/sessions.
	SessionsDir string
}

// Bridge aggregates the kernel wiring for one harness build. It is optional:
// a nil Bridge (or one built with Enabled=false) makes the harness run
// without the kernel — every failure path degrades fail-open, never blocking
// the agent main loop.
type Bridge struct {
	cfg Config

	mu    sync.Mutex
	hs    *HarnessSink // lazily created once a session label is known
	dir   string       // resolved sessions dir ("" = not yet)
	label string       // controller session label (first real session id)
	// lastSavings is the last observed cumulative usage savings, used to
	// attribute the incremental per-turn delta in Reuse.
	lastSavings float64
}

// NewBridge builds a Bridge from cfg.
func NewBridge(cfg Config) *Bridge {
	if cfg.Budget <= 0 {
		cfg.Budget = 4096
	}
	return &Bridge{cfg: cfg}
}

// Enabled reports whether the kernel is wired in.
func (b *Bridge) Enabled() bool { return b != nil && b.cfg.Enabled }

// InjectEnabled reports whether L2 injection is wired on (used to decide
// whether speculative prefetch warm-up is worth starting).
func (b *Bridge) InjectEnabled() bool { return b != nil && b.cfg.Enabled && b.cfg.Inject }

// Sink wraps inner so every event is also mirrored into the kernel session
// JSONL. Returns inner unchanged when the kernel is not enabled (zero-cost
// no-op on the hot path).
func (b *Bridge) Sink(inner event.Sink) event.Sink {
	if !b.Enabled() {
		return inner
	}
	return &mirrorSink{bridge: b, inner: inner}
}

// SetLabel records the controller's session label; the JSONL mirror file is
// created on the first event after this is set. The first label wins: a
// controller keeps one session id per build.
func (b *Bridge) SetLabel(label string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.label == "" && label != "" {
		b.label = label
	}
}

// Inject runs the kernel's L2 injector for query and returns the
// [semantix-reuse] block, or "" when unavailable/timed out (soft degrade).
func (b *Bridge) Inject(ctx context.Context, query string) string {
	if !b.Enabled() || !b.cfg.Inject {
		return ""
	}
	return Inject(ctx, b.cfg.Binary, query, b.cfg.Budget)
}

// Lookup runs the kernel's semantix_lookup tool over the subprocess CLI and
// returns the raw tool output, or "" on any failure (soft degrade).
func (b *Bridge) Lookup(ctx context.Context, query string, limit int, scope string) string {
	if !b.Enabled() || query == "" {
		return ""
	}
	if limit <= 0 {
		limit = 5
	}
	if scope == "" {
		scope = "project"
	}
	args := []string{"lookup", "--query", query, "--limit", itoa(limit), "--scope", scope}
	out, err := runCLI(ctx, b.cfg.Binary, "", args)
	if err != nil {
		return ""
	}
	return string(out)
}

// Reuse gathers the per-turn reuse panel data (U33/H4a): the hits for query
// plus the incremental cost savings since the last snapshot, both through
// the shared protocol client (protocol.go). Kernel unavailable/timed out
// degrades to a zero summary — the panel hides and the agent main loop
// never blocks on the kernel.
func (b *Bridge) Reuse(ctx context.Context, query string) ReuseSummary {
	if !b.Enabled() || query == "" {
		return ReuseSummary{}
	}
	hits, err := Lookup(ctx, b.cfg.Binary, "", query, 5, "project")
	if err != nil {
		return ReuseSummary{}
	}
	sum := ReuseSummary{Hits: len(hits), Sources: topSources(hits)}
	if usage, err := Usage(ctx, b.cfg.Binary, "", ""); err == nil {
		b.mu.Lock()
		prev := b.lastSavings
		b.lastSavings = usage.SavingsUSD
		b.mu.Unlock()
		if delta := usage.SavingsUSD - prev; delta > 0 {
			sum.SavingsUSD = delta
		}
	}
	return sum
}

// mirrorSink forwards every event to inner and mirrors the session-relevant
// subset into the kernel JSONL via the bridge's HarnessSink.
type mirrorSink struct {
	bridge *Bridge
	inner  event.Sink
}

func (s *mirrorSink) Emit(e event.Event) {
	s.inner.Emit(e)
	s.bridge.mirror(e)
}

// mirror lazily creates the HarnessSink on the first session event after a
// label is known, then forwards. Failures are non-fatal: a write error is
// surfaced once via the inner sink and never blocks emission.
func (b *Bridge) mirror(e event.Event) {
	b.mu.Lock()
	if b.hs == nil {
		if b.label == "" {
			b.mu.Unlock()
			return // no session id yet; skip this event
		}
		dir := b.cfg.SessionsDir
		if dir == "" {
			dir = "" // caller resolves the controller session dir before SetLabel
		}
		hs, err := NewHarnessSink(dirOrFallback(dir), b.label, "")
		if err != nil {
			b.mu.Unlock()
			return // sink unavailable: drop the mirror, keep the harness alive
		}
		b.hs = hs
	}
	hs := b.hs
	b.mu.Unlock()
	hs.Emit(e)
}

// Close flushes and closes the mirror sink, if created.
func (b *Bridge) Close() error {
	b.mu.Lock()
	hs := b.hs
	b.hs = nil
	b.mu.Unlock()
	if hs == nil {
		return nil
	}
	return hs.Close()
}

// dirOrFallback returns dir, falling back to a per-build default when empty.
// (The harness resolves <session dir>/sessions before SetLabel and passes it
// through SessionsDir; the fallback keeps the bridge self-contained.)
func dirOrFallback(dir string) string {
	if dir != "" {
		return dir
	}
	return ".semantix/sessions"
}

// itoa is a tiny integer formatter avoiding strconv in a hot path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
