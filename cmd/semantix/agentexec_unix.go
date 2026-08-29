//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// execAgent replaces the current process with the agent (syscall.Exec), so the
// interactive TUI owns the terminal and signals directly — no lingering
// umbrella parent. It only returns on failure to exec.
func execAgent(path string) int {
	err := syscall.Exec(path, []string{path}, os.Environ())
	// Exec only returns on error (bad binary, permissions, ENOEXEC).
	fmt.Fprintf(os.Stderr, "semantix: launch agent %s: %v\n", path, err)
	return 1
}
