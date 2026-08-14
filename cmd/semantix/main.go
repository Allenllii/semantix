package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

type dependencies struct {
	newExtractor func() slice.Extractor
	openStore    func(string) (slice.Store, error)
	newIndex     func() slice.Index
}

func productionDependencies() dependencies {
	return dependencies{
		newExtractor: slice.NewExtractor,
		openStore:    slice.NewFileStore,
		newIndex: func() slice.Index {
			return bm25.New()
		},
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, productionDependencies()))
}

// usageError marks a command-line usage mistake (unknown flag, missing or
// invalid argument). run() maps it to exit code 2 per the U19 contract
// (docs/reports/cli-v2-architecture.md §4.3).
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// usagef builds a usageError from a format string.
func usagef(format string, args ...any) error {
	return usageError{err: fmt.Errorf(format, args...)}
}

// usageWrap marks an existing error (e.g. a flag parse error) as a usage
// mistake while preserving its error chain so errors.Is/errors.As still
// work on it (flag.ErrHelp must stay detectable through the wrapper).
func usageWrap(err error) error {
	if err == nil {
		return nil
	}
	var ue usageError
	if errors.As(err, &ue) {
		return err // already marked
	}
	return usageError{err: err}
}

// isUsage reports whether err carries the usage-error marker.
func isUsage(err error) bool {
	var ue usageError
	return errors.As(err, &ue)
}

// commandGroup is one branch of the CLI v2 command tree
// (docs/reports/cli-v2-architecture.md §3). U19 freezes the four groups;
// later units (U21/U23-U27) mount commands into the empty ones.
type commandGroup int

const (
	groupKernelOps commandGroup = iota // kernel 运维（U19：现有 8 命令，行为不变）
	groupProduct                       // 产品与管理（U21/U23/U24/U25 挂载）
	groupMaintenance                   // 维护（U26 挂载）
	groupService                       // 服务模式（U27 挂载）
)

func (g commandGroup) String() string {
	switch g {
	case groupKernelOps:
		return "Kernel operations"
	case groupProduct:
		return "Product & management"
	case groupMaintenance:
		return "Maintenance"
	case groupService:
		return "Service mode"
	}
	return "Unknown group"
}

// plannedByGroup lists the commands planned for groups that U19 does not
// implement yet. They render in help as planned and reject dispatch with
// exit 2 until their unit lands.
var plannedByGroup = map[commandGroup][]string{
	groupProduct:     {"init", "config", "version", "install", "completion"},
	groupMaintenance: {"gc", "export", "import"},
	groupService:     {"serve", "watch"},
}

// commandSpec registers one CLI command in the command tree. All commands
// share one dispatch signature; error-returning commands are adapted with
// errCommand so the exit-code contract is enforced in one place.
type commandSpec struct {
	name    string
	group   commandGroup
	usage   string // one-line invocation synopsis (help)
	summary string // one-line description (help)
	run     func(args []string, stdout, stderr io.Writer, deps dependencies) int
}

// commands is the command tree, in help order within each group.
var commands = []commandSpec{
	{name: "extract", group: groupKernelOps,
		usage:   "semantix extract --input <session.jsonl> [--scope project|user|session]",
		summary: "extract session JSONL into semantic slices",
		run:     errCommand("extract", runExtract)},
	{name: "search", group: groupKernelOps,
		usage:   "semantix search [flags] <query>",
		summary: "semantic retrieval (bm25 / vector / hybrid)",
		run:     errCommand("search", runSearch)},
	{name: "verify", group: groupKernelOps,
		usage:   "semantix verify --session <file|dir> [--holdout 0.3]",
		summary: "offline replay validation (exit 3 = gate not met)",
		run:     depsCommand(runVerify)},
	{name: "eval", group: groupKernelOps,
		usage:   "semantix eval --set <oracle.tsv> [--tau-*]",
		summary: "retrieval strategy comparison (Issue #7)",
		run:     intCommand(runEval)},
	{name: "eval-judge", group: groupKernelOps,
		usage:   "semantix eval-judge [--stub yes|no] [--judge-*] [--audit <tsv>]",
		summary: "LLM judge authenticity evaluation (Issue #8)",
		run:     intCommand(runEvalJudge)},
	{name: "usage", group: groupKernelOps,
		usage:   "semantix usage [--db <usage.jsonl>] [--evolve-db <dir>]",
		summary: "cost-savings statistics (Issue #60)",
		run:     intCommand(runUsage)},
	{name: "lookup", group: groupKernelOps,
		usage:   "semantix lookup --query <q> [--limit N] [--db ...]",
		summary: "single-query retrieval (harness tool backend)",
		run:     errCommand("lookup", runLookup)},
	{name: "inject", group: groupKernelOps,
		usage:   "semantix inject --query <q> [--budget N] [--db ...]",
		summary: "L2 injection block generation (harness backend)",
		run:     errCommand("inject", runInject)},
	{name: "doctor", group: groupProduct,
		usage:   "semantix doctor [--config <path>] [--db <path>] [--json]",
		summary: "health check (db / config / embedder / judge; exit 3 on any FAIL)",
		run: func(args []string, stdout, stderr io.Writer, _ dependencies) int {
			return runDoctor(args, stdout, stderr)
		}},
}

