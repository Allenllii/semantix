package main

import (
	"bytes"
	"encoding/json"
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
	if code := runEvalJudge([]string{"--stub", "yes", "--audit", "testdata/does-not-exist.tsv"}, &out); code != 1 {
		t.Fatalf("missing audit file is a runtime/IO error, must return 1, got %d", code)
	}
}

func TestRunEvalJudgeRequiresBackend(t *testing.T) {
	var out bytes.Buffer
	if code := runEvalJudge([]string{"--audit", "testdata/judge-audit.tsv"}, &out); code != 2 {
		t.Fatalf("no stub and no judge config must return 2, got %d", code)
	}
}

func TestRunEvalJudgeStubError(t *testing.T) {
	var out bytes.Buffer
	// --stub error: every Confirm fails → verdict "error" rows, zero
	// approvals; --min-consistency 0 makes the gate pass.
	code := runEvalJudge([]string{"--stub", "error", "--audit", "testdata/judge-audit.tsv", "--min-consistency", "0"}, &out)
	if code != 0 {
		t.Fatalf("stub=error with --min-consistency 0 must pass, got code %d", code)
	}
	if !strings.Contains(out.String(), "judge=error") {
		t.Fatalf("stub=error must produce verdict=error rows:\n%s", out.String())
	}
}

func TestRunEvalJudgeRejectsBadStub(t *testing.T) {
	var out bytes.Buffer
	// An unknown --stub mode is a usage mistake: it must not silently
	// fall back to "no" (which would mask a CI typo).
	if code := runEvalJudge([]string{"--stub", "bogus", "--audit", "testdata/judge-audit.tsv"}, &out); code != 2 {
		t.Fatalf("unknown --stub mode must be a usage error (2), got %d", code)
	}
}

func TestRunEvalJudgeRejectsNaNThresholds(t *testing.T) {
	for _, args := range [][]string{
		{"--stub", "yes", "--audit", "testdata/judge-audit.tsv", "--min-consistency", "NaN"},
		{"--stub", "yes", "--audit", "testdata/judge-audit.tsv", "--min-consistency", "Inf"},
		{"--stub", "yes", "--audit", "testdata/judge-audit.tsv", "--p-prom", "NaN"},
		{"--stub", "yes", "--audit", "testdata/judge-audit.tsv", "--p-prom", "-0.5"},
		{"--stub", "yes", "--audit", "testdata/judge-audit.tsv", "--min-consistency", "101"},
	} {
		var out bytes.Buffer
		if code := runEvalJudge(args, &out); code != 2 {
			t.Errorf("runEvalJudge(%v) = %d, want usage error 2", args, code)
		}
	}
}

// TestEvalJudgeJSONEnvelopePass: eval-judge --json on the passing path emits
// the §4.2 envelope with the authenticity metrics in data (U35 acceptance).
func TestEvalJudgeJSONEnvelopePass(t *testing.T) {
	var out bytes.Buffer
	code := runEvalJudge([]string{"--stub", "error", "--audit", "testdata/judge-audit.tsv",
		"--min-consistency", "0", "--json"}, &out)
	if code != 0 {
		t.Fatalf("stub=error --min-consistency 0 must pass, got code %d; out:\n%s", code, out.String())
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("envelope not JSON-parseable: %v\n%s", err, out.String())
	}
	if !env.OK || env.Command != "eval-judge" || env.Error != nil {
		t.Fatalf("envelope = ok:%v command:%q error:%+v, want ok eval-judge", env.OK, env.Command, env.Error)
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	var data evalJudgeData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("data not parseable: %v\n%s", err, raw)
	}
	if data.Pairs != 14 || !data.Pass {
		t.Fatalf("data = %+v, want pairs=14 pass=true", data)
	}
	if data.Consistency < 0 || data.FalseApprove < 0 || data.DeltaUpper < 0 {
		t.Fatalf("metrics must be non-negative: %+v", data)
	}
}

// TestEvalJudgeJSONGateFail: the consistency gate (exit 3) must still emit a
// parseable ok:false envelope with error.code=3 — the CI JSON consumer always
// gets valid JSON (U35 acceptance).
func TestEvalJudgeJSONGateFail(t *testing.T) {
	var out bytes.Buffer
	code := runEvalJudge([]string{"--stub", "no", "--audit", "testdata/judge-audit.tsv",
		"--min-consistency", "95", "--json"}, &out)
	if code != 3 {
		t.Fatalf("stub=no must fail the 95%% gate with code 3, got %d", code)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("gate-failure envelope not JSON-parseable: %v\n%s", err, out.String())
	}
	if env.OK || env.Error == nil || env.Error.Code != 3 {
		t.Fatalf("envelope = ok:%v error:%+v, want ok:false error.code=3", env.OK, env.Error)
	}
	if env.Error.Message == "" {
		t.Fatalf("error message must not be empty: %+v", env.Error)
	}
}
