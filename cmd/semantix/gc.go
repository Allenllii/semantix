package main

import (
	"flag"
	"fmt"
	"io"
	"math"

	"semantix/kernel/slice"
)

// runGC cleans expired / low-score slices from the slice library (U26):
//   - --retention-days N: remove slices created more than N days ago
//     (slices with unknown creation time are never expired by retention)
//   - --min-weight W:     remove slices whose value weight is below W
//   - --dry-run:          report what would be removed without deleting
//
// Both criteria are optional and independent (a slice matching either is
// removed); bare `gc` with no criteria removes nothing, so the command is
// safe to run unattended. Exit code follows U19 §4.3.
func runGC(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("gc", flag.ContinueOnError)
	flags.SetOutput(stderr)
	retentionDays := flags.Int("retention-days", 0, "remove slices older than N days (0 disables)")
	minWeight := flags.Float64("min-weight", 0, "remove slices with weight below W (0 disables)")
	dryRun := flags.Bool("dry-run", false, "report candidates without deleting")
	dbOverride := flags.String("db", cfgString(deps.resolved, "store.db", ""), "database path override")
	jsonOutput := flags.Bool("json", false, "write JSON envelope output")
	if err := flags.Parse(args); err != nil {
		// Parse failures (unknown flag, bad value) are usage errors: exit 2
		// per U19 §4.3. ErrHelp unwraps to flag.ErrHelp so run() still exits 0.
		return usageError{err: err}
	}
	if flags.NArg() != 0 {
		return usageErrf("gc: unexpected arguments: %v", flags.Args())
	}
	if *retentionDays < 0 {
		return usageErrf("gc: --retention-days must be >= 0 (got %d)", *retentionDays)
	}
	// NaN/Inf would silently disable the criterion; validate like the zone
	// flags in flags.go (NaN comparisons are always false).
	if math.IsNaN(*minWeight) || math.IsInf(*minWeight, 0) || *minWeight < 0 {
		return usageErrf("gc: --min-weight must be a finite value >= 0 (got %v)", *minWeight)
	}

	store, err := deps.openStore(storePath(*dbOverride, "project"))
	if err != nil {
		if *jsonOutput {
			return failJSON(stdout, "gc", err)
		}
		return err
	}
	defer closeStore(store)

	res, err := slice.GC(store, slice.GCOptions{
		RetentionDays: *retentionDays,
		MinWeight:     *minWeight,
		DryRun:        *dryRun,
	})
	if err != nil {
		if *jsonOutput {
			return failJSON(stdout, "gc", err)
		}
		return err
	}
	// Fold the journal into the base so the store is a plain v1 JSONL file
	// again — gc doubles as the downgrade path for older binaries.
	if !*dryRun {
		if c, ok := store.(interface{ Compact() error }); ok {
			if err := c.Compact(); err != nil {
				if *jsonOutput {
					return failJSON(stdout, "gc", err)
				}
				return err
			}
		}
	}
	if *jsonOutput {
		expired, lowScore := res.Expired, res.LowScore
		if expired == nil {
			expired = []string{}
		}
		if lowScore == nil {
			lowScore = []string{}
		}
		data := map[string]interface{}{
			"checked":   res.Checked,
			"removed":   res.Removed,
			"dry_run":   *dryRun,
			"expired":   expired,
			"low_score": lowScore,
		}
		return writeEnvelope(stdout, "gc", data)
	}
	if *dryRun {
		fmt.Fprintf(stdout, "gc: dry-run checked=%d would_remove=%d\n", res.Checked, res.Removed)
	} else {
		fmt.Fprintf(stdout, "gc: checked=%d removed=%d\n", res.Checked, res.Removed)
	}
	for _, id := range res.Expired {
		fmt.Fprintf(stdout, "  expired  %s\n", id)
	}
	for _, id := range res.LowScore {
		fmt.Fprintf(stdout, "  low-score %s\n", id)
	}
	return nil
}
