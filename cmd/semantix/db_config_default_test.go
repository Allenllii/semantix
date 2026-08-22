package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/config"
	"semantix/kernel/slice"
)

// Issue #221: gc's --db default must read config store.db (like lookup/verify),
// not the hardcoded "" that always falls back to .semantix/project.db. The test
// configures store.db to a custom path, runs gc WITHOUT --db, and asserts it
// operated on the configured store. t.Chdir isolates the default project.db to
// an empty temp dir so a regression (default "") fails instead of silently
// hitting a stray project.db.

func configDBDeps(customDB string) dependencies {
	return dependencies{
		newExtractor: slice.NewExtractor,
		openStore:    slice.NewFileStore,
		newIndex:     func() slice.Index { return bm25.New() },
		resolved: &config.Resolved{Fields: []config.Field{
			{Key: "store.db", Value: customDB, Source: config.SourceFile},
		}},
	}
}

func seedStore(t *testing.T, path string, contents ...string) {
	t.Helper()
	store, err := slice.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(store)
	for i, c := range contents {
		sl := &slice.Slice{ID: fmt.Sprintf("s%d", i), Type: slice.Prompt, Scope: slice.Project, Content: []byte(c)}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCReadsConfiguredDB(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	customDB := filepath.Join(dir, "custom.db")
	seedStore(t, customDB, "切片一", "切片二", "切片三")

	var out, errOut bytes.Buffer
	// --dry-run with an unreachable min-weight checks every slice, deletes none.
	if err := runGC([]string{"--dry-run", "--min-weight", "1000000", "--json"}, &out, &errOut, configDBDeps(customDB)); err != nil {
		t.Fatalf("gc: %v (stderr: %s)", err, errOut.String())
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out.String())
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data is not an object: %#v", env.Data)
	}
	if checked, _ := data["checked"].(float64); checked != 3 {
		t.Fatalf("gc checked=%v slices, want 3 — gc did not read the configured store.db", data["checked"])
	}
}
