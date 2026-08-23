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

// TestSemantixLookupTimeoutFailsSoft locks in the production resilience
// contract independently of the argv test's deliberately widened budget.
func TestSemantixLookupTimeoutFailsSoft(t *testing.T) {
	bin := t.TempDir()
	scriptName := "semantix"
	scriptBody := "#!/bin/sh\necho unexpected\n"
	if runtime.GOOS == "windows" {
		scriptName = "semantix.bat"
		scriptBody = "@echo off\r\necho unexpected\r\n"
	}
	if err := os.WriteFile(filepath.Join(bin, scriptName), []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := (semantixLookup{timeout: time.Nanosecond}).Execute(context.Background(),
		json.RawMessage(`{"query":"deadline"}`))
	if err != nil {
		t.Fatalf("timeout must fail soft, got error: %v", err)
	}
	if out != "" {
		t.Fatalf("timeout must fail soft with empty output, got %q", out)
	}
}
