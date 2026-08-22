// Package sched implements the M3 MVP scheduler (N04): one tool round's
// resource orchestration — parallel groups, model tier, injection list, and
// prefetch hints — decided by deterministic rules plus a light behavior-
// learning overlay.
//
// Design (see 总体架构-流程树.md §N04 and Agent-Infra-架构设计.md §P3):
//   - static base: contiguous known read-only tools form parallel groups,
//     writers/unknowns/blacklisted tools run serially (provider order kept);
//   - behavior learning: a read-only tool whose recent success history is
//     below the floor is treated as unsafe to parallelize (candidates must
//     pass the behavior gate, per N04 "候选须过静态数据依赖检查");
//   - tier: the frozen turn intent is considered before writer presence and
//     round size; every decision carries a stable explanation;
//   - injection list: SliceHits ids in canonical (ID-ascending) order, so the
//     byte-stable prefix-cache invariant is preserved.
//
// The Decider interface itself (DecideRound) is frozen since U1; this file
// adds the concrete implementation.
package sched

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"semantix/kernel/slice"
)

// ApplyEvolution installs the online behavior-gate threshold used by the next
// DecideRound call. Static scheduler configuration is left unchanged.
func (d *RuleDecider) ApplyEvolution(successFloor float64) error {
	if math.IsNaN(successFloor) || math.IsInf(successFloor, 0) {
		return errors.New("sched: non-finite evolution threshold")
	}
	if successFloor < 0 {
		successFloor = 0
	}
	if successFloor > 1 {
		successFloor = 1
	}
	d.mu.Lock()
	d.cfg.SuccessFloor = successFloor
	d.mu.Unlock()
	return nil
}

// SerialToolNames are the built-in tools that never join a parallel group
// (kept in sync with the semantix harness partitionToolCalls blacklist:
// complete_step, todo_write, wait, bash_output, use_capability, compress).
var SerialToolNames = map[string]struct{}{
	"complete_step":  {},
	"todo_write":     {},
	"wait":           {},
	"bash_output":    {},
	"use_capability": {},
	"compress":       {},
}

// PrefetchPlanFunc optionally maps the current round's tool names to
// prefetch target keys (tool names). The kernel prefetch package plugs in
// here so RoundPlan.PrefetchIDs stays populated without sched importing
// prefetch (keeps the dependency direction sched → nothing).
type PrefetchPlanFunc func(lastToolNames []string) []string

// PrefetchPlanResult is the load-aware callback's candidate list and stable
// explanation. IDs are still reduced to tool-name keys by the adapter.
type PrefetchPlanResult struct {
	IDs    []string
	Reason string
}

// LoadAwarePrefetchPlanFunc is an additive callback seam; PrefetchPlanFunc is
// retained for existing integrations.
type LoadAwarePrefetchPlanFunc func(lastToolNames []string, hint PrefetchLoadHint) PrefetchPlanResult

// Config carries operator knobs; zero values select documented defaults.
type Config struct {
	// MaxParallel caps how many read-only tools may run in one parallel
	// group (default 8, mirrors the harness runParallel semaphore).
	MaxParallel int
	// MinSamples is the behavior-statistics sample count a tool needs
	// before the success gate applies (default 5).
	MinSamples int
	// SuccessFloor: a read-only tool whose success rate is below this
	// (with >= MinSamples samples) is pulled out of parallel groups
	// (default 0.7).
	SuccessFloor float64
	// ComplexTools: a round with more calls than this is scheduled to the
	// pro tier (default 3).
	ComplexTools int
	// DefaultTier names the cheap tier (default "flash").
	DefaultTier string
	// ProTier names the strong tier (default "pro").
	ProTier string
	// InjectMax caps the InjectIDs list length (default 8).
	InjectMax int
	// SerialTools adds extra tool names that never parallelize, on top of
	// the built-in SerialToolNames set.
	SerialTools []string
}

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{
		MaxParallel:  8,
		MinSamples:   5,
		SuccessFloor: 0.7,
		ComplexTools: 3,
		DefaultTier:  "flash",
		ProTier:      "pro",
		InjectMax:    8,
	}
}

// toolStat is one tool's recent behavior history (all access under
// RuleDecider.mu).
type toolStat struct {
	runs     int
	failures int
	durMs    int64
}

// successRate returns the tool's observed success rate; 1 for no history.
func (t *toolStat) successRate() float64 {
	if t == nil || t.runs == 0 {
		return 1
	}
	return float64(t.runs-t.failures) / float64(t.runs)
}

// RuleDecider is the concrete M3 Decider. Safe for concurrent use: each
// DecideRound call is independent and Observe only mutates stats under the
// lock. It is a plain struct (no interface split) so the harness can hold
// it directly; it satisfies the frozen Decider interface.
type RuleDecider struct {
	mu             sync.Mutex
	cfg            Config
	stats          map[string]*toolStat
	prefetchFn     PrefetchPlanFunc
	loadPrefetchFn LoadAwarePrefetchPlanFunc
}

