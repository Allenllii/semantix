package agent

import (
	"fmt"
	"sync"
	"time"

	kernelevent "semantix/kernel/event"
	"semantix/kernel/sched"
)

// BudgetController accumulates window spend and maps the spent/limit ratio to
// a degrade action (U41 C3): halt_prefetch ≥ 70%, degrade_tier ≥ 90%,
// hard_stop ≥ 100%. The escalation is sticky within a window — it never
// relaxes — and resets only when the window rolls over (day boundary).
//
// It is distinct from runBudget (task-level token/cost/wall cap): this is a
// window-level financial quota with laddered degradation, not a per-task stop.
type BudgetController struct {
	mu        sync.Mutex
	limitUSD  float64
	window    string
	spentUSD  float64
	lastAction string
	dayKey    int // UTC day ordinal; changes trigger a day-window reset
}

// NewBudgetController builds a controller with the given quota. limitUSD <= 0
// disables budgeting (no action ever fires). window is "session" or "day";
// anything else is treated as "session".
func NewBudgetController(limitUSD float64, window string) *BudgetController {
	if window != "day" {
		window = "session"
	}
	return &BudgetController{limitUSD: limitUSD, window: window, dayKey: currentUTCDayKey()}
}

// currentUTCDayKey returns the UTC day ordinal (whole-day boundary).
func currentUTCDayKey() int {
	return int(time.Now().UTC().Unix() / 86400)
}

// Observe folds one billed cost into the window spend, rolling the window
// across a day boundary first. A non-positive cost is ignored (free reads
// must never look like a crossing).
func (b *BudgetController) Observe(costUSD float64) {
	if b == nil || costUSD <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.window == "day" {
		if dk := currentUTCDayKey(); dk != b.dayKey {
			b.dayKey = dk
			b.spentUSD = 0
			b.lastAction = ""
		}
	}
	b.spentUSD += costUSD
}

// Action returns the current degrade action, sticky within the window.
func (b *BudgetController) Action() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limitUSD <= 0 {
		return ""
	}
	a := b.classify()
	if rankOf(a) > rankOf(b.lastAction) {
		b.lastAction = a
	}
	return b.lastAction
}

// Snapshot returns the catalog budget state for the kernel scheduler.
func (b *BudgetController) Snapshot() kernelevent.ResourceBudget {
	if b == nil {
		return kernelevent.ResourceBudget{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return kernelevent.ResourceBudget{LimitUSD: b.limitUSD, SpentUSD: b.spentUSD, Window: b.window}
}

// SpentUSD returns the accumulated spend (test/display seam).
func (b *BudgetController) SpentUSD() float64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spentUSD
}

// classify maps the current ratio to an action without touching lastAction.
func (b *BudgetController) classify() string {
	r := b.spentUSD / b.limitUSD
	switch {
	case r >= 1.0:
		return sched.BudgetActionHardStop
	case r >= 0.9:
		return sched.BudgetActionDegradeTier
	case r >= 0.7:
		return sched.BudgetActionHaltPrefetch
	default:
		return ""
	}
}

// budgetHardStopError returns the user-visible hard_stop message (U41 C3),
// shared by the local-account guard and the kernel RoundPlan path. It is an
// explicit, non-silent error: the harness refuses the tool round and tells
// the user exactly how much was spent against the cap. Without a local
// controller (kernel-only wiring) the spend snapshot is unavailable, so the
// message names the scheduler as the source instead.
func (a *Agent) budgetHardStopError() error {
	if a == nil || a.budgetCtrl == nil {
		return fmt.Errorf("budget exhausted: scheduler issued hard_stop; no further tool calls will be issued")
	}
	snap := a.budgetCtrl.Snapshot()
	return fmt.Errorf("budget exhausted: spent $%.4f of the $%.4f %s limit; no further tool calls will be issued",
		snap.SpentUSD, snap.LimitUSD, snap.Window)
}

// rankOf orders actions by escalation severity (higher = more severe).
func rankOf(action string) int {
	switch action {
	case sched.BudgetActionHardStop:
		return 3
	case sched.BudgetActionDegradeTier:
		return 2
	case sched.BudgetActionHaltPrefetch:
		return 1
	default:
		return 0
	}
}
