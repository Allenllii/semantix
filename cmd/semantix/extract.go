package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"semantix/kernel/fingerprint"
	"semantix/kernel/slice"
)

func runExtract(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("extract", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "session JSONL path, or - for stdin")
	scopeValue := flags.String("scope", "project", "slice scope: session, project, or user")
	dbOverride := flags.String("db", "", "database path override")
	projectDB := flags.String("project-db", defaultProjectDB(), "project/session database path")
	userDB := flags.String("user-db", defaultUserDB(), "user database path")
	session := flags.String("session", "", "source session identifier")
	project := flags.String("project", "", "project slug")
	taskType := flags.String("task-type", "", "task type metadata")
	language := flags.String("language", "", "language metadata")
	fingerprintPaths := flags.String("fingerprint", "", "comma-separated relative paths to fingerprint (sha256) into each slice's Deps")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return errors.New("--input is required")
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	scope, err := parseScope(*scopeValue)
	if err != nil {
		return err
	}
	data, err := readInput(*input)
	if err != nil {
		return err
	}

	extractor := deps.newExtractor()
	if extractor == nil {
		return errors.New("slice extractor is unavailable; merge the U4 implementation first")
	}
	meta := slice.SliceMeta{
		SourceSession: *session,
		TaskType:      *taskType,
		Language:      *language,
		ProjectSlug:   *project,
	}
	if *fingerprintPaths != "" {
		var paths []string
		for _, p := range strings.Split(*fingerprintPaths, ",") {
			if p = strings.TrimSpace(p); p != "" {
				paths = append(paths, p)
			}
		}
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
		depsMap, err := fingerprint.Capture(wd, paths)
		if err != nil {
			return fmt.Errorf("capture fingerprints: %w", err)
		}
		meta.Deps = depsMap
		// Record mtimes alongside the sha256 digests (U16): the L3 gate uses
		// mtimes as its cheap fast-fail check before re-reading content.
		mtimes := make(map[string]int64, len(paths))
		for _, p := range paths {
			st, err := os.Stat(filepath.Join(wd, p))
			if err != nil {
				return fmt.Errorf("stat %s: %w", p, err)
			}
			mtimes[p] = st.ModTime().Unix()
		}
		meta.Mtimes = mtimes
	}
	items, err := extractor.Extract(data, meta)
	if err != nil {
		return fmt.Errorf("extract slices: %w", err)
	}

	dbPath := selectDB(scope, *dbOverride, *projectDB, *userDB)
	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create database directory: %w", err)
		}
	}
	store, err := deps.openStore(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	if store == nil {
		return errors.New("slice store is unavailable; merge the U4 implementation first")
	}
	defer closeStore(store)

	stored := 0
	for i, item := range items {
		if item == nil {
			return fmt.Errorf("extractor returned nil slice at position %d", i)
		}
		item.Scope = scope
		if err := store.Put(item); err != nil {
			return fmt.Errorf("store slice %q: %w", item.ID, err)
		}
		stored++
	}

	fmt.Fprintf(stdout, "extracted=%d stored=%d scope=%s db=%s\n", len(items), stored, scopeName(scope), dbPath)
	return nil
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read input %q: %w", path, err)
	}
	return data, nil
}
