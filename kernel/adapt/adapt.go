// Package adapt implements per-entry online threshold adaptation for the
// L3 reuse path (Issue #259 阶段 3, vCache arXiv:2502.03771 route): each
// high-frequency cached slice keeps its own grey-zone floor (TauLow),
// learned from that slice's positive/negative reuse evidence. The global
// thresholds act as the prior/cold-start; a slice only departs from them
// after enough of its own observations accumulate, and every adjustment
// respects an operator-specified false-hit error bound.
//
// Evidence contract: the consumer reports one observation per served L3
// decision — positive when the reuse was accepted without a retry, negative
// on a suspected false hit (Issue #262 retry heuristic) or a judge
// rejection of that slice. The engine folds observations into a per-entry
// negative-rate EWMA and nudges TauLow in 0.05 steps inside a freeze
// window, mirroring kernel/evolve's tuning discipline (freeze protects the
// vendor byte-prefix cache from churn).
package adapt

import (
	"encoding/json"
	"errors"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Default tuning constants. The band [MinTau, MaxTau] matches
// evolve.DefaultMinTau/MaxTau so the two online layers cannot disagree on
// what a legal tau looks like.
const (
	DefaultErrorBound   = 0.05 // operator-specified false-hit rate ceiling
	DefaultStep         = 0.05 // per adjustment step (aligns evolve.TauStep)
	DefaultMinSamples   = 20   // observations before the first adjustment (aligns evolve.MinSamples)
	DefaultMinHits      = 5    // reuses before an entry's learned tau takes effect
	DefaultFreezeEpochs = 60   // observations an adjustment stays frozen (aligns evolve.DefaultFreezeEpoch)
	DefaultAlpha        = 0.1  // negative-rate EWMA smoothing (aligns evolve.DefaultAlpha)
	DefaultMinTau       = 0.30 // floor for learned tau (never below)
	DefaultMaxTau       = 0.80 // ceiling for learned tau (never above)
)

// Config carries operator-provided tuning constants (zero values →
// defaults). ErrorBound < 0 disables adaptation entirely.
type Config struct {
	ErrorBound   float64
	Step         float64
	MinSamples   uint64
	MinHits      uint64
	FreezeEpochs uint64
	Alpha        float64
	MinTau       float64
	MaxTau       float64
}

// Entry is one cached slice's adaptive state. TauLow == 0 means "not yet
// learned — use the global threshold" (cold start). Relaxed marks that the
// last adjustment loosened the threshold (below the value in effect): the
// generous direction is gated by MinHits reuses, the conservative one
// applies immediately (see Engine.TauLow).
type Entry struct {
	SliceID   string  `json:"slice_id"`
	TauLow    float64 `json:"tau_low"`
	Relaxed   bool    `json:"relaxed"`
	Pos       uint64  `json:"pos"` // positive observations (reuse without retry)
	Neg       uint64  `json:"neg"` // negative observations (suspected false hit / judge reject)
	NegEWMA   float64 `json:"neg_ewma"`
	Frozen    uint64  `json:"frozen"` // remaining frozen observations
	UpdatedAt int64   `json:"updated_at"`
}

// Engine is the per-entry adaptive threshold state machine. All methods
// are safe for concurrent use; the decision path (TauLow) never blocks on
// I/O — persistence happens inside Observe at most every
// persistInterval observations or on an actual adjustment.
type Engine struct {
	mu          sync.Mutex
	cfg         Config
	path        string
	enabled     bool
	epoch       uint64 // total observations seen (all entries)
	adjustments uint64
	entries     map[string]*Entry
}

// persisted is the on-disk shape (append/replace whole file on write).
type persisted struct {
	Entries     []Entry `json:"entries"`
	Epoch       uint64  `json:"epoch"`
	Adjustments uint64  `json:"adjustments"`
}

// persistInterval bounds write frequency: a full file write at most every
// 64 observations outside of adjustments (adjustments always write).
const persistInterval = 64

// New loads (or initializes) the adaptive state at path and returns a
// ready engine. A missing file starts empty (cold start — global
// thresholds apply); a corrupt file is logged and treated as empty too:
// adaptive state is derived performance state, never data truth, so it
// must not block the decision path. ErrorBound < 0 in cfg disables
// adaptation (TauLow always returns the global value).
func New(cfg Config, path string) *Engine {
	e := &Engine{
		cfg: Config{
			ErrorBound:   DefaultErrorBound,
			Step:         DefaultStep,
			MinSamples:   DefaultMinSamples,
			MinHits:      DefaultMinHits,
			FreezeEpochs: DefaultFreezeEpochs,
			Alpha:        DefaultAlpha,
			MinTau:       DefaultMinTau,
			MaxTau:       DefaultMaxTau,
		},
		path:    path,
		enabled: true,
		entries: map[string]*Entry{},
	}
	if cfg.ErrorBound < 0 {
		e.enabled = false
		return e
	}
	if cfg.ErrorBound > 0 {
		e.cfg.ErrorBound = cfg.ErrorBound
	}
	if cfg.Step > 0 {
		e.cfg.Step = cfg.Step
	}
	if cfg.MinSamples > 0 {
		e.cfg.MinSamples = cfg.MinSamples
	}
	if cfg.MinHits > 0 {
		e.cfg.MinHits = cfg.MinHits
	}
	if cfg.FreezeEpochs > 0 {
		e.cfg.FreezeEpochs = cfg.FreezeEpochs
	}
	if cfg.Alpha > 0 && !math.IsNaN(cfg.Alpha) && !math.IsInf(cfg.Alpha, 0) {
		e.cfg.Alpha = cfg.Alpha
	}
	if cfg.MinTau > 0 {
		e.cfg.MinTau = cfg.MinTau
	}
	if cfg.MaxTau > 0 {
		e.cfg.MaxTau = cfg.MaxTau
	}
	if e.cfg.MinTau >= e.cfg.MaxTau {
		log.Printf("adapt: MinTau (%.2f) >= MaxTau (%.2f), clamping MaxTau to MinTau+0.05", e.cfg.MinTau, e.cfg.MaxTau)
		e.cfg.MaxTau = e.cfg.MinTau + 0.05
	}
	e.load()
	return e
}

// load reads the persisted state; corrupt or unreadable files are logged
// and skipped (derived state, cold start is safe).
func (e *Engine) load() {
	if e.path == "" {
		return
	}
	b, err := os.ReadFile(e.path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("adapt: read state %s: %v (starting cold)", e.path, err)
		return
	}
	var p persisted
	if err := json.Unmarshal(b, &p); err != nil {
		log.Printf("adapt: parse state %s: %v (starting cold)", e.path, err)
		return
	}
	for i := range p.Entries {
		ent := &p.Entries[i]
		if ent.SliceID == "" {
			continue
		}
		if math.IsNaN(ent.NegEWMA) || math.IsInf(ent.NegEWMA, 0) {
			ent.NegEWMA = 0
		}
		e.entries[ent.SliceID] = ent
	}
	e.epoch = p.Epoch
	e.adjustments = p.Adjustments
}

// Observe folds one decision observation for a slice into its adaptive
// state and, once the entry has enough samples and its freeze window is
// open, nudges the entry's TauLow toward the configured error bound.
// curTau is the tau_low currently in effect for this slice (the global
// value or the entry's learned value) — it seeds the first adjustment.
// A disabled engine is a no-op.
func (e *Engine) Observe(sliceID string, negative bool, curTau float64) {
	if sliceID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.enabled {
		return
	}
	e.epoch++
	ent := e.entries[sliceID]
	if ent == nil {
		ent = &Entry{SliceID: sliceID}
		e.entries[sliceID] = ent
	}
	if negative {
		ent.Neg++
		ent.NegEWMA = e.cfg.Alpha*1 + (1-e.cfg.Alpha)*ent.NegEWMA
	} else {
		ent.Pos++
		ent.NegEWMA = (1 - e.cfg.Alpha) * ent.NegEWMA
	}
	if ent.Frozen > 0 {
		// Frozen observation: fold the count but do not adjust.
		ent.Frozen--
		if e.epoch%persistInterval == 0 {
			e.persistLocked()
		}
		return
	}
	samples := ent.Pos + ent.Neg
	write := false
	if samples >= e.cfg.MinSamples {
		base := ent.TauLow
		if base <= 0 {
			base = curTau // first adjustment seeds from the value in effect
		}
		if base <= 0 {
			base = e.cfg.MinTau // defensive: never adjust from an empty prior
		}
		old := base
		tighter := false
		switch {
		case ent.NegEWMA > e.cfg.ErrorBound:
			base += e.cfg.Step // tighten: fewer direct reuses
			tighter = true
		case ent.NegEWMA < e.cfg.ErrorBound/2 && ent.Pos >= e.cfg.MinSamples:
			base -= e.cfg.Step // relax: reuse is safe, be more generous
		}
		if base < e.cfg.MinTau {
			base = e.cfg.MinTau
		}
		if base > e.cfg.MaxTau {
			base = e.cfg.MaxTau
		}
		if base != old {
			ent.TauLow = base
			if !tighter {
				ent.Relaxed = true // loosening is gated by MinHits
			}
			ent.Frozen = e.cfg.FreezeEpochs
			e.adjustments++
			write = true
		}
	}
	if write || e.epoch%persistInterval == 0 {
		e.persistLocked()
	}
}

// TauLow returns the effective tau_low for a slice: the entry's learned
// value once it exists and is learned (TauLow > 0). A learned value that
// TIGHTENED (raised) the threshold applies immediately — negative
// evidence must never be gated behind traffic volume. A learned value
// that RELAXED (lowered) it only applies after the entry accumulated
// MinHits reuses (trust gate for low-traffic slices). Cold start and
// disabled engines return the global value.
func (e *Engine) TauLow(sliceID string, global float64) float64 {
	if sliceID == "" {
		return global
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.enabled {
		return global
	}
	ent := e.entries[sliceID]
	if ent == nil || ent.TauLow <= 0 {
		return global
	}
	if ent.Relaxed && ent.Pos < e.cfg.MinHits {
		return global
	}
	return ent.TauLow
}

// Snapshot returns a defensive, SliceID-sorted copy of every entry
// (observability: calibrate reports).
func (e *Engine) Snapshot() []Entry {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Entry, 0, len(e.entries))
	for _, ent := range e.entries {
		out = append(out, *ent)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SliceID < out[j].SliceID })
	return out
}

