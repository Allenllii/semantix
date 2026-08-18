package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"semantix/kernel/slice"
)

// runImport restores a slice library from an `export` JSONL backup (U26).
// Idempotent: Put replaces by ID, so re-importing the same backup converges
// to the same store state (no duplicates). Corrupt lines are skipped and
// reported, matching the store's own tolerant parsing.
func runImport(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "-", "backup file path, or - for stdin")
	dbOverride := flags.String("db", cfgString(deps.resolved, "store.db", ""), "database path override")
	jsonOutput := flags.Bool("json", false, "write JSON envelope output")
	if err := flags.Parse(args); err != nil {
		// Parse failures (unknown flag, bad value) are usage errors: exit 2
		// per U19 §4.3. ErrHelp unwraps to flag.ErrHelp so run() still exits 0.
		return usageError{err: err}
	}
	if flags.NArg() != 0 {
		return usageErrf("import: unexpected arguments: %v", flags.Args())
	}

	store, err := deps.openStore(storePath(*dbOverride, "project"))
	if err != nil {
		if *jsonOutput {
			return failJSON(stdout, "import", err)
		}
		return err
	}
	defer closeStore(store)

	var r io.Reader = os.Stdin
	if *input != "-" {
		f, err := os.Open(*input)
		if err != nil {
			if *jsonOutput {
				return failJSON(stdout, "import", err)
			}
			return err
		}
		defer f.Close() // close on every return path
		r = f
	}
	imported, skipped, err := slice.Import(store, r)

	if err != nil {
		if *jsonOutput {
			return failJSON(stdout, "import", err)
		}
		return err
	}
	if *jsonOutput {
		return writeEnvelope(stdout, "import", map[string]interface{}{
			"imported": imported,
			"skipped":  skipped,
		})
	}
	fmt.Fprintf(stdout, "import: imported=%d skipped=%d\n", imported, skipped)
	return nil
}
