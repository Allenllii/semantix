package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
)

// Build metadata, injected at link time via -ldflags "-X main.version=...".
// Defaults keep the dev build informative.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = ""
)

type versionData struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
}

// runVersion prints version + commit + build time (U21 §3.1).
func runVersion(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "output as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "version: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	if *jsonOut {
		if err := writeJSON(stdout, okEnvelope("version", versionData{Version: version, Commit: commit, BuildTime: buildTime})); err != nil {
			fmt.Fprintf(stderr, "version: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintf(stdout, "version\t%s\n", version)
	fmt.Fprintf(stdout, "commit\t%s\n", commit)
	if buildTime != "" {
		fmt.Fprintf(stdout, "build_time\t%s\n", buildTime)
	}
	return 0
}

// writeJSON encodes v as indented JSON to w (shared by --json commands).
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
