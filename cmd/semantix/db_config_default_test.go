package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/config"
	"semantix/kernel/slice"
)

// Issue #221: export/import/gc's --db default must read config store.db (like
// lookup/verify), not the hardcoded "" that always falls back to
// .semantix/project.db. Each test configures store.db to a custom path, runs
// the command WITHOUT --db, and asserts the command operated on the configured
// store. t.Chdir isolates the default project.db to an empty temp dir so a
// regression (default "") fails instead of silently hitting a stray project.db.

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

func TestExportReadsConfiguredDB(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	customDB := filepath.Join(dir, "custom.db")
	seedStore(t, customDB, "修复 go 测试", "加 CI 配置", "补单元测试")

	var out, errOut bytes.Buffer
	if err := runExport([]string{"--output", "-"}, &out, &errOut, configDBDeps(customDB)); err != nil {
		t.Fatalf("export: %v (stderr: %s)", err, errOut.String())
	}
	lines := 0
	for _, ln := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.TrimSpace(ln) != "" {
			lines++
		}
	}
	if lines != 3 {
		t.Fatalf("exported %d JSONL lines, want 3 — export did not read the configured store.db", lines)
	}
}

func TestImportReadsConfiguredDB(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Build a 2-slice backup from a source store.
	srcDB := filepath.Join(dir, "src.db")
	seedStore(t, srcDB, "会话 A 切片", "会话 B 切片")
	backup := filepath.Join(dir, "backup.jsonl")
	src, err := slice.NewFileStore(srcDB)
	if err != nil {
		t.Fatal(err)
	}
	bf, err := os.Create(backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := slice.Export(src, bf); err != nil {
		t.Fatal(err)
	}
	bf.Close()
	closeStore(src)

	customDB := filepath.Join(dir, "custom.db")
	var out, errOut bytes.Buffer
	if err := runImport([]string{"--input", backup}, &out, &errOut, configDBDeps(customDB)); err != nil {
		t.Fatalf("import: %v (stderr: %s)", err, errOut.String())
	}

	// The configured store.db must now hold the imported slices; the default
	// project.db (in this temp cwd) must not have been touched.
	restored, err := slice.NewFileStore(customDB)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(restored)
	all, err := restored.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(all); got != 2 {
		t.Fatalf("configured store has %d slices after import, want 2 — import did not read the configured store.db", got)
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
