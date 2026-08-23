package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// bare `semantix` with no agent installed falls back to help (exit 2) — the
// kernel-only install keeps its old behavior.
func TestBareCommandFallsBackToHelpWhenNoAgent(t *testing.T) {
	orig := findAgentBinary
	t.Cleanup(func() { findAgentBinary = orig })
	findAgentBinary = func() string { return "" }

	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr, productionDependencies())
	if code != 2 {
		t.Fatalf("bare semantix (no agent) code = %d, want 2", code)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("Usage:")) {
		t.Fatalf("expected help on stderr, got %q", stderr.String())
	}
}

// bare `semantix` with an agent present launches it and returns its exit code,
// without ever loading kernel config or printing help.
func TestBareCommandLaunchesAgentWhenPresent(t *testing.T) {
	origFind, origLaunch := findAgentBinary, launchAgent
	t.Cleanup(func() { findAgentBinary, launchAgent = origFind, origLaunch })

	findAgentBinary = func() string { return "/opt/bundle/reasonix" }
	var launched string
	launchAgent = func(path string) int {
		launched = path
		return 0
	}

	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr, productionDependencies())
	if code != 0 {
		t.Fatalf("bare semantix (agent present) code = %d, want 0", code)
	}
	if launched != "/opt/bundle/reasonix" {
		t.Fatalf("launched %q, want /opt/bundle/reasonix", launched)
	}
	if stderr.Len() != 0 || stdout.Len() != 0 {
		t.Fatalf("launch path must not print help; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

// A subcommand invocation must never launch the agent — it routes to the
// kernel path (this is exactly what reasonix calls back as `semantix <cmd>`).
func TestSubcommandNeverLaunchesAgent(t *testing.T) {
	origFind, origLaunch := findAgentBinary, launchAgent
	t.Cleanup(func() { findAgentBinary, launchAgent = origFind, origLaunch })
	findAgentBinary = func() string { t.Fatal("findAgentBinary must not be consulted for a subcommand"); return "" }
	launchAgent = func(string) int { t.Fatal("launchAgent must not run for a subcommand"); return 0 }

	var stdout, stderr bytes.Buffer
	// `help` is a subcommand-like arg; it must reach runHelp, not the launcher.
	if code := run([]string{"help"}, &stdout, &stderr, productionDependencies()); code != 0 {
		t.Fatalf("semantix help code = %d, want 0", code)
	}
}

// findAgentBinary discovers an agent on PATH when none sits beside the
// executable (the PATH branch of the real discovery).
func TestFindAgentBinaryOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix executable-bit discovery")
	}
	dir := t.TempDir()
	agent := filepath.Join(dir, "reasonix")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got := findAgentBinary()
	// EvalSymlinks may canonicalize /var → /private/var on macOS; compare basenames + existence.
	if filepath.Base(got) != "reasonix" {
		t.Fatalf("findAgentBinary() = %q, want a reasonix path under %q", got, dir)
	}
	if !isExecutableFile(got) {
		t.Fatalf("discovered agent %q is not executable", got)
	}
}
