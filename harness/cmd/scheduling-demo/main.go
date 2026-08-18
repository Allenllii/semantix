// Command scheduling-demo runs the Agile-2 scheduling demo (Issue #193): it
// replays a small task family through the real agent loop once with the kernel
// scheduler on and once with it off, prints a side-by-side comparison, and
// writes the raw per-arm records as JSONL for docs/reports/agile2-scheduling-demo.md.
//
// The scheduling decisions and the harness behavior they change are real
// production code paths (Agent.Run → executeBatch → kernel/sched.RuleDecider).
// Per-tool durations and the token→USD price model are labeled synthetic
// fixtures; see the report's honesty note.
//
// Usage:
//
//	go run ./harness/cmd/scheduling-demo [-out DIR] [-quiet]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"semantix/harness/internal/scheddemo"
)

func main() {
	out := flag.String("out", filepath.FromSlash("docs/reports/data/agile2-scheduling-demo"), "directory for JSONL evidence")
	quiet := flag.Bool("quiet", false, "suppress the comparison table (still writes JSONL)")
	flag.Parse()

	cmps := scheddemo.RunAll(context.Background())

	if err := writeJSONL(*out, cmps); err != nil {
		fmt.Fprintf(os.Stderr, "scheduling-demo: writing JSONL: %v\n", err)
		os.Exit(1)
	}
	if !*quiet {
		printReport(os.Stdout, cmps)
	}
	fmt.Printf("\nJSONL evidence: %s (%d lines: %d scenarios × 2 arms)\n",
		filepath.Join(*out, "scheduling-demo.jsonl"), len(cmps)*2, len(cmps))
}

// writeJSONL emits one line per arm (kernel-on, then kernel-off) for each
// scenario, so the report can point at the exact decisions and tool results.
func writeJSONL(dir string, cmps []scheddemo.Comparison) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "scheduling-demo.jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, c := range cmps {
		if err := enc.Encode(c.On); err != nil {
			return err
		}
		if err := enc.Encode(c.Off); err != nil {
			return err
		}
	}
	return nil
}

func printReport(f *os.File, cmps []scheddemo.Comparison) {
	fmt.Fprintln(f, "Semantix scheduling demo (Issue #193) — kernel decision on vs off")
	fmt.Fprintln(f, "Fixtures: synthetic per-tool durations + DeepSeek V4 price model (decisions & concurrency are real).")

	var onTotal, offTotal float64
	for _, c := range cmps {
		fmt.Fprintf(f, "\n%s — %s\n", c.Scenario, c.Intent)
		tw := tabwriter.NewWriter(f, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "  metric\tkernel-on\tkernel-off")
		fmt.Fprintf(tw, "  peak concurrency\t%d\t%d\n", c.On.PeakConcurrency, c.Off.PeakConcurrency)
		fmt.Fprintf(tw, "  wall clock\t%s\t%s\n", dur(c.On.WallClock), dur(c.Off.WallClock))
		fmt.Fprintf(tw, "  cost (USD)\t%.6f\t%.6f\n", c.On.CostUSD, c.Off.CostUSD)
		fmt.Fprintf(tw, "  tiers\t%v\t%v\n", tiers(c.On.TierSequence), tiers(c.Off.TierSequence))
		fmt.Fprintf(tw, "  budget actions\t%v\t%v\n", dash(c.On.BudgetActions), dash(c.Off.BudgetActions))
		tw.Flush()
		onTotal += c.On.CostUSD
		offTotal += c.Off.CostUSD
	}

	saving := 0.0
	if offTotal > 0 {
		saving = (offTotal - onTotal) / offTotal * 100
	}
	fmt.Fprintf(f, "\nAggregate cost: kernel-on $%.6f vs kernel-off $%.6f — saving %.1f%%\n", onTotal, offTotal, saving)
}

func dur(d time.Duration) string { return d.Round(time.Millisecond).String() }

func tiers(s []string) []string {
	out := make([]string, len(s))
	for i, t := range s {
		if t == "" {
			out[i] = "pro*"
		} else {
			out[i] = t
		}
	}
	return out
}

func dash(s []string) any {
	if len(s) == 0 {
		return "-"
	}
	return s
}
