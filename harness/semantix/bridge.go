// Package semantix bridges the semantix harness to the semantix kernel:
// session events are mirrored to kernel-compatible session JSONL, and kernel
// retrieval (lookup/inject) is exposed to the agent via subprocess calls.
package semantix

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"semantix/harness/event"
	"semantix/kernel/bm25"
	kernelevent "semantix/kernel/event"
	"semantix/kernel/evolve"
	"semantix/kernel/inject"
	"semantix/kernel/slice"
	"semantix/kernel/usage"
	"semantix/kernel/zone"
)

// Config is the kernel wiring configuration for one build. It mirrors
// config.SemantixConfig without importing internal/config (keeps this
// package a leaf dependency).
type Config struct {
	// Enabled mirrors session events to the kernel's session JSONL sink.
	Enabled bool
	// Binary is the kernel CLI path; empty defaults to "semantix" on PATH.
	// Retained for the legacy semantix_lookup tool; the reuse panel and
	// injection read the kernel in-process (U39) and never spawn the CLI.
	Binary string
	// Inject appends the [semantix-reuse] block to the system prompt region.
	Inject bool
	// Budget caps the L2 injection block size in bytes (default 4096).
	Budget int
	// SessionsDir is where the session JSONL mirror is written; empty uses
	// <controller session dir>/sessions.
	SessionsDir string
	// ProjectDir is the kernel project directory the slice store and usage
	// log resolve against (kernel CLI semantics: <dir>/.semantix/...).
	// Empty uses the process working directory.
	ProjectDir string
	// CostMissUSD / CostHitUSD are the usage cost model prices (USD per 1M
	// tokens at cache miss / hit) for the reuse panel savings delta.
	// Zero keeps the kernel defaults (usage.DefaultCost*PerMTok).
	CostMissUSD float64
	CostHitUSD  float64
}

// Bridge aggregates the kernel wiring for one harness build. It is optional:
// a nil Bridge (or one built with Enabled=false) makes the harness run
// without the kernel — every failure path degrades fail-open, never blocking
// the agent main loop.
type Bridge struct {
	cfg    Config
	events *kernelevent.SyncBus

	mu    sync.Mutex
	hs    *HarnessSink // lazily created once a session label is known
	dir   string       // resolved sessions dir ("" = not yet)
	label string       // controller session label (first real session id)
	// lastSavings is the last observed cumulative usage savings, used to
	// attribute the incremental per-turn delta in Reuse.
	lastSavings float64
	evolution   *EvolutionLoop
}

// NewBridge builds a Bridge from cfg.
func NewBridge(cfg Config) *Bridge {
	if cfg.Budget <= 0 {
		cfg.Budget = 4096
	}
	bus := kernelevent.NewSyncBus()
	b := &Bridge{cfg: cfg, events: bus}
	bus.Subscribe(b.mirrorKernel)
	b.evolution = NewEvolutionLoop(bus, evolve.New(evolve.Config{}))
	return b
}

// AttachEvolution connects the live scheduler and prefetcher to the online loop.
func (b *Bridge) AttachEvolution(scheduler, prefetcher EvolutionTuner) {
	if b != nil && b.evolution != nil {
		b.evolution.Attach(scheduler, prefetcher)
	}
}

// InjectResult carries the stable block and the canonical slice identities it
// represents, allowing prefetch feedback to retain the existing targets wire.
type InjectResult struct {
	Text    string
	Targets []string
}

