package semanticreview

import (
	"strings"
	"testing"
)

func TestBuildEvidenceIsBoundedAndValid(t *testing.T) {
	payload, err := buildEvidence(Evidence{Task: strings.Repeat("任务", 1000), DiffSummary: strings.Repeat("diff", 3000), TestReceipts: []string{strings.Repeat("ok", 500)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > MaxEvidenceBytes {
		t.Fatalf("payload=%d", len(payload))
	}
	if !strings.Contains(payload, "untrusted evidence") {
		t.Fatal("missing evidence boundary")
	}
}

func TestParseVerdict(t *testing.T) {
	v, err := parseVerdict("```json\n{\"outcome\":\"revise\",\"missing_callers\":[\"middleware\"],\"missing_requirements\":[],\"missing_invariants\":[],\"missing_tests\":[],\"reason\":\"caller omitted\"}\n```")
	if err != nil || v.Outcome != "revise" || len(v.MissingCallers) != 1 {
		t.Fatalf("verdict=%+v err=%v", v, err)
	}
}

func TestRejectsUnknownOutcome(t *testing.T) {
	if _, err := parseVerdict(`{"outcome":"maybe"}`); err == nil {
		t.Fatal("expected error")
	}
}

func TestReviewerBudgetStaysBelowV24(t *testing.T) {
	if MaxTokens > 256 || MaxEvidenceBytes > 6*1024 {
		t.Fatalf("review budget grew: tokens=%d evidence=%d", MaxTokens, MaxEvidenceBytes)
	}
}
