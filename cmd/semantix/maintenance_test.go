package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"semantix/kernel/slice"
)

// buildMaintenanceDeps returns deps wired to a real fileStore on a temp path.
func buildMaintenanceDeps(t *testing.T) (dependencies, string) {
	t.Helper()
	db := filepath.Join(t.TempDir(), "library.jsonl")
	deps := dependencies{
		newExtractor: func() slice.Extractor { return nil },
		openStore:    func(path string) (slice.Store, error) { return slice.NewFileStore(path) },
		newIndex:     func() slice.Index { return nil },
	}
	return deps, db
}

func TestGCCLI(t *testing.T) {
	deps, db := buildMaintenanceDeps(t)
	old := int64(1) // ancient
	if err := os.WriteFile(db, []byte(
		"{\"id\":\"old\",\"type\":0,\"scope\":1,\"created_at\":"+strconv.FormatInt(old, 10)+",\"weight\":0.9}\n"+
			"{\"id\":\"low\",\"type\":1,\"scope\":1,\"created_at\":9999999999,\"weight\":0.1}\n"+
			"{\"id\":\"keep\",\"type\":0,\"scope\":1,\"created_at\":9999999999,\"weight\":0.9}\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}

	// Dry-run removes nothing but reports candidates. --no-rescore keeps the
	// handcrafted fixture weights authoritative (a rescore would replace
	// them with computed values, which is its own test).
	var stdout, stderr bytes.Buffer
	code := run([]string{"gc", "--retention-days", "7", "--min-weight", "0.5", "--no-rescore", "--dry-run", "--db", db}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("gc dry-run code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "would_remove=2") {
		t.Fatalf("gc dry-run stdout = %q", stdout.String())
	}
	store := openTestStore(t, db)
	if all, _ := store.ListAll(); len(all) != 3 {
		t.Fatalf("dry-run removed slices: len = %d", len(all))
	}

	// Real run removes old + low, keeps the rest.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"gc", "--retention-days", "7", "--min-weight", "0.5", "--no-rescore", "--db", db}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("gc code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "removed=2") {
		t.Fatalf("gc stdout = %q", stdout.String())
	}
	// Re-open: a store handle is a snapshot of open time (freeze-window
	// semantics); the gc ran in its own handle, so observe like a fresh
	// process would.
	store = openTestStore(t, db)
	items, err := store.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "keep" {
		t.Fatalf("after gc got %d slices: %v", len(items), items)
	}
}

func TestGCJSONEnvelope(t *testing.T) {
	deps, db := buildMaintenanceDeps(t)
	if err := os.WriteFile(db, []byte("{\"id\":\"old\",\"type\":0,\"scope\":1,\"created_at\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"gc", "--retention-days", "1", "--db", db, "--json"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("gc --json code = %d, stderr = %q", code, stderr.String())
	}
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v", err)
	}
	if !env.OK || env.Command != "gc" || env.Error != nil {
		t.Fatalf("envelope = %+v", env)
	}
	data := env.Data.(map[string]interface{})
	if data["removed"].(float64) != 1 || data["dry_run"].(bool) != false {
		t.Fatalf("data = %v", data)
	}
}

// Usage errors exit 2 (U19 §4.3) for the new commands — both post-parse
// validation and flag-parse failures (unknown flag / bad value).
func TestMaintenanceUsageErrorsExit2(t *testing.T) {
	deps, _ := buildMaintenanceDeps(t)
	cases := [][]string{
		{"gc", "--retention-days", "-5"},  // negative retention
		{"gc", "--bogus-flag"},            // unknown flag: parse failure
		{"gc", "--retention-days", "abc"}, // bad flag value: parse failure
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr, deps); code != 2 {
			t.Fatalf("run(%v) code = %d, want 2; stderr = %q", args, code, stderr.String())
		}
	}
}

// A usage error under --json still emits the §4.2 failure envelope (ok:false,
// error.code 2) so a JSON consumer gets parseable output.
func TestMaintenanceUsageErrorJSONEnvelope(t *testing.T) {
	deps, _ := buildMaintenanceDeps(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"gc", "--retention-days", "-5", "--json"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr = %q", code, stderr.String())
	}
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("failure envelope not valid JSON: %v\n%s", err, stdout.String())
	}
	if env.OK || env.Command != "gc" || env.Error == nil || env.Error.Code != 2 {
		t.Fatalf("failure envelope = %+v", env)
	}
}

// Flag-parse failure under --json also emits the failure envelope.
func TestMaintenanceFlagParseErrorJSONEnvelope(t *testing.T) {
	deps, _ := buildMaintenanceDeps(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"gc", "--bogus-flag", "--json"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr = %q", code, stderr.String())
	}
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("failure envelope not valid JSON: %v\n%s", err, stdout.String())
	}
	if env.OK || env.Error == nil || env.Error.Code != 2 {
		t.Fatalf("failure envelope = %+v", env)
	}
}

// Runtime failures (missing db dir) exit 1 and, with --json, emit ok:false.
func TestMaintenanceRuntimeErrorExit1(t *testing.T) {
	deps, _ := buildMaintenanceDeps(t)
	db := filepath.Join(t.TempDir(), "no", "such", "dir", "lib.jsonl")
	var stdout, stderr bytes.Buffer
	code := run([]string{"gc", "--db", db, "--json"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("gc code = %d, want 1; stderr = %q", code, stderr.String())
	}
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("failure envelope not valid JSON: %v\n%s", err, stdout.String())
	}
	if env.OK || env.Error == nil || env.Error.Code != 1 {
		t.Fatalf("failure envelope = %+v", env)
	}
}

// The gc JSON envelope carries the typed eviction distribution (Issue #277)
// alongside the evicted id list; the text path prints a by_type summary.
func TestGCJSONEnvelopeEvictedByType(t *testing.T) {
	deps, db := buildMaintenanceDeps(t)
	// 3 stale Result slices (type 3) + 1 stale Context slice (type 1),
	// all past retention; cap 2 keeps only the Context (type priority) and
	// the two Results are evicted.
	now := time.Now().Unix()
	line := func(id string, typ int) string {
		return fmt.Sprintf("{\"id\":%q,\"type\":%d,\"scope\":1,\"created_at\":%d}\n", id, typ, now-48*3600)
	}
	if err := os.WriteFile(db, []byte(
		line("r1", 3)+line("r2", 3)+line("r3", 3)+line("c1", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"gc", "--max-slices", "2", "--db", db, "--json"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("gc --json code = %d, stderr = %q", code, stderr.String())
	}
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v", err)
	}
	data := env.Data.(map[string]interface{})
	byType, ok := data["evicted_by_type"].(map[string]interface{})
	if !ok {
		t.Fatalf("evicted_by_type missing or wrong shape: %v", data["evicted_by_type"])
	}
	if byType["result"] != float64(2) || len(byType) != 1 {
		t.Fatalf("evicted_by_type = %v, want {result: 2} (Context survives by type priority)", byType)
	}

	// Text path: the by_type summary line lists types deterministically.
	// Fresh library — the JSON run above already consumed its fixture.
	deps2, db2 := buildMaintenanceDeps(t)
	if err := os.WriteFile(db2, []byte(
		line("r1", 3)+line("r2", 3)+line("r3", 3)+line("c1", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"gc", "--max-slices", "2", "--db", db2}, &stdout, &stderr, deps2)
	if code != 0 {
		t.Fatalf("gc text code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "by_type result:2") {
		t.Fatalf("text output missing by_type summary:\n%s", stdout.String())
	}
}