// Events is the in-process kernel event bus shared by the harness and kernel
// services. ResourceCatalog uses it even when the legacy session mirror is off.
func (b *Bridge) Events() kernelevent.Bus {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.events == nil {
		b.events = kernelevent.NewSyncBus()
	}
	return b.events
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

// Inject runs the kernel's L2 injector in-process for query and returns the
// [semantix-reuse] block, or "" when the kernel store is unavailable (soft
// degrade — the harness never blocks on the kernel). Semantics match the
// kernel CLI `semantix inject` defaults (U39 in-process data source).
func (b *Bridge) Inject(ctx context.Context, query string) string {
	return b.InjectDetailed(ctx, query).Text
}

func (b *Bridge) InjectDetailed(ctx context.Context, query string) InjectResult {
	if !b.Enabled() || !b.cfg.Inject {
		return InjectResult{}
	}
	idx, err := b.kernelIndex()
	if err != nil {
		return InjectResult{}
	}
	z := zone.Default()
	inj, err := (&inject.Injector{
		Index:  idx,
		Scope:  slice.Project,
		K:      5,
		Budget: b.cfg.Budget,
		Zones:  &z,
	}).Build(query)
	if err != nil || inj == nil || len(inj.Slices) == 0 {
		return InjectResult{}
	}
	targets := make([]string, 0, len(inj.Slices))
	for _, sl := range inj.Slices {
		if sl != nil {
			targets = append(targets, sl.ID)
		}
	}
	sort.Strings(targets)
	return InjectResult{Text: inj.Text, Targets: targets}
}

// RecordPrefetch emits one terminal outcome for a warmed result.
func (b *Bridge) RecordPrefetch(hit bool, targets []string, turn int) {
	if b == nil || !b.Enabled() || len(targets) == 0 {
		return
	}
	targets = append([]string(nil), targets...)
	sort.Strings(targets)
	kind := kernelevent.PrefetchWaste
	var payload any = kernelevent.PrefetchWastePayload{Targets: targets}
	if hit {
		kind = kernelevent.PrefetchHit
		payload = kernelevent.PrefetchHitPayload{Targets: targets}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	b.mu.Lock()
	session := b.label
	b.mu.Unlock()
	b.events.Emit(kernelevent.Event{Kind: kind, SessionID: session, Turn: turn, At: time.Now().UTC(), Data: data})
}

// Reuse gathers the per-turn reuse panel data (U33/H4a) in-process: the
// project-store hits for query (kernel/lookup semantics, limit 5) plus the
// incremental cost savings since the last usage snapshot. Kernel store
// unavailable degrades to a zero summary — the panel hides and the agent
// main loop never blocks on the kernel.
func (b *Bridge) Reuse(ctx context.Context, query string) ReuseSummary {
	if !b.Enabled() || query == "" {
		return ReuseSummary{}
	}
	idx, err := b.kernelIndex()
	if err != nil {
		return ReuseSummary{}
	}
	hits, err := idx.Search(query, 5, slice.Project)
	if err != nil {
		return ReuseSummary{}
	}
	sessions := make([]string, 0, len(hits))
	for _, h := range hits {
		if h.Slice != nil {
			sessions = append(sessions, h.Slice.Meta.SourceSession)
		}
	}
	sum := ReuseSummary{Hits: len(hits), Sources: topSources(sessions)}
	if s, err := usage.Summarize(b.usagePath(), b.costMiss(), b.costHit()); err == nil {
		b.mu.Lock()
		prev := b.lastSavings
		b.lastSavings = s.SavingsUSD
		b.mu.Unlock()
		if delta := s.SavingsUSD - prev; delta > 0 {
			sum.SavingsUSD = delta
		}
	}
	return sum
}

// kernelIndex opens the project slice store and rebuilds the in-memory index
// covering every scope (kernel CLI lookup/inject parity). Rebuilt per call:
// the store is a small JSONL file and indexing is millisecond-scale; caching
// is deferred to the kernel wiring follow-up (U40).
func (b *Bridge) kernelIndex() (slice.Index, error) {
	store, err := slice.NewFileStore(filepath.Join(b.projectDir(), ".semantix", "project.db"))
	if err != nil {
		return nil, err
	}
	idx := bm25.New()
	for _, scope := range []slice.Scope{slice.Session, slice.Project, slice.User} {
		items, err := store.List(scope)
		if err != nil {
			return nil, err
		}
		for _, sl := range items {
			if err := idx.Insert(sl); err != nil {
				return nil, err
			}
		}
	}
	return idx, nil
}

// projectDir resolves the kernel project directory for in-process store and
// usage-log reads (kernel CLI semantics: <dir>/.semantix/...).
func (b *Bridge) projectDir() string {
	if b.cfg.ProjectDir != "" {
		return b.cfg.ProjectDir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// usagePath is the kernel usage log the reuse panel savings delta reads.
func (b *Bridge) usagePath() string {
	return filepath.Join(b.projectDir(), ".semantix", "usage.jsonl")
}

// costMiss / costHit resolve the usage cost model prices, falling back to the
// kernel defaults when the build did not configure them.
func (b *Bridge) costMiss() float64 {
	if b.cfg.CostMissUSD > 0 {
		return b.cfg.CostMissUSD
	}
	return usage.DefaultCostMissPerMTok
}

func (b *Bridge) costHit() float64 {
	if b.cfg.CostHitUSD > 0 {
		return b.cfg.CostHitUSD
	}
	return usage.DefaultCostHitPerMTok
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
	hs := b.sessionSink()
	if hs == nil {
		return
	}
	hs.Emit(e)
}

func (b *Bridge) mirrorKernel(e kernelevent.Event) {
	if e.Kind != kernelevent.PrefetchHit && e.Kind != kernelevent.PrefetchWaste && e.Kind != kernelevent.EvolutionTick {
		return
	}
	hs := b.sessionSink()
	if hs != nil {
		hs.EmitKernel(e)
	}
}

func (b *Bridge) sessionSink() *HarnessSink {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hs != nil {
		return b.hs
	}
	if b.label == "" {
		return nil
	}
	hs, err := NewHarnessSink(dirOrFallback(b.cfg.SessionsDir), b.label, "")
	if err != nil {
		return nil
	}
	b.hs = hs
	return hs
}

// Close flushes and closes the mirror sink, if created.
func (b *Bridge) Close() error {
	if b.evolution != nil {
		b.evolution.Close()
	}
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
