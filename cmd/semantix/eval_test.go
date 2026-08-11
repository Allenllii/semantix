package main

import (
	"bytes"
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
