package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// This file makes a bare `semantix` (no subcommand) launch the co-located
// coding agent in the current directory, so the one global `semantix` command
// is both the interactive agent (bare) and the memory kernel (subcommands).
// The agent already shells back out to `semantix <subcommand>` for L2/L3
// memory (harness SemantixConfig.Binary defaults to "semantix" on PATH), so a
// single installed binary closes the loop with no recursion — reasonix only
// ever calls `semantix` WITH a subcommand, which routes to the kernel path.

// agentBinaryNames are the coding-agent executables the umbrella launches,
// in preference order. The downloadable bundle installs the agent as
// `reasonix`; `semantix-agent` is the in-repo cmd name.
var agentBinaryNames = []string{"reasonix", "semantix-agent"}

// exeSuffix is ".exe" on Windows, "" elsewhere.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// findAgentBinary locates the coding-agent binary: first next to this
// executable (the bundle installs kernel + agent side by side), then on PATH.
// Returns "" when no agent is installed — the caller then falls back to help,
// so a kernel-only install keeps its old bare-command behavior. Overridable
// in tests.
var findAgentBinary = func() string {
	var selfDir string
	if exe, err := os.Executable(); err == nil {
		if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
			selfDir = filepath.Dir(resolved)
		} else {
			selfDir = filepath.Dir(exe)
		}
	}
	for _, name := range agentBinaryNames {
		if selfDir != "" {
			cand := filepath.Join(selfDir, name+exeSuffix())
			if isExecutableFile(cand) {
				return cand
			}
		}
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// isExecutableFile reports whether path is a regular file the OS will run.
// On Windows the executable bit does not exist, so presence + regular-file is
// enough; on unix it must have an owner/group/other execute bit.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

// launchAgent runs the agent at path with the current directory as its
// workspace (the harness derives the workspace from cwd). It forwards no args
// — a bare `semantix` becomes a bare agent invocation, which is what plays the
// intro and enters the interactive TUI. On unix it replaces this process
// (execAgent → syscall.Exec) so signals and the TTY go straight to the agent;
// on other platforms it runs the agent as a child and returns its exit code.
// Overridable in tests.
var launchAgent = func(path string) int {
	return execAgent(path)
}