// Adjustments reports how many times any entry's tau actually changed.
func (e *Engine) Adjustments() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.adjustments
}

// Epoch returns the total observation count (all entries).
func (e *Engine) Epoch() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.epoch
}

// persistLocked writes the state file atomically (temp + rename). Callers
// hold e.mu. A failed write is logged and ignored — adaptive state is
// derived and rebuildable; it must never break the decision path.
func (e *Engine) persistLocked() {
	if e.path == "" {
		return
	}
	p := persisted{
		Entries:     make([]Entry, 0, len(e.entries)),
		Epoch:       e.epoch,
		Adjustments: e.adjustments,
	}
	for _, ent := range e.entries {
		p.Entries = append(p.Entries, *ent)
	}
	sort.Slice(p.Entries, func(i, j int) bool { return p.Entries[i].SliceID < p.Entries[j].SliceID })
	b, err := json.Marshal(p)
	if err != nil {
		log.Printf("adapt: marshal state: %v", err)
		return
	}
	tmp := e.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("adapt: write state %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, e.path); err != nil {
		log.Printf("adapt: commit state %s: %v", e.path, err)
	}
}

// DefaultAdaptDB returns the default state file path next to a slice
// store: <store-dir>/l3-adapt.json.
func DefaultAdaptDB(storeDB string) string {
	return filepath.Join(filepath.Dir(storeDB), "l3-adapt.json")
}
