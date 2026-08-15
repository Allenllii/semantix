package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestEvalComparesPolicies: the oracle evaluation set must run end-to-end
// and produce both policy rows with reuse/error rates; the three-region
// verdict must be PASS (error rate not worse than the single-threshold
// baseline on the same set, Issue #7 acceptance).
func TestEvalComparesPolicies(t *testing.T) {
	set := filepath.Join("testdata", "eval-greyzone.tsv")
	var out bytes.Buffer
	if code := runEval([]string{"--set", set}, &out); code != 0 {
		t.Fatalf("runEval code = %d, want 0; out:\n%s", code, out.String())
	}
	text := out.String()
	for _, want := range []string{
		"policy\treuse\terror\treuse_rate\terror_rate",
		"single\t5\t1\t100.0%\t20.0%",
		"three\t3\t0\t60.0%\t0.0%",
		"# verdict: PASS",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

// TestEvalAlarm: with a tight grey target the WARN line fires; --strict
// turns it into exit code 3 (CI gate), --grey-target 0 disables it.
func TestEvalAlarm(t *testing.T) {
	set := filepath.Join("testdata", "eval-greyzone.tsv")
	var out bytes.Buffer
	if code := runEval([]string{"--set", set, "--grey-target", "10"}, &out); code != 0 {
		t.Fatalf("code = %d, want 0 (WARN but non-strict); out:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "WARN: grey_ratio") {
		t.Fatalf("missing WARN line:\n%s", out.String())
	}

	out.Reset()
	if code := runEval([]string{"--set", set, "--grey-target", "10", "--strict"}, &out); code != 3 {
		t.Fatalf("strict code = %d, want 3; out:\n%s", code, out.String())
	}

	out.Reset()
	if code := runEval([]string{"--set", set, "--grey-target", "0", "--strict"}, &out); code != 0 {
		t.Fatalf("disabled alarm code = %d, want 0; out:\n%s", code, out.String())
	}
	if strings.Contains(out.String(), "WARN") {
		t.Fatalf("alarm should be disabled with --grey-target 0:\n%s", out.String())
	}
}

func TestEvalRejectsBadSet(t *testing.T) {
	var out bytes.Buffer
	if code := runEval([]string{"--set", "does-not-exist.tsv"}, &out); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if code := runEval(nil, &out); code != 2 {
		t.Fatalf("no --set code = %d, want 2", code)
	}
}

func TestReadEvalSet(t *testing.T) {
	rows, err := readEvalSet(filepath.Join("testdata", "eval-greyzone.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 15 {
		t.Fatalf("rows = %d, want 15", len(rows))
	}
	if rows[0].class != "fix-test" || rows[0].query != "修复 go 测试失败" {
		t.Fatalf("first row = %+v", rows[0])
	}
}

// TestEvalRejectsNaNInFlags: NaN/Inf thresholds must be usage errors, not
// silently pass the range check (NaN <= 0 and NaN >= 1 are both false).
func TestEvalRejectsNaNInFlags(t *testing.T) {
	set := filepath.Join("testdata", "eval-greyzone.tsv")
	for _, frac := range []string{"NaN", "Inf", "-Inf"} {
		var out bytes.Buffer
		if code := runEval([]string{"--set", set, "--train-frac", frac}, &out); code != 2 {
			t.Errorf("--train-frac %s: code = %d, want usage error 2", frac, code)
		}
	}
}

// TestEvalJSONEnvelope: eval --json emits the §4.2 envelope with the policy
// comparison, zone distribution, and verdict in data (U35 acceptance).
func TestEvalJSONEnvelope(t *testing.T) {
	set := filepath.Join("testdata", "eval-greyzone.tsv")
	var out bytes.Buffer
	if code := runEval([]string{"--set", set, "--json"}, &out); code != 0 {
		t.Fatalf("runEval --json code = %d, want 0; out:\n%s", code, out.String())
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("envelope not JSON-parseable: %v\n%s", err, out.String())
	}
	if !env.OK || env.Command != "eval" || env.Error != nil {
		t.Fatalf("envelope = ok:%v command:%q error:%+v, want ok eval", env.OK, env.Command, env.Error)
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	var data evalData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("data not parseable: %v\n%s", err, raw)
	}
	if data.Rows != 15 || data.Replay != 5 {
		t.Fatalf("data = %+v, want rows=15 replay=5", data)
	}
	if len(data.Policies) != 2 || data.Policies[0].Name != "single" || data.Policies[1].Name != "three" {
		t.Fatalf("policies = %+v, want [single three]", data.Policies)
	}
	if data.Policies[1].Reuse != 3 || data.Policies[1].Error != 0 {
		t.Fatalf("three policy = %+v, want reuse=3 error=0", data.Policies[1])
	}
	if data.Verdict != "PASS" {
		t.Fatalf("verdict = %q, want PASS", data.Verdict)
	}
	if data.Zones.Hit+data.Zones.Grey+data.Zones.Miss != data.Replay {
		t.Fatalf("zones %+v do not sum to replay %d", data.Zones, data.Replay)
	}
}

// TestEvalJSONStrictGate: --strict with a tight grey target exits 3 and the
// --json consumer still gets a parseable ok:false envelope with error.code=3.
func TestEvalJSONStrictGate(t *testing.T) {
	set := filepath.Join("testdata", "eval-greyzone.tsv")
	var out bytes.Buffer
	if code := runEval([]string{"--set", set, "--grey-target", "10", "--strict", "--json"}, &out); code != 3 {
		t.Fatalf("strict --json code = %d, want 3; out:\n%s", code, out.String())
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("gate-failure envelope not JSON-parseable: %v\n%s", err, out.String())
	}
	if env.OK || env.Error == nil || env.Error.Code != 3 {
		t.Fatalf("envelope = ok:%v error:%+v, want ok:false error.code=3", env.OK, env.Error)
	}
}
