package sched

import (
	"testing"
)

// Research plan W5: the complexity tier rule becomes feedback-driven once a
// tier has enough complex-shape evidence, while intent/writer safety floors
// and the insufficient-evidence fallback stay on the hard rules.
func TestTierLearnerDemotesComplexWhenCheapTierHolds(t *testing.T) {
	d := NewRuleDecider(Config{})
	// Before evidence: hard rule keeps pro for a 5-call read-only round.
	calls := make([]ToolCallInfo, 5)
	for i := range calls {
		calls[i] = ToolCallInfo{Name: "grep", ReadOnly: true}
	}
	plan, err := d.DecideRound(t.Context(), RoundInput{Intent: "exploration", ToolCalls: calls})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tier != "pro" || plan.TierReason != "complex:5" {
		t.Fatalf("cold complex round = %s (%s), want pro (complex:5)", plan.Tier, plan.TierReason)
	}
	// Feed the cheap tier enough successful, cheap complex rounds.
	for i := 0; i < 12; i++ {
		d.ObserveTier(TierObservation{Tier: "flash", Intent: "exploration", Calls: 5, Success: true, Tokens: 900})
		d.ObserveTier(TierObservation{Tier: "pro", Intent: "exploration", Calls: 5, Success: true, Tokens: 4000})
	}
	plan, err = d.DecideRound(t.Context(), RoundInput{Intent: "exploration", ToolCalls: calls})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tier != "flash" || plan.TierReason != "learned:complex-cheap-ok" {
		t.Fatalf("learned round = %s (%s), want flash (learned:complex-cheap-ok)", plan.Tier, plan.TierReason)
	}
	st := d.TierLearnedStats()
	if st.LearnedUse != 1 || st.Fallbacks != 1 {
		t.Fatalf("learnedUse=%d fallbacks=%d, want 1/1", st.LearnedUse, st.Fallbacks)
	}
}

func TestTierLearnerKeepsProWhenCheapTierFails(t *testing.T) {
	d := NewRuleDecider(Config{})
	for i := 0; i < 12; i++ {
		d.ObserveTier(TierObservation{Tier: "flash", Intent: "exploration", Calls: 5, Success: false, Tokens: 900})
		d.ObserveTier(TierObservation{Tier: "pro", Intent: "exploration", Calls: 5, Success: true, Tokens: 4000})
	}
	calls := make([]ToolCallInfo, 5)
	for i := range calls {
		calls[i] = ToolCallInfo{Name: "grep", ReadOnly: true}
	}
	plan, err := d.DecideRound(t.Context(), RoundInput{Intent: "exploration", ToolCalls: calls})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tier != "pro" || plan.TierReason != "learned:complex-needs-pro" {
		t.Fatalf("failing cheap tier = %s (%s), want pro (learned:complex-needs-pro)", plan.Tier, plan.TierReason)
	}
}

func TestTierSafetyFloorsAreNeverLearned(t *testing.T) {
	d := NewRuleDecider(Config{})
	for i := 0; i < 30; i++ {
		d.ObserveTier(TierObservation{Tier: "flash", Intent: "exploration", Calls: 5, Success: true, Tokens: 900})
	}
	writer := []ToolCallInfo{{Name: "edit_file", ReadOnly: false}, {Name: "grep", ReadOnly: true}, {Name: "grep", ReadOnly: true}, {Name: "grep", ReadOnly: true}, {Name: "grep", ReadOnly: true}}
	plan, err := d.DecideRound(t.Context(), RoundInput{Intent: "exploration", ToolCalls: writer})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tier != "pro" || plan.TierReason != "writer:edit_file" {
		t.Fatalf("writer floor = %s (%s), want pro (writer:edit_file)", plan.Tier, plan.TierReason)
	}
	plan, err = d.DecideRound(t.Context(), RoundInput{Intent: "persistent_action", ToolCalls: writer[1:]})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tier != "pro" || plan.TierReason != "intent:persistent_action" {
		t.Fatalf("intent floor = %s (%s), want pro (intent:persistent_action)", plan.Tier, plan.TierReason)
	}
	// A small (non-complex) round stays on the hard default even with
	// abundant cheap-tier evidence.
	small := []ToolCallInfo{{Name: "grep", ReadOnly: true}, {Name: "grep", ReadOnly: true}}
	plan, err = d.DecideRound(t.Context(), RoundInput{Intent: "exploration", ToolCalls: small})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tier != "flash" || plan.TierReason != "default" {
		t.Fatalf("small round = %s (%s), want flash (default)", plan.Tier, plan.TierReason)
	}
}
