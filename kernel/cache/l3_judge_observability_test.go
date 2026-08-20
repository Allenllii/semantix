package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"semantix/kernel/slice"
)

// ---- Issue #242 gap 1: grey-zone judge verdicts must be observable ----
//
// Before this, a grey candidate that did not get reused left no durable
// trace distinguishing "the judge declined" from "the judge call failed
// and we fell back to fail-closed" — and the judge's own model call was
// invisible to cost accounting. These tests pin the observation hook that
// makes both answerable.

func collectJudge(t *testing.T, d *L3Decider) *[]JudgeObservation {
	t.Helper()
	got := &[]JudgeObservation{}
	d.OnJudge = func(_ context.Context, o JudgeObservation) { *got = append(*got, o) }
	return got
}

func TestDecideL3JudgeObservationApproved(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: &mockJudge{confirm: true}}
	got := collectJudge(t, d)

	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("judge-approved grey candidate should be reused")
	}
	if len(*got) != 1 {
		t.Fatalf("observations = %d, want exactly 1", len(*got))
	}
	o := (*got)[0]
	if o.SliceID != "l3-1" {
		t.Errorf("SliceID = %q, want l3-1", o.SliceID)
	}
	if o.Verdict != JudgeApproved {
		t.Errorf("Verdict = %q, want %q", o.Verdict, JudgeApproved)
	}
	if !o.Called {
		t.Error("Called = false, want true: an approved verdict means the judge model was billed")
	}
	if o.FailClosed {
		t.Error("FailClosed = true, want false: this was a real verdict")
	}
	if o.RelConfidence <= 0 {
		t.Errorf("RelConfidence = %v, want the score/top1 ratio that put the candidate in grey", o.RelConfidence)
	}
	if o.PromptBytes <= 0 {
		t.Errorf("PromptBytes = %d, want the judge input size estimate (cost accounting)", o.PromptBytes)
	}
	if o.Reason == "" {
		t.Error("Reason is empty, want the RuleGate.Chain reason")
	}
}

func TestDecideL3JudgeObservationDeclined(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: &mockJudge{confirm: false}}
	got := collectJudge(t, d)

	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil || res != nil {
		t.Fatalf("declined grey candidate must not be reused: res=%v err=%v", res, err)
	}
	if len(*got) != 1 {
		t.Fatalf("observations = %d, want exactly 1", len(*got))
	}
	o := (*got)[0]
	if o.Verdict != JudgeDeclined {
		t.Errorf("Verdict = %q, want %q", o.Verdict, JudgeDeclined)
	}
	if !o.Called {
		t.Error("Called = false: a declined verdict still cost one judge call")
	}
	if o.FailClosed {
		t.Error("FailClosed = true, want false: a real 'no' is not a fallback")
	}
	if o.Err != "" {
		t.Errorf("Err = %q, want empty on a real verdict", o.Err)
	}
}

// The distinction the acceptance report could not make: a judge that errors
// or times out must be recorded as fail-closed, never as a verdict.
func TestDecideL3JudgeObservationFailClosedOnError(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(),
		Judge: &mockJudge{confirm: true, err: context.DeadlineExceeded}}
	got := collectJudge(t, d)

	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil || res != nil {
		t.Fatalf("judge error must fail closed: res=%v err=%v", res, err)
	}
	if len(*got) != 1 {
		t.Fatalf("observations = %d, want exactly 1", len(*got))
	}
	o := (*got)[0]
	if o.Verdict != JudgeFailClosed {
		t.Errorf("Verdict = %q, want %q", o.Verdict, JudgeFailClosed)
	}
	if !o.FailClosed {
		t.Error("FailClosed = false, want true")
	}
	if !o.Called {
		t.Error("Called = false: a timed-out call is still an upstream call that may be billed")
	}
	if o.Err == "" {
		t.Error("Err is empty, want the judge error text")
	}
}

// A grey candidate rejected by the fingerprint gate never reaches the judge:
// it must be recorded as skipped (no judge cost), not as a verdict.
func TestDecideL3JudgeObservationSkippedBeforeJudge(t *testing.T) {
	root, idx, dep, _ := buildTestLib(t)
	if err := os.WriteFile(filepath.Join(root, dep), []byte("module demo\nv2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: &mockJudge{confirm: true}}
	got := collectJudge(t, d)

	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil || res != nil {
		t.Fatalf("changed dep must fail closed: res=%v err=%v", res, err)
	}
	if len(*got) != 1 {
		t.Fatalf("observations = %d, want exactly 1", len(*got))
	}
	o := (*got)[0]
	if o.Verdict != JudgeSkipped {
		t.Errorf("Verdict = %q, want %q", o.Verdict, JudgeSkipped)
	}
	if o.Called {
		t.Error("Called = true: the fingerprint gate rejected before the judge, so nothing was billed")
	}
}

// No judge wired: grey rejection is deterministic and free, so it must not
// emit judge observations (they would be pure noise in the usage log).
func TestDecideL3NoJudgeEmitsNoObservation(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones()}
	got := collectJudge(t, d)

	if _, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project}); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 0 {
		t.Fatalf("observations = %d, want 0 when no judge is wired", len(*got))
	}
}
