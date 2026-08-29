package judge

import "context"

// VariantJudge is a Judge that can answer the same question with a
// rephrased rubric — the second perspective of the promotion consensus
// gate (Issue #280, Cannot Self-Correct arXiv:2310.01798: a single
// wording of a self-assessment is structurally weak).
//
// The consensus flow (implemented in kernel/cache L3Decider.judgeGrey):
// the primary judge approves first (one Confirm call), then the second
// perspective — ConfirmSecondary — must approve too before a promotion
// entry is written. Judges that do not implement VariantJudge degrade to
// the single-perspective baseline (consensus=1).
type VariantJudge interface {
	Judge
	// ConfirmSecondary asks the same candidate with the rephrased rubric
	// (same six-dimension checklist, different wording and example order;
	// same single-token yes/no output contract).
	ConfirmSecondary(ctx context.Context, c Candidate) (bool, error)
}