func run(args []string, stdout, stderr io.Writer, deps dependencies) int {
	if len(args) == 0 {
		printHelp(stderr)
		return 2
	}
	switch args[0] {
	case "help", "-h", "--help":
		return runHelp(args[1:], stdout, stderr)
	}
	cmd := findCommand(args[0])
	if cmd == nil {
		fmt.Fprintf(stderr, "semantix: unknown command %q\n\n", args[0])
		printHelp(stderr)
		return 2
	}
	return cmd.run(args[1:], stdout, stderr, deps)
}

// findCommand looks a command up in the command tree by name.
func findCommand(name string) *commandSpec {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// runHelp implements `semantix help [command]`: with a command argument it
// prints that command's synopsis; otherwise the full grouped command tree.
func runHelp(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}
	cmd := findCommand(args[0])
	if cmd == nil {
		fmt.Fprintf(stderr, "semantix help: unknown command %q\n", args[0])
		return 2
	}
	fmt.Fprintf(stdout, "Usage: %s\n\n%s\n", cmd.usage, cmd.summary)
	return 0
}

// printHelp renders the command tree grouped by the four U19 groups. Groups
// without registered commands list their planned names (U21/U23-U27).
func printHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  semantix <command> [flags]")
	fmt.Fprintln(w, "  semantix help               list commands by group")
	fmt.Fprintln(w, "  semantix help <command>     show one command's synopsis")
	fmt.Fprintln(w, "  semantix <command> --help   show one command's flags")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exit codes: 0 ok · 1 runtime error · 2 usage error · 3 gate not met")
	fmt.Fprintln(w)
	for g := groupKernelOps; g <= groupService; g++ {
		var inGroup []commandSpec
		for _, c := range commands {
			if c.group == g {
				inGroup = append(inGroup, c)
			}
		}
		if len(inGroup) == 0 {
			if planned := plannedByGroup[g]; len(planned) > 0 {
				fmt.Fprintf(w, "%s (planned: %s)\n", g, strings.Join(planned, " "))
			} else {
				fmt.Fprintf(w, "%s\n", g)
			}
			fmt.Fprintln(w)
			continue
		}
		fmt.Fprintf(w, "%s\n", g)
		for _, c := range inGroup {
			fmt.Fprintf(w, "  %-11s %s\n", c.name, c.summary)
		}
		if planned := plannedByGroup[g]; len(planned) > 0 {
			fmt.Fprintf(w, "  (planned: %s)\n", strings.Join(planned, " "))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, `Run "semantix help <command>" or "semantix <command> --help" for details.`)
}

// errCommand adapts an error-returning command (extract/search/lookup/inject)
// to the dispatch signature, mapping errors to the U19 exit-code contract:
// 0 ok, 0 for --help, 2 usage error, 1 runtime error.
func errCommand(name string, fn func(args []string, stdout, stderr io.Writer, deps dependencies) error) func(args []string, stdout, stderr io.Writer, deps dependencies) int {
	return func(args []string, stdout, stderr io.Writer, deps dependencies) int {
		if err := fn(args, stdout, stderr, deps); err == nil {
			return 0
		} else if errors.Is(err, flag.ErrHelp) {
			return 0 // --help is a successful request, not a failure
		} else if isUsage(err) {
			fmt.Fprintf(stderr, "semantix %s: %v\n", name, err)
			return 2
		} else {
			fmt.Fprintf(stderr, "semantix %s: %v\n", name, err)
			return 1
		}
	}
}

// intCommand adapts commands that already return an exit code directly
// (eval-judge/usage) to the dispatch signature.
func intCommand(fn func(args []string, stdout io.Writer) int) func(args []string, stdout, stderr io.Writer, deps dependencies) int {
	return func(args []string, stdout, stderr io.Writer, deps dependencies) int {
		return fn(args, stdout)
	}
}

// depsCommand adapts commands that return an exit code and take deps but
// no stderr (verify/eval) to the dispatch signature. Their diagnostics
// keep going to stdout, as before U19.
func depsCommand(fn func(args []string, stdout io.Writer, deps dependencies) int) func(args []string, stdout, stderr io.Writer, deps dependencies) int {
	return func(args []string, stdout, stderr io.Writer, deps dependencies) int {
		return fn(args, stdout, deps)
	}
}

type storeCloser interface {
	Close() error
}

func closeStore(store slice.Store) {
	if closer, ok := store.(storeCloser); ok {
		_ = closer.Close()
	}
}
