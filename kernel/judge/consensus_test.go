package judge

import (
	"context"
	"errors"
	"testing"
)

// stubVariant is a controllable VariantJudge recording both perspectives.
type stubVariant struct {
	primary   bool
	secondary bool
	primaryCalls,
	secondaryCalls int
	err error
}

func (s *stubVariant) Confirm(_ context.Context, _ Candidate) (bool, error) {
	s.primaryCalls++
	if s.err != nil {
		return false, s.err
	}
	return s.primary, nil
}

func (s *stubVariant) ConfirmSecondary(_ context.Context, _ Candidate) (bool, error) {
	s.secondaryCalls++
	if s.err != nil {
		return false, s.err
	}
	return s.secondary, nil
}

var _ VariantJudge = (*stubVariant)(nil)

// Both perspectives must approve for the consensus gate.
func TestConsensusRequiresBothPerspectives(t *testing.T) {
	cases := []struct {
		name      string
		primary   bool
		secondary bool
		want      bool
	}{
		{"both approve", true, true, true},
		{"primary rejects", false, true, false},
		{"secondary rejects", true, false, false},
		{"both reject", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &stubVariant{primary: c.primary, secondary: c.secondary}
			got, err := Consensus(s).Confirm(context.Background(), Candidate{})
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("consensus = %v, want %v", got, c.want)
			}
		})
	}
}

// The secondary perspective is skipped when the primary already rejects
// (no wasted second call).
func TestConsensusShortCircuitsOnPrimaryReject(t *testing.T) {
	s := &stubVariant{primary: false, secondary: true}
	if got, err := Consensus(s).Confirm(context.Background(), Candidate{}); err != nil || got {
		t.Fatalf("consensus = %v %v, want false", got, err)
	}
	if s.secondaryCalls != 0 {
		t.Fatalf("secondary calls = %d, want 0 (short circuit)", s.secondaryCalls)
	}
}

// A judge error is unavailability (Issue #245), propagated as-is — the
// caller fails closed.
func TestConsensusPropagatesError(t *testing.T) {
	s := &stubVariant{primary: true, err: errors.New("boom")}
	if _, err := Consensus(s).Confirm(context.Background(), Candidate{}); err == nil {
		t.Fatal("error must propagate")
	}
}

// Non-VariantJudge judges (NoopJudge, plain stubs) degrade transparently
// to the single-perspective baseline.
func TestConsensusDegradesForPlainJudge(t *testing.T) {
	calls := 0
	plain := JudgeFunc(func(_ context.Context, _ Candidate) (bool, error) {
		calls++
		return true, nil
	})
	if got, err := Consensus(plain).Confirm(context.Background(), Candidate{}); err != nil || !got {
		t.Fatalf("degraded consensus = %v %v, want true", got, err)
	}
	if calls != 1 {
		t.Fatalf("plain judge calls = %d, want 1", calls)
	}
}

// JudgeFunc adapts a func to the Judge interface (test helper).
type JudgeFunc func(ctx context.Context, c Candidate) (bool, error)

func (f JudgeFunc) Confirm(ctx context.Context, c Candidate) (bool, error) { return f(ctx, c) }

var _ Judge = JudgeFunc(nil)

// The rephrased rubric keeps the same output contract (single token
// yes/no) while the wording differs from the primary.
func TestSecondaryRubricDiffersFromPrimary(t *testing.T) {
	c := Candidate{Query: "q", SliceID: "s", Content: "c"}
	if rubricPrompt(c) == secondaryRubricPrompt(c) {
		t.Fatal("secondary rubric must differ from primary (wording variance)")
	}
	for _, p := range []string{rubricPrompt(c), secondaryRubricPrompt(c)} {
		if !containsYesNoContract(p) {
			t.Fatalf("rubric missing yes/no contract:\n%s", p)
		}
	}
}

func containsYesNoContract(p string) bool {
	return len(p) > 0 && (contains(p, "yes") && contains(p, "no"))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
