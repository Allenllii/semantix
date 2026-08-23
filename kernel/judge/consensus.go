package judge

import "context"

// VariantJudge is a Judge that can be asked the same question with a
// rephrased rubric — the second perspective of the consensus gate
// (Issue #280, Cannot Self-Correct arXiv:2310.01798: a single wording of
// a self-assessment is structurally weak).
type VariantJudge interface {
	Judge
	// ConfirmSecondary asks the same candidate with the rephrased rubric
	// (same six-dimension checklist, different wording and example order;
	// same single-token yes/no output contract).
	ConfirmSecondary(ctx context.Context, c Candidate) (bool, error)
}

// consensusJudge wraps a judge so Confirm requires BOTH perspectives:
// primary && secondary. A wrapped judge that does not implement
// VariantJudge (NoopJudge, test stubs) transparently degrades to the
// single-perspective baseline — consensus only applies where a real
// rephrased rubric exists.
type consensusJudge struct {
	inner Judge
}

// Consensus wraps j in the two-perspective gate.
func Consensus(j Judge) Judge {
	return consensusJudge{inner: j}
}

// Confirm is the consensus verdict: primary approved AND secondary
// approved. Either error is an unavailability (caller treats it as a
// fail-closed judge error, Issue #245), not a decline.
func (c consensusJudge) Confirm(ctx context.Context, cand Candidate) (bool, error) {
	vj, ok := c.inner.(VariantJudge)
	if !ok {
		return c.inner.Confirm(ctx, cand)
	}
	primary, err := vj.Confirm(ctx, cand)
	if err != nil || !primary {
		return primary, err
	}
	return vj.ConfirmSecondary(ctx, cand)
}
