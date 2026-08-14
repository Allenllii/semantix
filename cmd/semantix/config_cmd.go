package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"semantix/kernel/config"
)

type configEntry struct {
	Value  any    `json:"value"`
	Source string `json:"source"`
}

// runConfig prints the effective configuration with per-key source annotation
// (U21 §3.3). Exit codes: 0 ok / 1 config file IO error / 2 invalid usage or
// invalid config content.
func runConfig(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "config file path (default ./semantix.toml)")
	db := fs.String("db", "", "database path override")
	jsonOut := fs.Bool("json", false, "output as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "config: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	res, err := config.Load(config.Options{ConfigPath: *configPath, DB: *db})
	if err != nil {
		code := 2
		var ce *config.Error
		if errors.As(err, &ce) && ce.Kind == config.KindIO {
			code = 1
		}
		if *jsonOut {
			_ = writeJSON(stdout, errEnvelope("config", code, err.Error()))
		} else {
			fmt.Fprintf(stderr, "config: %v\n", err)
		}
		return code
	}

	if *jsonOut {
		data := make(map[string]configEntry, len(res.Fields))
		for _, f := range res.Fields {
			data[f.Key] = configEntry{Value: f.Value, Source: string(f.Source)}
		}
		if err := writeJSON(stdout, okEnvelope("config", data)); err != nil {
			fmt.Fprintf(stderr, "config: %v\n", err)
			return 1
		}
		return 0
	}

	for _, f := range res.Fields {
		fmt.Fprintf(stdout, "%s = %v\t# source=%s\n", f.Key, f.Value, f.Source)
	}
	return 0
}
