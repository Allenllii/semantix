package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/spf13/pflag"
	"io"

	"semantix/kernel/slice"
)

// runReject marks one slice as user-rejected (Issue #267 step 2): the
// slice Rejected counter increments and UserFeedback goes -1, aligning with
// Security §4.1.3 "否决即失效". This is the manual negative-feedback path;
// the automatic harness detection emits SliceReject events separately (fed
// to the evolve engine as inject_pollution).
func runReject(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := pflag.NewFlagSet("reject", pflag.ContinueOnError) // pflag: flags may follow the positional slice-id
	flags.SetOutput(stderr)
	reason := flags.String("reason", "", "reject reason (recorded in the JSON output, not persisted)")
	dbOverride := flags.String("db", cfgString(deps.resolved, "store.db", ""), "database path override")
	jsonOutput := flags.Bool("json", false, "write JSON envelope output")
	if err := flags.Parse(args); err != nil {
		// pflag uses its own ErrHelp; errCommand treats flag.ErrHelp as a
		// successful --help request, so translate it to keep the contract.
		if errors.Is(err, pflag.ErrHelp) {
			return flag.ErrHelp
		}
		return usageError{err: err}
	}
	if flags.NArg() != 1 {
		return usageErrf("reject: expected exactly one slice id, got %d", flags.NArg())
	}
	id := flags.Arg(0)

	store, err := deps.openStore(storePath(*dbOverride, "project"))
	if err != nil {
		if *jsonOutput {
			return failJSON(stdout, "reject", err)
		}
		return err
	}
	defer closeStore(store)

	// UpdateStats returns errNotFound for unknown ids — "否决即失效" must not
	// silently accept a typo id.
	if err := store.UpdateStats(id, slice.SliceStats{Rejected: 1, UserFeedback: -1}); err != nil {
		if *jsonOutput {
			return failJSON(stdout, "reject", err)
		}
		return err
	}
	if *jsonOutput {
		return writeEnvelope(stdout, "reject", map[string]any{
			"slice_id": id,
			"rejected": true,
			"reason":   *reason,
		})
	}
	fmt.Fprintln(stdout, "rejected slice "+id)
	if *reason != "" {
		fmt.Fprintln(stdout, "reason: "+*reason)
	}
	return nil
}