// NewRuleDecider builds a RuleDecider with cfg (zero values → Defaults).
func NewRuleDecider(cfg Config) *RuleDecider {
	def := Defaults()
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = def.MaxParallel
	}
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = def.MinSamples
	}
	if cfg.SuccessFloor <= 0 || cfg.SuccessFloor > 1 {
		cfg.SuccessFloor = def.SuccessFloor
	}
	if cfg.ComplexTools <= 0 {
		cfg.ComplexTools = def.ComplexTools
	}
	if cfg.DefaultTier == "" {
		cfg.DefaultTier = def.DefaultTier
	}
	if cfg.ProTier == "" {
		cfg.ProTier = def.ProTier
	}
	if cfg.InjectMax <= 0 {
		cfg.InjectMax = def.InjectMax
	}
	return &RuleDecider{
		cfg:   cfg,
		stats: make(map[string]*toolStat),
	}
}

// SetPrefetchPlanFunc wires the optional prefetch planner (nil disables).
func (d *RuleDecider) SetPrefetchPlanFunc(fn PrefetchPlanFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prefetchFn = fn
}

// SetLoadAwarePrefetchPlanFunc installs the preferred load-aware planner.
// When set, it takes precedence over the legacy PrefetchPlanFunc.
func (d *RuleDecider) SetLoadAwarePrefetchPlanFunc(fn LoadAwarePrefetchPlanFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loadPrefetchFn = fn
}

// DecideRound implements the frozen Decider interface.
func (d *RuleDecider) DecideRound(ctx context.Context, in RoundInput) (RoundPlan, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	suspended := canonicalNames(in.SuspendedTools)
	active := withoutSuspended(in.ToolCalls, suspended)
	action := decideBudgetAction(in.Budget)
	tier, tierReason := decideTier(in.Intent, active, d.cfg)
	groups := d.partition(active)
	plan := RoundPlan{
		ParallelGroups: groups,
		Tier:           tier,
		TierReason:     tierReason,
		InjectIDs:      canonicalInjectIDs(in.SliceHits, d.cfg.InjectMax),
		SuspendTools:   suspended,
		MaxParallel:    d.cfg.MaxParallel,
		BudgetAction:   action,
	}
	if d.loadPrefetchFn != nil {
		hint := normalizedPrefetchLoad(in.PrefetchLoad, groups, d.cfg.MaxParallel)
		result := d.loadPrefetchFn(toolNames(active), hint)
		plan.PrefetchIDs = result.IDs
		plan.PrefetchReason = result.Reason
	} else if d.prefetchFn != nil {
		plan.PrefetchIDs = d.prefetchFn(toolNames(active))
	}
	if action == BudgetActionHaltPrefetch || action == BudgetActionHardStop {
		plan.PrefetchIDs = nil
		plan.PrefetchReason = "budget:" + action
	}
	if action == BudgetActionDegradeTier {
		plan.Tier = d.cfg.DefaultTier
		plan.TierReason = "budget:" + BudgetActionDegradeTier
	}
	return plan, nil
}

func normalizedPrefetchLoad(hint PrefetchLoadHint, groups [][]string, maxParallel int) PrefetchLoadHint {
	if hint.ConcurrencyLimit <= 0 {
		hint.ConcurrencyLimit = maxParallel
	}
	if hint.ConcurrencyUsed <= 0 {
		for _, group := range groups {
			if len(group) > hint.ConcurrencyUsed {
				hint.ConcurrencyUsed = len(group)
			}
		}
	}
	if hint.ConcurrencyUsed > hint.ConcurrencyLimit && hint.ConcurrencyLimit > 0 {
		hint.ConcurrencyUsed = hint.ConcurrencyLimit
	}
	return hint
}

func canonicalNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func withoutSuspended(calls []ToolCallInfo, suspended []string) []ToolCallInfo {
	if len(suspended) == 0 {
		return calls
	}
	blocked := make(map[string]struct{}, len(suspended))
	for _, name := range suspended {
		blocked[name] = struct{}{}
	}
	out := make([]ToolCallInfo, 0, len(calls))
	for _, call := range calls {
		if _, ok := blocked[call.Name]; !ok {
			out = append(out, call)
		}
	}
	return out
}

func decideBudgetAction(b BudgetState) string {
	if b.LimitUSD <= 0 || b.SpentUSD < 0 {
		return ""
	}
	ratio := b.SpentUSD / b.LimitUSD
	switch {
	case ratio >= 1:
		return BudgetActionHardStop
	case ratio >= 0.9:
		return BudgetActionDegradeTier
	case ratio >= 0.8:
		// Issue #270 step 2: shrink injection before dropping it. The tier
		// stays on the default — only the injector budget is halved at the
		// execution point.
		return BudgetActionDegradeInject
	case ratio >= 0.7:
		return BudgetActionHaltPrefetch
	default:
		return ""
	}
}

