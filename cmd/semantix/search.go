package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

type searchResult struct {
	ID      string  `json:"id"`
	Type    int     `json:"type"`
	Scope   string  `json:"scope"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

func runSearch(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("search", flag.ContinueOnError)
	flags.SetOutput(stderr)
	queryFlag := flags.String("query", "", "search query (alternatively pass it as positional text)")
	scopeValue := flags.String("scope", "project", "slice scope: session, project, or user")
	limit := flags.Int("limit", 10, "maximum number of results")
	dbOverride := flags.String("db", "", "database path override")
	projectDB := flags.String("project-db", defaultProjectDB(), "project/session database path")
	userDB := flags.String("user-db", defaultUserDB(), "user database path")
	jsonOutput := flags.Bool("json", false, "write JSON results")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than zero")
	}

	query := strings.TrimSpace(*queryFlag)
	if query == "" {
		query = strings.TrimSpace(strings.Join(flags.Args(), " "))
	} else if flags.NArg() != 0 {
		return errors.New("use either --query or positional query text, not both")
	}
	if query == "" {
		return errors.New("query is required")
	}

	scope, err := parseScope(*scopeValue)
	if err != nil {
		return err
	}
	dbPath := selectDB(scope, *dbOverride, *projectDB, *userDB)
	store, err := deps.openStore(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	if store == nil {
		return errors.New("slice store is unavailable; merge the U4 implementation first")
	}
	defer closeStore(store)

	items, err := store.List(scope)
	if err != nil {
		return fmt.Errorf("list %s slices: %w", scopeName(scope), err)
	}
	index := deps.newIndex()
	if index == nil {
		return errors.New("search index is unavailable")
	}
	for i, item := range items {
		if item == nil {
			return fmt.Errorf("store returned nil slice at position %d", i)
		}
		if err := index.Insert(item); err != nil {
			return fmt.Errorf("index slice %q: %w", item.ID, err)
		}
	}

	hits, err := index.Search(query, *limit, scope)
	if err != nil {
		return fmt.Errorf("search index: %w", err)
	}
	results := make([]searchResult, len(hits))
	for i, hit := range hits {
		results[i] = searchResult{
			ID:      hit.Slice.ID,
			Type:    int(hit.Slice.Type),
			Scope:   scopeName(hit.Slice.Scope),
			Content: string(hit.Slice.Content),
			Score:   hit.Score,
		}
	}

	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}
	for i, result := range results {
		content := stripESC(strings.Join(strings.Fields(result.Content), " "))
		fmt.Fprintf(stdout, "%d. score=%.6f id=%s scope=%s\n   %s\n", i+1, result.Score, result.ID, result.Scope, content)
	}
	return nil
}
