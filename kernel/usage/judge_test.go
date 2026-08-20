package usage

import (
	"math"
	"path/filepath"
	"testing"
)

// Issue #242 gap 1: the grey-zone judge is a separate upstream model call
// that never went through the New API channel and never produced a usage
// event, so its cost could not be accounted for at all. It must now be
// countable straight out of the usage log.

func TestSummarizeAggregatesJudgeDecisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	r, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	evs := []Event{
		{SessionID: "s1", Turn: 1, TokensIn: 100, TokensOut: 20, JudgeDecisions: []JudgeDecision{
			{SliceID: "a", RelConfidence: 0.675, Verdict: "approved", Called: true, LatencyMs: 800, PromptTokens: 400},
		}},
		{SessionID: "s1", Turn: 2, TokensIn: 100, TokensOut: 20, JudgeDecisions: []JudgeDecision{
			{SliceID: "b", RelConfidence: 0.61, Verdict: "declined", Called: true, LatencyMs: 700, PromptTokens: 300},
			{SliceID: "c", RelConfidence: 0.58, Verdict: "fail_closed", Called: true, FailClosed: true,
				Error: "context deadline exceeded", LatencyMs: 30000, PromptTokens: 300},
		}},
		{SessionID: "s2", Turn: 1, TokensIn: 100, TokensOut: 20, JudgeDecisions: []JudgeDecision{
			{SliceID: "d", RelConfidence: 0.7, Verdict: "skipped", PromptTokens: 250},
		}},
		{SessionID: "s2", Turn: 2, TokensIn: 100, TokensOut: 20}, // no judge at all
	}
	for _, e := range evs {
		if err := r.Append(e); err != nil {
			t.Fatal(err)
		}
	}

	s, err := Summarize(path, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if s.JudgeDecisions != 4 {
		t.Errorf("JudgeDecisions = %d, want 4", s.JudgeDecisions)
	}
	// Only calls that actually reached the judge are billable; the skipped
	// one was rejected by the fingerprint gate and cost nothing.
	if s.JudgeCalls != 3 {
		t.Errorf("JudgeCalls = %d, want 3 (skipped is free)", s.JudgeCalls)
	}
	if s.JudgeApproved != 1 || s.JudgeDeclined != 1 || s.JudgeFailClosed != 1 || s.JudgeSkipped != 1 {
		t.Errorf("verdict split = %d/%d/%d/%d, want 1/1/1/1 approved/declined/fail_closed/skipped",
			s.JudgeApproved, s.JudgeDeclined, s.JudgeFailClosed, s.JudgeSkipped)
	}
	// Skipped decisions never sent a prompt, so their bytes must not be billed.
	if s.JudgePromptTokens != 1000 {
		t.Errorf("JudgePromptTokens = %d, want 1000 (400+300+300, skipped excluded)", s.JudgePromptTokens)
	}
	if s.JudgeLatencyMs != 31500 {
		t.Errorf("JudgeLatencyMs = %d, want 31500", s.JudgeLatencyMs)
	}

	// The whole point: judge cost is now computable at the judge model's
	// own price, independent of the served model's price.
	got := s.JudgeCostUSD(0.27)
	want := 1000 * 0.27 / 1e6
	if math.Abs(got-want) > 1e-12 {
		t.Errorf("JudgeCostUSD(0.27) = %v, want %v", got, want)
	}
}

// The judge fields must not disturb the existing savings arithmetic: the
// GW4 report's headline rate is computed from these same numbers.
func TestJudgeDecisionsDoNotChangeSavings(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.jsonl")
	judged := filepath.Join(dir, "judged.jsonl")

	base := Event{SessionID: "s", Turn: 1, TokensIn: 1000, TokensOut: 200, CacheHitToken: 600}
	rp, err := NewRecorder(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := rp.Append(base); err != nil {
		t.Fatal(err)
	}
	rj, err := NewRecorder(judged)
	if err != nil {
		t.Fatal(err)
	}
	withJudge := base
	withJudge.JudgeDecisions = []JudgeDecision{
		{SliceID: "a", Verdict: "declined", Called: true, PromptTokens: 5000},
	}
	if err := rj.Append(withJudge); err != nil {
		t.Fatal(err)
	}

	a, err := Summarize(plain, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Summarize(judged, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if a.SavingsUSD != b.SavingsUSD || a.SavingsRate != b.SavingsRate || a.CostPaidUSD != b.CostPaidUSD {
		t.Fatalf("judge fields changed the served-model arithmetic: %+v vs %+v", a, b)
	}
	if b.JudgeCalls != 1 {
		t.Fatalf("JudgeCalls = %d, want 1", b.JudgeCalls)
	}
}
