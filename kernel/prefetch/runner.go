package prefetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"semantix/kernel/slice"
)

// Executor runs one read-only prefetch task and returns the content bytes to
// be persisted as a Result slice. The kernel stays decoupled from any
// concrete tool: an adapter provides its own Executor for harness tools;
// SliceAssembler is the built-in slice-library-backed one (spec §2.5).
type Executor interface {
	Execute(ctx context.Context, t PrefetchTask) ([]byte, error)
}

// Runner executes a plan's tasks serially and persists successful results as
// Result slices in the store. Tasks are low-priority background work: serial
// execution keeps them from competing with real tool calls, and the per-task
// duration bound is the caller's ctx (e.g. remaining stream-wait time).
type Runner struct {
	// Store receives the persisted Result slices (required).
	Store slice.Store
	// Executor performs each task (required).
	Executor Executor
	// Scope is the slice scope for stored results; the zero value (Session)
	// is treated as Project, since prefetch results are meant for reuse
	// across sessions.
	Scope slice.Scope

	// BlockedEgress counts tasks rejected for crossing a process boundary
	// (Issue #273): egress or undeclared-locality tasks are never executed.
	BlockedEgress atomic.Int64
}

// Run executes tasks in order and stores each successful result. It returns
// the stored slice IDs; one failing task does not abort the rest, and any
// failures are joined into the returned error.
func (r *Runner) Run(ctx context.Context, tasks []PrefetchTask) ([]string, error) {
	if r.Store == nil || r.Executor == nil {
		return nil, errors.New("prefetch: Runner requires Store and Executor")
	}
	scope := r.Scope
	if scope == 0 {
		scope = slice.Project
	}
	var ids []string
	var errs []error
	for _, t := range tasks {
		// Issue #273 red line: only explicitly-local tasks run. Egress (or
		// undeclared) tasks are rejected — a speculative external call leaks
		// inferred user intent to an observer before any commit, and no
		// read-only/ACL discipline can unwatch it.
		if t.Locality != LocalityLocal {
			r.BlockedEgress.Add(1)
			continue
		}
		content, err := r.Executor.Execute(ctx, t)
		if err != nil {
			errs = append(errs, fmt.Errorf("prefetch: execute %s %q: %w", t.Kind, t.Key, err))
			continue
		}
		sl := &slice.Slice{
			Type:    slice.Result,
			Scope:   scope,
			Content: content,
			Weight:  1.0,
			Meta:    slice.SliceMeta{SourceSession: "prefetch:" + t.Key},
		}
		sl.ID = resultSliceID(content, scope)
		if err := r.Store.Put(sl); err != nil {
			errs = append(errs, fmt.Errorf("prefetch: store result for %q: %w", t.Key, err))
			continue
		}
		ids = append(ids, sl.ID)
	}
	return ids, errors.Join(errs...)
}

// SliceAssembler is the default read-only executor (spec §2.5): it retrieves
// candidate slices for the task's key (a predicted tool name) and assembles
// their content so the next tool round can reuse them without a search.
type SliceAssembler struct {
	// Index provides candidate-slice retrieval (required).
	Index slice.Index
	// K is the number of candidates per task; <=0 uses 3.
	K int
}

// Execute searches the project-scoped index for t.Key and returns the
// top-K slices' content, assembled in canonical (ID-sorted) order for
// byte-stable output (DeepSeek prefix-cache friendly, mirroring kernel/inject).
func (a *SliceAssembler) Execute(ctx context.Context, t PrefetchTask) ([]byte, error) {
	if a.Index == nil {
		return nil, errors.New("prefetch: SliceAssembler requires Index")
	}
	k := a.K
	if k <= 0 {
		k = 3
	}
	hits, err := a.Index.Search(t.Key, k, slice.Project)
	if err != nil {
		return nil, fmt.Errorf("slice-assembly %q: search: %w", t.Key, err)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Slice.ID < hits[j].Slice.ID })
	var b strings.Builder
	for _, h := range hits {
		fmt.Fprintf(&b, "--- slice %s ---\n%s\n", h.Slice.ID, h.Slice.Content)
	}
	return []byte(b.String()), nil
}

// resultSliceID mirrors kernel/slice's deterministic content-derived ID
// (sha256 of type|scope|content, first 8 bytes, hex). kernel/slice does not
// export its sliceID, and this file is the only other place a Result slice
// is constructed, so the 5-line algorithm is duplicated here with a
// cross-reference; if kernel/slice ever exports it, call that instead.
// Keeping the algorithm identical is what makes the "same content does not
// re-store" dedup property hold across both constructors.
func resultSliceID(content []byte, scope slice.Scope) string {
	h := sha256.New()
	h.Write([]byte{byte(slice.Result), byte(scope)})
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil)[:8])
}
