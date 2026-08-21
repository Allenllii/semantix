package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/usage"
)

// writeAudit writes a small oracle sample and returns its path.
func writeAudit(t *testing.T, rows []string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "audit.tsv")
	content := "# query\tcached_answer\toracle\n" + strings.Join(rows, "\n") + "\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// 3 approve + 3 reject oracle rows; stub=yes approves everything:
// TP=3 FP=3 TN=0 FN=0 → consistency 50%, ε_fa 50%, ε_fr 0%,
// precision 0.5, recall 1.0, F1 2/3.
func TestCalibrateConfusionMatrix(t *testing.T) {
	audit := writeAudit(t, []string{
		"q1\tanswer1\tapprove",
		"q2\tanswer2\tapprove",
		"q3\tanswer3\tapprove",
		"q4\tanswer4\treject",
		"q5\tanswer5\treject",
		"q6\tanswer6\treject",
	})
	var out bytes.Buffer
	code := runCalibrate([]string{"--audit", audit, "--stub", "yes", "--min-consistency", "0"}, &out)
	if code != 0 {
		t.Fatalf("code = %d, want 0; out:\n%s", code, out.String())
	}
	o := out.String()
	if !strings.Contains(o, "confusion: tp=3 fp=3 tn=0 fn=0") {
		t.Errorf("confusion row missing/wrong:\n%s", o)
	}
	if !strings.Contains(o, "50.0\t50.0\t0.0\t0.500\t1.000\t0.667") {
		t.Errorf("metrics row missing/wrong:\n%s", o)
	}
}

func TestCalibrateGateExitThree(t *testing.T) {
	audit := writeAudit(t, []string{
		"q1\ta1\tapprove",
		"q2\ta2\tapprove",
		"q3\ta3\tapprove",
		"q4\ta4\treject",
	})
	var out bytes.Buffer
	// stub=no rejects everything → consistency = reject share = 25% < 95%.
	code := runCalibrate([]string{"--audit", audit, "--stub", "no", "--min-consistency", "95"}, &out)
	if code != 3 {
		t.Fatalf("consistency below gate must exit 3, got %d; out:\n%s", code, out.String())
	}
}

// TestCalibrateRuntimeOnly
func newUsageLogPath(t *testing.T) string {
	return filepath.Join(t.TempDir(), "usage.jsonl")
}

func TestCalibrateRuntimeOnly(t *testing.T) {
	ul := newUsageLogPath(t)
	writeUsageLog(t, ul, []usage.Event{
		{SessionID: "s1", Turn: 1, TokensIn: 100, L3Reuse: true},
		{SessionID: "s2", Turn: 1, TokensIn: 200, L3FalseHit: true},
	})
	var out bytes.Buffer
	code := runCalibrate([]string{"--usage", ul}, &out)
	if code != 0 {
		t.Fatalf("runtime-only must pass (no gate), got %d", code)
	}
	o := out.String()
	if !strings.Contains(o, "# runtime (usage log)") {
		t.Errorf("runtime block header missing:\n%s", o)
	}
	// l3_reuses=1, false_hits=1 → rate 100.0 (1/1).
	if !strings.Contains(o, "1\t0\t0\t0\t0\t0\t0\t1\t100.0") {
		t.Errorf("runtime row missing/wrong:\n%s", o)
	}
}

func TestCalibrateUsageMissingIsNA(t *testing.T) {
	var out bytes.Buffer
	code := runCalibrate([]string{"--usage", filepath.Join(t.TempDir(), "no-such.jsonl")}, &out)
	if code != 0 {
		t.Fatalf("missing usage log must degrade to N/A, got %d", code)
	}
	if !strings.Contains(out.String(), "N/A") {
		t.Errorf("missing usage log must render N/A:\n%s", out.String())
	}
}

func TestCalibrateRequiresInput(t *testing.T) {
	var out bytes.Buffer
	if code := runCalibrate(nil, &out); code != 2 {
		t.Fatalf("no --audit and no --usage must exit 2, got %d", code)
	}
}

func TestCalibrateRejectsBadAudit(t *testing.T) {
	var out bytes.Buffer
	if code := runCalibrate([]string{"--audit", "testdata/does-not-exist.tsv"}, &out); code != 1 {
		t.Fatalf("missing audit file must exit 1, got %d", code)
	}
	bad := writeAudit(t, []string{"only-two-columns\tapprove"})
	if code := runCalibrate([]string{"--audit", bad, "--stub", "yes"}, &out); code != 2 {
		t.Fatalf("malformed audit line must exit 2, got %d", code)
	}
}

func TestCalibrateJSONEnvelope(t *testing.T) {
	audit := writeAudit(t, []string{
		"q1\ta1\tapprove",
		"q2\ta2\tapprove",
		"q3\ta3\treject",
	})
	ul := newUsageLogPath(t)
	writeUsageLog(t, ul, []usage.Event{
		{SessionID: "s1", Turn: 1, L3Reuse: true},
		{SessionID: "s2", Turn: 1, L3FalseHit: true},
	})
	var out bytes.Buffer
	code := runCalibrate([]string{"--audit", audit, "--usage", ul, "--stub", "yes",
		"--min-consistency", "0", "--json"}, &out)
	if code != 0 {
		t.Fatalf("code = %d, want 0; out:\n%s", code, out.String())
	}
	var env struct {
		OK      bool          `json:"ok"`
		Command string        `json:"command"`
		Data    calibrateData `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\n%s", err, out.String())
	}
	if !env.OK || env.Command != "calibrate" {
		t.Fatalf("envelope = %+v", env)
	}
	if env.Data.Audit == nil || env.Data.Runtime == nil {
		t.Fatal("both audit and runtime blocks must be present and separate")
	}
	if env.Data.Audit.Confusion.TP != 2 || env.Data.Audit.Confusion.FP != 1 {
		t.Fatalf("audit confusion = %+v, want tp=2 fp=1", env.Data.Audit.Confusion)
	}
	if env.Data.Runtime.L3Reuses != 1 || env.Data.Runtime.L3FalseHits != 1 {
		t.Fatalf("runtime = %+v, want reuses=1 false_hits=1", env.Data.Runtime)
	}
	if env.Data.Runtime.L3FalseHitRate == nil || *env.Data.Runtime.L3FalseHitRate != 100 {
		t.Fatalf("runtime false-hit rate = %v, want 100", env.Data.Runtime.L3FalseHitRate)
	}
}

func TestCalibrateJSONGateEnvelope(t *testing.T) {
	audit := writeAudit(t, []string{"q1\ta1\tapprove"})
	var out bytes.Buffer
	code := runCalibrate([]string{"--audit", audit, "--stub", "no", "--min-consistency", "95", "--json"}, &out)
	if code != 3 {
		t.Fatalf("gate failure must exit 3, got %d", code)
	}
	var env struct {
		OK      bool   `json:"ok"`
		Error   *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal error envelope: %v", err)
	}
	if env.OK || env.Error == nil || env.Error.Code != 3 {
		t.Fatalf("error envelope = %+v, want ok=false error.code=3", env)
	}
}
