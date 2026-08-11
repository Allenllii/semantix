package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunEvalJudgeStubNo(t *testing.T) {
	var out bytes.Buffer
	code := runEvalJudge([]string{"--stub", "no", "--audit", "testdata/judge-audit.tsv", "--min-consistency", "95"}, &out)
	// oracle=approve rows with stub=no → consistency < 95% → exit 3 expected.
	if code != 3 {
		t.Fatalf("stub=no must fail the 95%% consistency gate, got code %d", code)
	}
	if !strings.Contains(out.String(), "consistency_%") {
		t.Fatalf("summary header missing: %s", out.String())
	}
	if !strings.Contains(out.String(), "false_approve_%") {
		t.Fatalf("ε_fa column missing: %s", out.String())
	}
}

func TestRunEvalJudgeStubYes(t *testing.T) {
	var out bytes.Buffer
	code := runEvalJudge([]string{"--stub", "yes", "--audit", "testdata/judge-audit.tsv", "--min-consistency", "95"}, &out)
	// stub=yes approves everything → consistency = oracle-approve share (10/14 ≈ 71%)…
	// wait: consistency counts judge==oracle; approve rows 10 → 71% < 95% → must fail!
	if code != 3 {
		t.Fatalf("stub=yes must fail the 95%% gate (approve share is 71%%), got code %d", code)
	}
}

func TestRunEvalJudgeRejectsBadAudit(t *testing.T) {
	var out bytes.Buffer
	if code := runEvalJudge([]string{"--stub", "yes", "--audit", "testdata/does-not-exist.tsv"}, &out); code != 2 {
		t.Fatalf("missing audit file must return 2, got %d", code)
	}
}

func TestRunEvalJudgeRequiresBackend(t *testing.T) {
	var out bytes.Buffer
	if code := runEvalJudge([]string{"--audit", "testdata/judge-audit.tsv"}, &out); code != 2 {
		t.Fatalf("no stub and no judge config must return 2, got %d", code)
	}
}
