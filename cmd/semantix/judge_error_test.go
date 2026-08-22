package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/judge"
	"semantix/kernel/usage"
)

// Issue #245: a judge that could not be reached is unavailability, not a rule
// rejection and not a decline. These tests pin the three CLI surfaces that
// publish the counter, none of which had any judge assertion before.

func writeJudgeErrorLog(t *testing.T) string {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "usage.jsonl")
	r, err := usage.NewRecorder(logPath)
	if err != nil {
		t.Fatal(err)
	}
	ev := usage.Event{
		SessionID: "s1", Turn: 1, TokensIn: 100,
		L3GreyCandidates: 3, L3JudgeReject: 1, L3JudgeError: 2, L3RulesReject: 4,
	}
	if err := r.Append(ev); err != nil {
		t.Fatal(err)
	}
	return logPath
}

func TestRunUsagePrintsJudgeError(t *testing.T) {
	logPath := writeJudgeErrorLog(t)
	var out bytes.Buffer
	if code := runUsage([]string{"--db", logPath}, &out, productionDependencies()); code != 0 {
		t.Fatalf("usage exit code = %d, want 0", code)
	}
	o := out.String()
	if !strings.Contains(o, "l3_judge_error\t2") {
		t.Errorf("l3_judge_error not printed:\n%s", o)
	}
	// The regression guard for the conflation: judge errors must not be folded
	// back into the rule-rejection counter on the way out.
	if !strings.Contains(o, "l3_rules_reject\t4") {
		t.Errorf("l3_rules_reject must stay 4, judge errors are counted separately:\n%s", o)
	}
	if !strings.Contains(o, "l3_judge_reject\t1") {
		t.Errorf("l3_judge_reject must stay 1:\n%s", o)
	}
}

func TestRunUsageJSONCarriesJudgeError(t *testing.T) {
	logPath := writeJudgeErrorLog(t)
	var out bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--json"}, &out, productionDependencies()); code != 0 {
		t.Fatalf("usage --json exit code = %d, want 0", code)
	}
	var env struct {
		Data struct {
			L3JudgeError  int `json:"l3_judge_error"`
			L3JudgeReject int `json:"l3_judge_reject"`
			L3RulesReject int `json:"l3_rules_reject"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("--json output not parseable: %v\n%s", err, out.String())
	}
	if env.Data.L3JudgeError != 2 || env.Data.L3JudgeReject != 1 || env.Data.L3RulesReject != 4 {
		t.Fatalf("data = %+v, want judge_error=2 judge_reject=1 rules_reject=4", env.Data)
	}
}

func TestCalibrateRuntimeCarriesJudgeError(t *testing.T) {
	logPath := writeJudgeErrorLog(t)
	var out bytes.Buffer
	if code := runCalibrate([]string{"--usage", logPath, "--json"}, &out); code != 0 {
		t.Fatalf("calibrate --json exit code = %d, want 0", code)
	}
	var env struct {
		Data struct {
			Runtime struct {
				L3JudgeError  int `json:"l3_judge_error"`
				L3RulesReject int `json:"l3_rules_reject"`
			} `json:"runtime"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("--json output not parseable: %v\n%s", err, out.String())
	}
	if env.Data.Runtime.L3JudgeError != 2 {
		t.Fatalf("runtime.l3_judge_error = %d, want 2", env.Data.Runtime.L3JudgeError)
	}
	if env.Data.Runtime.L3RulesReject != 4 {
		t.Fatalf("runtime.l3_rules_reject = %d, want 4 (unchanged by the split)", env.Data.Runtime.L3RulesReject)
	}
}

// TestVerifyJudgeJSONKeysAreSnakeCase pins the verify --json judge block key
// set for the first time. judge.Stats has no json tags, so marshaling it
// directly leaked PascalCase keys into an otherwise snake_case envelope;
// verifyJudgeJSON is the CLI-local DTO that fixes it.
func TestVerifyJudgeJSONKeysAreSnakeCase(t *testing.T) {
	b, err := json.Marshal(newVerifyJudgeJSON(judgeStatsFixture()))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"confirmed", "rules_reject", "fingerprint",
		"judge_reject", "judge_error", "judge_approved", "need_judge",
	}
	if len(got) != len(want) {
		t.Fatalf("judge block keys = %v, want exactly %v", keysOf(got), want)
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %q in judge block: %v", k, keysOf(got))
		}
	}
	if got["judge_error"] != float64(7) {
		t.Errorf("judge_error = %v, want 7", got["judge_error"])
	}
}

func judgeStatsFixture() judge.Stats {
	return judge.Stats{Confirmed: 1, RulesReject: 2, Fingerprint: 3, JudgeReject: 5, JudgeError: 7, JudgeApproved: 11, NeedJudge: 13}
}

func keysOf(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