// Observe feeds one completed tool round back into the behavior statistics.
// Every executed tool call should be reported once; unexecuted (blocked/
// cancelled) calls should be reported with success=false when the failure
// is attributed to the tool itself. Duration is milliseconds.
func (d *RuleDecider) Observe(name string, success bool, durMs int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.stats[name]
	if st == nil {
		st = &toolStat{}
		d.stats[name] = st
	}
	st.runs++
	if !success {
		st.failures++
	}
	st.durMs += durMs
}

// ToolStatSnapshot is the exported, immutable view of one tool's history.
type ToolStatSnapshot struct {
	Runs        int
	Failures    int
	DurationMs  int64
	SuccessRate float64
}

// Stats returns a snapshot of the observed per-tool statistics (for
// observability / the cost dashboard).
func (d *RuleDecider) Stats() map[string]ToolStatSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]ToolStatSnapshot, len(d.stats))
	for name, st := range d.stats {
		out[name] = ToolStatSnapshot{
			Runs:        st.runs,
			Failures:    st.failures,
			DurationMs:  st.durMs,
			SuccessRate: st.successRate(),
		}
	}
	return out
}

// serialNames merges built-in and configured serial tools into one set.
func (d *RuleDecider) serialNames() map[string]struct{} {
	if len(d.cfg.SerialTools) == 0 {
		return SerialToolNames
	}
	out := make(map[string]struct{}, len(SerialToolNames)+len(d.cfg.SerialTools))
	for n := range SerialToolNames {
		out[n] = struct{}{}
	}
	for _, n := range d.cfg.SerialTools {
		out[n] = struct{}{}
	}
	return out
}

// partition splits calls into parallel groups (contiguous read-only calls
// that pass both the static blacklist and the behavior gate) and serial
// singles, preserving provider order. A read-only tool whose behavior
// history fails the success gate is pulled into its own serial slot —
// conservative: an unreliable read may still be re-run safely, but must not
// run concurrently with siblings that could depend on it. Called with d.mu
// held.
func (d *RuleDecider) partition(calls []ToolCallInfo) [][]string {
	serial := d.serialNames()
	var groups [][]string
	for i := 0; i < len(calls); {
		if d.parallelisable(calls[i], serial) {
			start := i
			i++
			for i < len(calls) && d.parallelisable(calls[i], serial) {
				i++
			}
			// Sub-split oversized groups under the cap.
			cap := d.cfg.MaxParallel
			for s := start; s < i; s += cap {
				end := s + cap
				if end > i {
					end = i
				}
				groups = append(groups, ids(calls[s:end]))
			}
			continue
		}
		groups = append(groups, []string{calls[i].CallID})
		i++
	}
	return groups
}

// parallelisable reports whether call may join a parallel group: known
// read-only, not blacklisted, and passing the behavior gate. Called with
// d.mu held.
func (d *RuleDecider) parallelisable(c ToolCallInfo, serial map[string]struct{}) bool {
	if !c.ReadOnly {
		return false
	}
	if _, black := serial[c.Name]; black {
		return false
	}
	return d.passesBehaviorGate(c.Name)
}

// passesBehaviorGate applies the behavior-learning overlay: a tool with
// enough history whose success rate falls below the floor runs serially.
// Unknown tools (no history) pass by default. Called with d.mu held.
func (d *RuleDecider) passesBehaviorGate(name string) bool {
	st := d.stats[name]
	if st == nil || st.runs < d.cfg.MinSamples {
		return true // no evidence yet: default to the static rule
	}
	return st.successRate() >= d.cfg.SuccessFloor
}

// decideTier maps the turn's frozen intent and current round to a model tier.
// The first matching rule wins so TierReason remains stable and auditable.
func decideTier(intent string, calls []ToolCallInfo, cfg Config) (string, string) {
	intent = strings.ToLower(strings.TrimSpace(intent))
	if intent == "mutation" || intent == "persistent_action" {
		return cfg.ProTier, "intent:" + intent
	}
	for _, c := range calls {
		if !c.ReadOnly {
			return cfg.ProTier, "writer:" + c.Name
		}
	}
	if len(calls) > cfg.ComplexTools {
		return cfg.ProTier, fmt.Sprintf("complex:%d", len(calls))
	}
	return cfg.DefaultTier, "default"
}

// canonicalInjectIDs returns hit slice ids in canonical (ID-ascending)
// order, capped at InjectMax. Canonical order keeps the injected block
// byte-stable across rounds — never score order (inject §design invariants).
func canonicalInjectIDs(hits []slice.Hit, max int) []string {
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		if h.Slice != nil {
			ids = append(ids, h.Slice.ID)
		}
	}
	sort.Strings(ids)
	if max > 0 && len(ids) > max {
		ids = ids[:max]
	}
	return ids
}

// toolNames returns the call names in round order (used for prefetch).
func toolNames(calls []ToolCallInfo) []string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	return names
}

// ids extracts CallIDs preserving order.
func ids(calls []ToolCallInfo) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.CallID
	}
	return out
}

// stripPrefix normalizes a tool name for stats lookup (lower-case, trimmed).
func stripPrefix(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
