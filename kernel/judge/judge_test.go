package judge

import (
	"context"
	"errors"
	"testing"

	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

func cand(z zone.Zone) Candidate {
	return Candidate{Query: "q", SliceID: "s", Content: "a", Scope: slice.Project, Type: slice.Prompt, Zone: z}
}

func TestRuleGateZones(t *testing.T) {
	g := RuleGate{}
	if v, _ := g.Check(cand(zone.Hit)); v != Confirm {
		t.Errorf("hit -> %v, want Confirm", v)
	}
	if v, _ := g.Check(cand(zone.Miss)); v != Reject {
		t.Errorf("miss -> %v, want Reject", v)
	}
	if v, _ := g.Check(cand(zone.Grey)); v != Reject {
		t.Errorf("grey (no judge) -> %v, want Reject (conservative)", v)
	}
}

func TestRuleGateGreyNeedsJudge(t *testing.T) {
	g := RuleGate{Judge: NoopJudge{}}
	if v, _ := g.Check(cand(zone.Grey)); v != NeedJudge {
		t.Errorf("grey (with judge) -> %v, want NeedJudge", v)
	}
}

type stubJudge struct{ ok bool }

func (s stubJudge) Confirm(context.Context, Candidate) (bool, error) { return s.ok, nil }

func TestChainGreyNoJudgeRejects(t *testing.T) {
	g := RuleGate{}
	v, reason, err := g.Chain(context.Background(), cand(zone.Grey))
	if err != nil || v != Reject {
		t.Fatalf("chain = %v %q %v, want Reject (conservative, never NeedJudge)", v, reason, err)
	}
	// This reason comes from Check's grey-without-judge arm (Chain returns it
	// via the `v != NeedJudge` early return); Chain itself has no nil-judge
	// branch — Check never emits NeedJudge without a judge. See Issue #245.
	if reason != "grey zone: no judge wired, conservative reject" {
		t.Errorf("reason = %q", reason)
	}
}

func TestChainGreyJudgeApproves(t *testing.T) {
	g := RuleGate{Judge: stubJudge{ok: true}}
	v, reason, err := g.Chain(context.Background(), cand(zone.Grey))
	if err != nil || v != Confirm || reason != "judge approved" {
		t.Fatalf("chain = %v %q %v, want Confirm", v, reason, err)
	}
}

func TestChainGreyJudgeDeclines(t *testing.T) {
	g := RuleGate{Judge: stubJudge{ok: false}}
	v, _, err := g.Chain(context.Background(), cand(zone.Grey))
	if err != nil || v != Reject {
		t.Fatalf("chain = %v %v, want Reject", v, err)
	}
}

func TestChainJudgeErrorConservative(t *testing.T) {
	var st Stats
	g := RuleGate{Judge: errJudge{}, Stats: &st}
	v, reason, err := g.Chain(context.Background(), cand(zone.Grey))
	if err == nil || v != Reject {
		t.Fatalf("chain = %v %v, want Reject with error", v, err)
	}
	// Fail-closed behavior is unchanged; only the accounting moves (Issue #245).
	if reason != "judge error: conservative reject" {
		t.Errorf("reason = %q, want the unchanged fail-closed reason", reason)
	}
	if st.JudgeError != 1 {
		t.Errorf("JudgeError = %d, want 1 (a judge call failure is not a verdict)", st.JudgeError)
	}
	// The regression guard for the #245 conflation: a judge outage must never
	// inflate the rule-rejection counter, nor be read as "judge declined".
	if st.RulesReject != 0 {
		t.Errorf("RulesReject = %d, want 0 (judge error is not a rule rejection)", st.RulesReject)
	}
	if st.JudgeReject != 0 {
		t.Errorf("JudgeReject = %d, want 0 (judge error is not a judge decline)", st.JudgeReject)
	}
}

// TestChainCounterAttribution pins which counter each Chain branch increments.
// Before Issue #245 nothing in this package asserted on Stats at all (RuleGate.count's
// body had zero test coverage), so the counter semantics the whole observability
// chain depends on were only verified indirectly, three layers away in kernel/cache.
func TestChainCounterAttribution(t *testing.T) {
	for _, tc := range []struct {
		name  string
		gate  RuleGate
		zone  zone.Zone
		want  Stats
		wantV Verdict
	}{
		{"clear hit", RuleGate{Judge: stubJudge{ok: true}}, zone.Hit, Stats{Confirmed: 1}, Confirm},
		{"clear miss", RuleGate{Judge: stubJudge{ok: true}}, zone.Miss, Stats{RulesReject: 1}, Reject},
		{"grey no judge", RuleGate{}, zone.Grey, Stats{RulesReject: 1}, Reject},
		{"grey judge approves", RuleGate{Judge: stubJudge{ok: true}}, zone.Grey, Stats{NeedJudge: 1, Confirmed: 1, JudgeApproved: 1}, Confirm},
		{"grey judge declines", RuleGate{Judge: stubJudge{ok: false}}, zone.Grey, Stats{NeedJudge: 1, JudgeReject: 1}, Reject},
		{"grey judge errors", RuleGate{Judge: errJudge{}}, zone.Grey, Stats{NeedJudge: 1, JudgeError: 1}, Reject},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var st Stats
			g := tc.gate
			g.Stats = &st
			v, _, _ := g.Chain(context.Background(), cand(tc.zone))
			if v != tc.wantV {
				t.Errorf("verdict = %v, want %v", v, tc.wantV)
			}
			if st != tc.want {
				t.Errorf("stats = %+v, want %+v", st, tc.want)
			}
		})
	}
}

type errJudge struct{}

func (errJudge) Confirm(context.Context, Candidate) (bool, error) {
	return false, errors.New("model unavailable")
}

func TestChainHitSkipsJudge(t *testing.T) {
	// hit candidates must never reach the judge (cost).
	reached := false
	g := RuleGate{Judge: countingJudge{fn: func() { reached = true }}}
	if v, _, err := g.Chain(context.Background(), cand(zone.Hit)); err != nil || v != Confirm {
		t.Fatalf("chain = %v %v, want Confirm", v, err)
	}
	if reached {
		t.Fatal("hit candidate must not reach the judge")
	}
}

type countingJudge struct{ fn func() }

func (c countingJudge) Confirm(ctx context.Context, cand Candidate) (bool, error) {
	c.fn()
	return true, nil
}
