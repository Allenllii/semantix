package agent

import (
	"testing"
)

func effortPtr(s string) *string { return &s }

// TestEffectiveEffortOverridePrecedence pins the contract for the single
// Request.EffortOverride slot, which the governor owned outright before a
// session level existed. Every cell fixes the governor's state explicitly:
// two producers write the same field, so an assertion that leaves the governor
// unpinned is flaky by construction rather than by accident.
//
// The rule under test: an explicit session level wins whenever one is set —
// including an explicit "auto", which asks for the provider's configured depth
// and therefore also suppresses the governor. The governor applies only when
// the user has never touched the dial.
func TestEffectiveEffortOverridePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		engaged  bool
		session  *string
		want     string
		wantWhy  string
	}{
		{
			name: "governor idle, no session level", engaged: false, session: nil,
			want: "", wantWhy: "nothing overrides the configured depth",
		},
		{
			name: "governor idle, explicit auto", engaged: false, session: effortPtr(""),
			want: "", wantWhy: "auto means the configured depth",
		},
		{
			name: "governor idle, session depth", engaged: false, session: effortPtr("high"),
			want: "high", wantWhy: "the only producer",
		},
		{
			name: "governor engaged, no session level", engaged: true, session: nil,
			want: governorEffort, wantWhy: "the governor keeps its full effect on an untouched dial",
		},
		{
			name: "governor engaged, explicit auto", engaged: true, session: effortPtr(""),
			want: "", wantWhy: "an explicit auto is a user decision and outranks the experiment",
		},
		{
			name: "governor engaged, session depth", engaged: true, session: effortPtr("max"),
			want: "max", wantWhy: "an explicit level outranks the experiment",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := governorEnabled
			governorEnabled = true
			t.Cleanup(func() { governorEnabled = old })

			a := &Agent{}
			a.task.governor.engaged = tc.engaged
			a.SetSessionEffort(tc.session)

			if got := a.effectiveEffortOverride(); got != tc.want {
				t.Errorf("effectiveEffortOverride = %q, want %q (%s)", got, tc.want, tc.wantWhy)
			}
		})
	}
}

// TestSessionEffortRoundTripsThreeStates: "never set" and "explicitly auto"
// must stay distinguishable. config.NormalizeEffort maps auto to the empty
// string, so a bare-string box would collapse them — and the user could never
// take the dial back from the governor once the experiment was on.
func TestSessionEffortRoundTripsThreeStates(t *testing.T) {
	a := &Agent{}

	if level, ok := a.SessionEffort(); ok || level != "" {
		t.Fatalf("fresh agent = (%q, %v), want unset", level, ok)
	}

	a.SetSessionEffort(effortPtr("high"))
	if level, ok := a.SessionEffort(); !ok || level != "high" {
		t.Fatalf("after set = (%q, %v), want (high, true)", level, ok)
	}

	a.SetSessionEffort(effortPtr(""))
	if level, ok := a.SessionEffort(); !ok || level != "" {
		t.Fatalf("after explicit auto = (%q, %v), want (\"\", true) — set, and set to auto", level, ok)
	}

	a.SetSessionEffort(nil)
	if level, ok := a.SessionEffort(); ok || level != "" {
		t.Fatalf("after clear = (%q, %v), want unset", level, ok)
	}
}

// TestGovernorResumesAfterSessionLevelCleared: clearing is not the same as
// setting auto — it hands the slot back to the governor.
func TestGovernorResumesAfterSessionLevelCleared(t *testing.T) {
	old := governorEnabled
	governorEnabled = true
	t.Cleanup(func() { governorEnabled = old })

	a := &Agent{}
	a.task.governor.engaged = true

	a.SetSessionEffort(effortPtr("low"))
	if got := a.effectiveEffortOverride(); got != "low" {
		t.Fatalf("with session level = %q, want low", got)
	}
	a.SetSessionEffort(nil)
	if got := a.effectiveEffortOverride(); got != governorEffort {
		t.Fatalf("after clear = %q, want the governor's %q back", got, governorEffort)
	}
}
