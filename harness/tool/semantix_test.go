package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSemantixLookupSchemaAndName(t *testing.T) {
	tl := semantixLookup{}
	if tl.Name() != "semantix_lookup" {
		t.Errorf("name: %s", tl.Name())
	}
	if !tl.ReadOnly() {
		t.Error("must be read-only")
	}
	var s map[string]any
	if err := json.Unmarshal(tl.Schema(), &s); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if s["type"] != "object" {
		t.Errorf("schema type: %v", s["type"])
	}
}

// TestSemantixLookupBudget pins the production subprocess deadline. The 3s
// default is half of the fail-soft contract — overrun degrades to an empty
// result — so a test widening the budget for its own assertions must not be
// able to move it. Zero value means production.
func TestSemantixLookupBudget(t *testing.T) {
	if got := (semantixLookup{}).budget(); got != 3*time.Second {
		t.Errorf("production budget: got %s, want 3s", got)
	}
	if got := (semantixLookup{timeout: 250 * time.Millisecond}).budget(); got != 250*time.Millisecond {
		t.Errorf("injected budget: got %s, want 250ms", got)
	}
	if got := (semantixLookup{timeout: -1}).budget(); got != 3*time.Second {
		t.Errorf("negative budget must fall back to production: got %s", got)
	}
}

func TestSemantixLookupRequiresQuery(t *testing.T) {
	_, err := semantixLookup{}.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want error for missing query")
	}
	_, err = semantixLookup{}.Execute(context.Background(), json.RawMessage(`{"query":"x","limit":999}`))
	if err != nil {
		t.Fatalf("limit clamped, no error expected: %v", err)
	}
}

// TestSemantixLookupFailsSoft: a missing/breaking `semantix` binary must
// degrade to an empty result, never an error.
func TestSemantixLookupFailsSoft(t *testing.T) {
	// No semantix binary in this environment → subprocess fails → soft empty.
	out, err := semantixLookup{}.Execute(context.Background(),
		json.RawMessage(`{"query":"anything"}`))
	if err != nil {
		t.Fatalf("soft degrade violated: %v", err)
	}
	if out != "" {
		t.Errorf("want empty output, got %q", out)
	}
}

// TestSemantixLookupSubprocess verifies the exact argv by putting a fake
// `semantix` script first on PATH.
func TestSemantixLookupSubprocess(t *testing.T) {
	bin := t.TempDir()
	scriptName := "semantix"
	scriptBody := "#!/bin/sh\necho \"$@\"\n"
	if runtime.GOOS == "windows" {
		scriptName = "semantix.bat"
		scriptBody = "@echo off\r\necho %*\r\n"
	}
	script := filepath.Join(bin, scriptName)
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// This test asserts argv, not the production subprocess time budget. Give
	// fork+exec enough room when the full race-enabled suite loads the runner.
	out, err := (semantixLookup{timeout: time.Minute}).Execute(context.Background(),
		json.RawMessage(`{"query":"修复 go 测试","limit":7}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "lookup --query 修复 go 测试 --limit 7 --json"
	if runtime.GOOS == "windows" {
		want = `lookup --query "修复 go 测试" --limit 7 --json`
	}
	if got := strings.ReplaceAll(out, "\r\n", "\n"); got != want+"\n" {
		t.Errorf("argv mismatch:\n got %q\nwant %q", out, want+"\n")
	}
}
