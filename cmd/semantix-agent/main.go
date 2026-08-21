// Command semantix-agent is the harness agent CLI (vendor of Reasonix, MIT,
package main

import (
	"os"
	"runtime/debug"

	"semantix/harness/cli"
	"semantix/harness/config"
	"semantix/harness/crashreport"

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
	os.Exit(runWithCrashCapture(os.Args[1:], version))
}

func runWithCrashCapture(args []string, buildVersion string) (exitCode int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = crashreport.CapturePanic(config.ReasonixHomeDir(), buildVersion, recovered, debug.Stack())
			panic(recovered)
		}
	}()
	return runCLI(args, buildVersion)
}
