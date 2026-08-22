// Command semantix-agent is the harness agent CLI (vendor of Reasonix, MIT,
package main

import (
	"io"
	"os"
	"runtime/debug"

	"semantix/harness/cli"
	"semantix/harness/config"
	"semantix/harness/crashreport"
	"semantix/internal/intro"

	// Blank imports wire compile-time built-ins into their registries.
	_ "semantix/harness/provider/anthropic"
	_ "semantix/harness/provider/openai"
	_ "semantix/harness/provider/responses"
	_ "semantix/harness/tool/builtin"
)

// Build identity injected via -ldflags (see scripts/release/build-full.sh).
// version remains the single-line contract for `semantix-agent --version`;
// gitCommit/buildTimeUTC feed `semantix-agent version --verbose` / `--json`
// without embedding config paths.
var (
	version      = "dev"
	gitCommit    = ""
	buildTimeUTC = ""
)

// runCLI is the CLI entry; tests may stub it. Production routes through
// RunWithBuildInfo so ldflags metadata is available to version --verbose/--json.
var runCLI = func(args []string, buildVersion string) int {
	return cli.RunWithBuildInfo(args, cli.BuildInfo{
		Version:      buildVersion,
		GitCommit:    gitCommit,
		BuildTimeUTC: buildTimeUTC,
	})
}

func main() {
	maybePlayIntro(os.Args[1:], os.Stdout)
	os.Exit(runWithCrashCapture(os.Args[1:], version))
}

// isInteractiveTerminal reports whether stdout is a live terminal; tests
// stub it to exercise the intro decision without a pty.
var isInteractiveTerminal = intro.TerminalSupportsEffects

// maybePlayIntro plays the branded wordmark animation (Issue #307) before
// the interactive CLI starts. Only a bare `semantix-agent` reaching a real
// terminal plays it; subcommands, flags, piped output, and
// SEMANTIX_NO_INTRO all skip it so scripted invocations stay byte-stable.
func maybePlayIntro(args []string, stdout io.Writer) {
	if !shouldPlayIntro(args, stdout) {
		return
	}
	_ = intro.Run(stdout, intro.Options{Version: version})
}

func shouldPlayIntro(args []string, stdout io.Writer) bool {
	if len(args) != 0 {
		return false
	}
	if os.Getenv("SEMANTIX_NO_INTRO") != "" {
		return false
	}
	return isInteractiveTerminal(stdout)
}

func runWithCrashCapture(args []string, buildVersion string) (exitCode int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = crashreport.CapturePanic(config.SemantixHomeDir(), buildVersion, recovered, debug.Stack())
			panic(recovered)
		}
	}()
	return runCLI(args, buildVersion)
}
