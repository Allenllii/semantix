package agent

import (
	"strings"
	"testing"

	"semantix/kernel/sched"
)

func TestBudgetControllerClassifiesThresholds(t *testing.T) {
	b := NewBudgetController(100, "session")
	cases := []struct {
		spend float64
		want  string
	}{
		{0, ""},
		{69.99, ""},
		{70, sched.BudgetActionHaltPrefetch}, // ≥70% includes boundary
		{89.99, sched.BudgetActionHaltPrefetch},
		{90, sched.BudgetActionDegradeTier}, // ≥90% includes boundary
		{99.99, sched.BudgetActionDegradeTier},
		{100, sched.BudgetActionHardStop}, // ≥100% includes boundary
		{110, sched.BudgetActionHardStop},
	}
	for _, c := range cases {
		b = NewBudgetController(100, "session")
		b.Observe(c.spend)
		if got := b.Action(); got != c.want {
			t.Errorf("spend=%v → action %q, want %q", c.spend, got, c.want)
		}
	}
}

func TestBudgetControllerStickyEscalation(t *testing.T) {
	b := NewBudgetController(100, "session")
	b.Observe(100) // hard_stop
	if got := b.Action(); got != sched.BudgetActionHardStop {
		t.Fatalf("action = %q, want hard_stop", got)
	}
	// Even if a later read were lower, the action never relaxes within the window.
	if got := b.Action(); got != sched.BudgetActionHardStop {
		t.Errorf("action regressed to %q", got)
	}
}

func TestBudgetControllerUnlimited(t *testing.T) {
	b := NewBudgetController(0, "session") // 0 = unlimited
	b.Observe(999999)
	if got := b.Action(); got != "" {
		t.Errorf("unlimited action = %q, want empty", got)
	}
	neg := NewBudgetController(-5, "session")
	neg.Observe(999999)
	if got := neg.Action(); got != "" {
		t.Errorf("negative-limit action = %q, want empty", got)
	}
}

func TestBudgetControllerIgnoresNonPositive(t *testing.T) {
	b := NewBudgetController(100, "session")
	b.Observe(0)
	b.Observe(-10)
	if got := b.SpentUSD(); got != 0 {
		t.Errorf("spentUSD = %v, want 0", got)
	}
}

func TestBudgetControllerSnapshot(t *testing.T) {
	b := NewBudgetController(100, "day")
	b.Observe(30)
	snap := b.Snapshot()
	if snap.LimitUSD != 100 || snap.SpentUSD != 30 || snap.Window != "day" {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestBudgetControllerWindowDefaultsToSession(t *testing.T) {
	b := NewBudgetController(100, "")
	if b.window != "session" {
		t.Errorf("window = %q, want session", b.window)
	}
	b2 := NewBudgetController(100, "bogus")
	if b2.window != "session" {
		t.Errorf("window = %q, want session", b2.window)
	}
}

func TestBudgetHardStopError(t *testing.T) {
	a := &Agent{budgetCtrl: NewBudgetController(100, "session")}
	a.budgetCtrl.Observe(120)
	err := a.budgetHardStopError()
	if err == nil {
		t.Fatal("expected a hard-stop error")
	}
	if !containsAny(err.Error(), "budget exhausted", "120", "100") {
		t.Errorf("error %q should name the spend and limit", err.Error())
	}
}

func TestBudgetHardStopErrorNoController(t *testing.T) {
	if err := (&Agent{}).budgetHardStopError(); err == nil {
		t.Fatal("expected a fallback error without a controller")
	}
	if err := (&Agent{}).budgetHardStopError(); err.Error() != "budget exhausted" {
		t.Errorf("fallback error = %q", err.Error())
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
