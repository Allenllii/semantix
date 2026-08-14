package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"semantix/kernel/inject"
	"semantix/kernel/lookup"
	"semantix/kernel/slice"
)

// runLookup exposes the semantix_lookup tool contract over the CLI
// (U8 tool side): given a query, print top-k reuse slices as JSON.
func runLookup(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("lookup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	query := flags.String("query", "", "task description to match against stored slices")
	scopeValue := flags.String("scope", "project", "slice scope: session, project, or user")
	limit := flags.Int("limit", 5, "maximum number of slices (capped at 50)")
	dbOverride := flags.String("db", "", "database path override")
	// Accepted for compatibility with the harness tool contract
	// (semantix_lookup calls `semantix lookup --json`); output is JSON anyway.
	_ = flags.Bool("json", false, "output as JSON (default output is already JSON)")
	zf := addZoneFlags(flags)
	if err := flags.Parse(args); err != nil {
		return usageWrap(err)
	}
	if err := zf.validate(); err != nil {
		return usagef("%v", err)
	}
	if *limit > 50 { // match kernel lookup.Execute hard cap
		*limit = 50
	}
	if *limit <= 0 { // kernel Execute treats <=0 as default
		*limit = 5
	}
	if *query == "" && flags.NArg() > 0 {
		*query = flags.Arg(0)
	}
	if *query == "" {
		return usagef("lookup: --query is required")
	}

	store, err := deps.openStore(storePath(*dbOverride, *scopeValue))
	if err != nil {
		return err
	}
	idx := deps.newIndex()
	// index is session-local: rebuild from the store so lookup sees
	// previously extracted slices.
	if err := indexFromStore(store, idx); err != nil {
		return err
	}

	scope, err := parseScope(*scopeValue)
	if err != nil {
		return err
	}
	hits, err := idx.Search(*query, *limit, scope)
	if err != nil {
		return err
	}
	top1 := 0.0
	if len(hits) > 0 {
		top1 = hits[0].Score
	}
	zones := zf.zones()
	out := make([]lookup.Result, 0, len(hits))
	for _, h := range hits {
		out = append(out, lookup.Result{
			ID:      h.Slice.ID,
			Type:    h.Slice.Type.String(),
			Scope:   h.Slice.Scope.String(),
			Score:   h.Score,
			Zone:    zones.Classify(h.Score, top1).String(),
			Content: string(h.Slice.Content),
		})
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// runInject assembles an L2 injection block for a query (U8 compose side):
// top-k reuse slices in canonical order, budget-capped, marker-wrapped.
func runInject(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("inject", flag.ContinueOnError)
	flags.SetOutput(stderr)
	query := flags.String("query", "", "current user turn to match against stored slices")
	scopeValue := flags.String("scope", "project", "slice scope: session, project, or user")
	k := flags.Int("k", 5, "top-k slices to consider")
	budget := flags.Int("budget", inject.DefaultBudget, "max injection block bytes")
	dbOverride := flags.String("db", "", "database path override")
	zf := addZoneFlags(flags)
	if err := flags.Parse(args); err != nil {
		return usageWrap(err)
	}
	if err := zf.validate(); err != nil {
		return usagef("%v", err)
	}
	if *query == "" && flags.NArg() > 0 {
		*query = flags.Arg(0)
	}
	if *query == "" {
		return usagef("inject: --query is required")
	}

	store, err := deps.openStore(storePath(*dbOverride, *scopeValue))
	if err != nil {
		return err
	}
	idx := deps.newIndex()
	if err := indexFromStore(store, idx); err != nil {
		return err
	}
	scope, err := parseScope(*scopeValue)
	if err != nil {
		return err
	}

	z := zf.zones()
	inj, err := (&inject.Injector{Index: idx, Scope: scope, K: *k, Budget: *budget, Zones: &z}).Build(*query)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s\n", stripESC(inj.Text))
	return nil
}

// storePath resolves the store path for a scope (mirrors search.go).
func storePath(override, scope string) string {
	if override != "" {
		return override
	}
	switch scope {
	case "session":
		return defaultProjectDB()
	case "user":
		return defaultUserDB()
	default:
		return defaultProjectDB()
	}
}

// indexFromStore rebuilds an in-memory index from the persistent store,
// covering every scope (session/project/user).
func indexFromStore(store slice.Store, idx slice.Index) error {
	for _, scope := range []slice.Scope{slice.Session, slice.Project, slice.User} {
		items, err := store.List(scope)
		if err != nil {
			return err
		}
		for _, sl := range items {
			if err := idx.Insert(sl); err != nil {
				return err
			}
		}
	}
	return nil
}
