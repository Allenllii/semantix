package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"semantix/kernel/slice"
)

// runExport backs up the whole slice library to JSONL (U26): every slice in
// the store, including Meta, as one JSON object per line — a lossless backup
// that `import` restores. `--output -` streams to stdout for piping.
func runExport(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "-", "backup file path, or - for stdout")
	dbOverride := flags.String("db", cfgString(deps.resolved, "store.db", ""), "database path override")
	jsonOutput := flags.Bool("json", false, "write JSON envelope output")
	if err := flags.Parse(args); err != nil {
		// Parse failures (unknown flag, bad value) are usage errors: exit 2
		// per U19 §4.3. ErrHelp unwraps to flag.ErrHelp so run() still exits 0.
		return usageError{err: err}
	}
	if flags.NArg() != 0 {
		return usageErrf("export: unexpected arguments: %v", flags.Args())
	}
	// The envelope must be the only thing on stdout; raw JSONL cannot share it.
	if *jsonOutput && *output == "-" {
		return usageErrf("export: --json requires --output <file> (raw JSONL is streamed with --output -)")
	}

	store, err := deps.openStore(storePath(*dbOverride, "project"))
	if err != nil {
		if *jsonOutput {
			return failJSON(stdout, "export", err)
		}
		return err
	}
	defer closeStore(store)

	var w io.Writer = stdout
	var outFile *os.File
	var tmpName string
	if *output != "-" {
		// Write via a uniquely-named temp file (0600) then atomic rename,
		// mirroring fileStore.writeAll: a failed export never truncates or
		// half-destroys the previous backup, and the file never lands
		// world-readable.
		tmp, err := os.CreateTemp(filepath.Dir(*output), filepath.Base(*output)+".tmp-*")
		if err != nil {
			if *jsonOutput {
				return failJSON(stdout, "export", err)
			}
			return err
		}
		tmpName = tmp.Name()
		defer os.Remove(tmpName) // no-op after successful rename
		if err := tmp.Chmod(0o600); err != nil {
			tmp.Close()
			if *jsonOutput {
				return failJSON(stdout, "export", err)
			}
			return err
		}
		w = tmp
		outFile = tmp
	}
	n, skipped, err := slice.Export(store, w)

	if err != nil {
		// Every path that opens the file closes it exactly once: the error
		// path here, the durability path below.
		if outFile != nil {
			_ = outFile.Close()
		}
		if *jsonOutput {
			return failJSON(stdout, "export", err)
		}
		return err
	}
	// Flush to disk, then atomically replace the previous backup (durability,
	// matching writeAll's fsync + rename standard in file_store.go).
	if outFile != nil {
		if serr := outFile.Sync(); serr != nil {
			_ = outFile.Close()
			if *jsonOutput {
				return failJSON(stdout, "export", serr)
			}
			return serr
		}
		if cerr := outFile.Close(); cerr != nil {
			if *jsonOutput {
				return failJSON(stdout, "export", cerr)
			}
			return cerr
		}
		if rerr := os.Rename(tmpName, *output); rerr != nil {
			if *jsonOutput {
				return failJSON(stdout, "export", rerr)
			}
			return rerr
		}
	}
	if *jsonOutput {
		return writeEnvelope(stdout, "export", map[string]interface{}{
			"exported": n,
			"skipped":  skipped,
			"output":   *output,
		})
	}
	if skipped > 0 {
		fmt.Fprintf(stderr, "export: warning: %d corrupt store lines skipped\n", skipped)
	}
	if *output == "-" {
		// JSONL owns stdout; the summary must not pollute the stream.
		fmt.Fprintf(stderr, "export: %d slices to stdout\n", n)
	} else {
		fmt.Fprintf(stdout, "export: %d slices to %s\n", n, *output)
	}
	return nil
}
