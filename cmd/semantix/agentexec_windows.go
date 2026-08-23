//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// execAgent runs the agent as a child process with inherited stdio (Windows
// has no exec-replace), propagating its exit code.
func execAgent(path string) int {
	cmd := exec.Command(path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "semantix: launch agent %s: %v\n", path, err)
		return 1
	}
	return 0
}
