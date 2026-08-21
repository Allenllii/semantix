package slice

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- ListAll / Delete (Store contract additions, U26) ---

func TestFileStoreListAllAndDelete(t *testing.T) {
	st, err := NewFileStore(filepath.Join(t.TempDir(), "slices.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	a := &Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("x")}
	b := &Slice{ID: "b", Type: Result, Scope: User, Content: []byte("y")}
	c := &Slice{ID: "c", Type: Context, Scope: Session, Content: []byte("z")}
	for _, s := range []*Slice{a, b, c} {
		if err := st.Put(s); err != nil {
			t.Fatal(err)
		}
	}
	all, err := st.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("ListAll len = %d, want 3", len(all))
	}
	// Delete removes across scopes.
	if err := st.Delete("b"); err != nil {
		t.Fatal(err)
	}
	all, err = st.ListAll()
	if err != nil || len(all) != 2 {
		t.Fatalf("after Delete len = %d (%v), want 2", len(all), err)
	}
	// Deleting a missing id is a no-op, not an error.
	if err := st.Delete("nope"); err != nil {
		t.Fatalf("Delete(missing) = %v, want nil", err)
	}
	all, err = st.ListAll()
	if err != nil || len(all) != 2 {
		t.Fatalf("after no-op Delete len = %d (%v), want 2", len(all), err)
	}
}

// Extractor stamps CreatedAt so retention-based gc has a basis.
func TestExtractorSetsCreatedAt(t *testing.T) {
	transcript := []byte(`{"role":"user","content":"hi"}` + "\n" + `{"role":"assistant","content":"ok"}`)
	got, err := NewExtractor().Extract(transcript, SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected slices")
	}
	for _, s := range got {
		if s.CreatedAt == 0 {
			t.Fatalf("slice %s has zero CreatedAt", s.ID)
		}
	}
}

// --- export / import round-trip ---

func TestExportImportRoundTrip(t *testing.T) {
	st, err := NewFileStore(filepath.Join(t.TempDir(), "slices.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	in := []*Slice{
		{ID: "a", Type: Prompt, Scope: Project, Content: []byte("alpha"), Weight: 0.9, CreatedAt: now - 100, Meta: SliceMeta{SourceSession: "s1", Language: "zh-CN"}},
		{ID: "b", Type: Result, Scope: User, Content: []byte("beta"), Weight: 0.4, CreatedAt: now - 200, Meta: SliceMeta{TaskType: "debug"}},
	}
	for _, s := range in {
		if err := st.Put(s); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	n, skipped, err := Export(st, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("export count = %d, want 2", n)
	}
	if skipped != 0 {
		t.Fatalf("export skipped = %d, want 0", skipped)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("export lines = %d, want 2", len(lines))
	}
	// Each line must be a full Slice JSON including Meta (U26 acceptance).
	var parsed Slice
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("line not valid Slice JSON: %v", err)
	}
	if parsed.Meta.SourceSession != "s1" || parsed.Meta.Language != "zh-CN" {
		t.Fatalf("Meta not exported: %+v", parsed.Meta)
	}

	// Import into a fresh store and verify lossless round-trip.
	dst, err := NewFileStore(filepath.Join(t.TempDir(), "restored.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	imported, skipped, err := Import(dst, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if imported != 2 || skipped != 0 {
		t.Fatalf("import = %d imported, %d skipped; want 2, 0", imported, skipped)
	}
	restored, err := dst.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 2 {
		t.Fatalf("restored len = %d, want 2", len(restored))
	}
	byID := map[string]*Slice{}
	for _, s := range restored {
		byID[s.ID] = s
	}
	for _, want := range in {
		got := byID[want.ID]
		if got == nil {
			t.Fatalf("slice %s missing after import", want.ID)
		}
		if got.Weight != want.Weight || got.CreatedAt != want.CreatedAt || string(got.Content) != string(want.Content) {
			t.Fatalf("slice %s not lossless: %+v", want.ID, got)
		}
		if got.Meta.SourceSession != want.Meta.SourceSession || got.Meta.Language != want.Meta.Language {
			t.Fatalf("slice %s meta not lossless: %+v", want.ID, got.Meta)
		}
	}
}

// Re-importing the same backup converges (idempotent): no duplicates.
func TestImportIdempotent(t *testing.T) {
	src, err := NewFileStore(filepath.Join(t.TempDir(), "src.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := src.Put(&Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, _, err := Export(src, &buf); err != nil {
		t.Fatal(err)
	}

	dst, err := NewFileStore(filepath.Join(t.TempDir(), "dst.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := Import(dst, bytes.NewReader(buf.Bytes())); err != nil {
			t.Fatal(err)
		}
	}
	all, err := dst.ListAll()
	if err != nil || len(all) != 1 {
		t.Fatalf("after double import len = %d (%v), want 1", len(all), err)
	}
}

// Import skips corrupt and ID-less lines without aborting the batch.
func TestImportTolerantSkip(t *testing.T) {
	data := "{not json\n{\"id\":\"ok\",\"type\":0,\"scope\":1,\"content\":\"Z29vZA==\"}\n{\"type\":1,\"scope\":1}\n\n"
	dst, err := NewFileStore(filepath.Join(t.TempDir(), "dst.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	imported, skipped, err := Import(dst, strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if imported != 1 || skipped != 2 {
		t.Fatalf("import = %d imported, %d skipped; want 1, 2", imported, skipped)
	}
	if _, err := dst.Get("ok"); err != nil || imported == 0 {
		t.Fatal("valid line was not imported")
	}
}

// --- gc ---

func mustStore(t *testing.T, path string, items ...*Slice) Store {
	t.Helper()
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range items {
		if err := st.Put(s); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestGCRetentionAndWeight(t *testing.T) {
	now := time.Now().Unix()
	old := now - 10*24*3600 // 10 days ago
	fresh := now - 3600     // 1 hour ago
	st := mustStore(t, filepath.Join(t.TempDir(), "gc.jsonl"),
		&Slice{ID: "old", Type: Prompt, Scope: Project, CreatedAt: old, Weight: 0.9},
		&Slice{ID: "fresh", Type: Prompt, Scope: Project, CreatedAt: fresh, Weight: 0.9},
		&Slice{ID: "low", Type: Result, Scope: User, CreatedAt: fresh, Weight: 0.1},
		&Slice{ID: "unknown", Type: Context, Scope: Session, CreatedAt: 0, Weight: 0.9}, // legacy: never expired by retention
	)

	res, err := GC(st, GCOptions{RetentionDays: 7, MinWeight: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if res.Checked != 4 {
		t.Fatalf("checked = %d, want 4", res.Checked)
	}
	if len(res.Expired) != 1 || res.Expired[0] != "old" {
		t.Fatalf("expired = %v, want [old]", res.Expired)
	}
	if len(res.LowScore) != 1 || res.LowScore[0] != "low" {
		t.Fatalf("low score = %v, want [low]", res.LowScore)
	}
	if res.Removed != 2 {
		t.Fatalf("removed = %d, want 2", res.Removed)
	}
	if got := stGetOrNil(t, st, "old"); got != nil {
		t.Fatal("old slice was not deleted")
	}
	if got := stGetOrNil(t, st, "low"); got != nil {
		t.Fatal("low-score slice was not deleted")
	}
	for _, id := range []string{"fresh", "unknown"} {
		if got := stGetOrNil(t, st, id); got == nil {
			t.Fatalf("slice %s should have been kept", id)
		}
	}
}

// Zero retention + zero weight = bare GC removes nothing.
func TestGCNoCriteriaRemovesNothing(t *testing.T) {
	st := mustStore(t, filepath.Join(t.TempDir(), "gc.jsonl"),
		&Slice{ID: "a", Type: Prompt, Scope: Project, CreatedAt: 1, Weight: 0.01})
	res, err := GC(st, GCOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 0 || res.Checked != 1 {
		t.Fatalf("res = %+v, want 0 removed / 1 checked", res)
	}
	if stGetOrNil(t, st, "a") == nil {
		t.Fatal("slice deleted despite no criteria")
	}
}

// Dry-run reports candidates but deletes nothing.
func TestGCDryRun(t *testing.T) {
	st := mustStore(t, filepath.Join(t.TempDir(), "gc.jsonl"),
		&Slice{ID: "old", Type: Prompt, Scope: Project, CreatedAt: 1, Weight: 0.9})
	res, err := GC(st, GCOptions{RetentionDays: 1, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 || len(res.Expired) != 1 {
		t.Fatalf("dry-run res = %+v, want 1 candidate", res)
	}
	if stGetOrNil(t, st, "old") == nil {
		t.Fatal("dry-run deleted a slice")
	}
}

func stGetOrNil(t *testing.T, st Store, id string) *Slice {
	t.Helper()
	got, err := st.Get(id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	return got
}

// Export output file has perm 0600 (matches store file hardening).
func TestExportPerm(t *testing.T) {
	st := mustStore(t, filepath.Join(t.TempDir(), "src.jsonl"),
		&Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("x")})
	out := filepath.Join(t.TempDir(), "backup.jsonl")
	f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Export(st, f); err != nil {
		t.Fatal(err)
	}
	f.Close()
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no POSIX permission bits (Go reports 0666 regardless of
	// the mode passed to OpenFile), so assert only where they are real.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("export perm = %o, want 600", perm)
		}
	}
}

// A corrupt store line must be surfaced by Export (lossless-backup contract),
// not silently dropped.
func TestExportReportsCorruptStoreLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "src.jsonl")
	if err := os.WriteFile(path, []byte(
		"{\"id\":\"good\",\"type\":0,\"scope\":1,\"content\":\"eA==\"}\n"+
			"{not json\n"+
			"{\"id\":\"also-good\",\"type\":1,\"scope\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	n, skipped, err := Export(st, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("export count = %d, want 2", n)
	}
	if skipped != 1 {
		t.Fatalf("export skipped = %d, want 1 (corrupt line must be surfaced)", skipped)
	}
}

// A store with an oversized line stays readable (ListAll/Export keep working
// and report the skipped line) — the line is treated like any corrupt line.
// Corrupt/oversized lines survive (skipped) until compaction folds the base.
func TestFileStoreToleratesOversizedLine(t *testing.T) {
	big := make([]byte, 9*1024*1024) // > 8 MiB
	for i := range big {
		big[i] = 'x'
	}
	path := filepath.Join(t.TempDir(), "src.jsonl")
	if err := os.WriteFile(path, []byte("{\"id\":\"ok\",\"type\":0,\"scope\":1}\n"+string(big)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	all, err := st.ListAll()
	if err != nil {
		t.Fatalf("store unreadable after oversized line: %v", err)
	}
	if len(all) != 1 || all[0].ID != "ok" {
		t.Fatalf("ListAll = %d slices (%v), want just ok", len(all), all)
	}
	// Export counts the oversized line as skipped (lossless-backup contract).
	var buf bytes.Buffer
	n, skipped, err := Export(st, &buf)
	if err != nil || n != 1 || skipped != 1 {
		t.Fatalf("export = %d, skipped %d, err %v; want 1, 1, nil", n, skipped, err)
	}
	// The store must still accept writes; the rewrite drops the bad line.
	if err := st.Put(&Slice{ID: "new", Type: Prompt, Scope: Project}); err != nil {
		t.Fatalf("Put after oversized line: %v", err)
	}
	all, err = st.ListAll()
	if err != nil || len(all) != 2 {
		t.Fatalf("after Put len = %d (%v), want 2", len(all), err)
	}
}

// One oversized line must not abort the whole import; it is counted as skipped.
func TestImportOversizedLineSkipped(t *testing.T) {
	big := make([]byte, 9*1024*1024) // > 8 MiB scanner cap
	for i := range big {
		big[i] = 'x'
	}
	data := "{\"id\":\"ok\",\"type\":0,\"scope\":1,\"content\":\"Z29vZA==\"}\n" +
		string(big) + "\n" +
		"{\"id\":\"after\",\"type\":1,\"scope\":1}\n"
	dst, err := NewFileStore(filepath.Join(t.TempDir(), "dst.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	imported, skipped, err := Import(dst, strings.NewReader(data))
	if err != nil {
		t.Fatalf("import aborted on oversized line: %v", err)
	}
	if imported != 2 {
		t.Fatalf("imported = %d, want 2 (valid lines before/after oversized must land)", imported)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1 (oversized line)", skipped)
	}
}

// Zero Weight (legacy, never scored) is protected from the score criterion,
// exactly like unknown CreatedAt is protected from retention.
func TestGCProtectsUnknownWeight(t *testing.T) {
	now := time.Now().Unix()
	st := mustStore(t, filepath.Join(t.TempDir(), "gc.jsonl"),
		&Slice{ID: "legacy", Type: Prompt, Scope: Project, CreatedAt: now, Weight: 0}, // unknown weight
		&Slice{ID: "scored-low", Type: Result, Scope: User, CreatedAt: now, Weight: 0.2},
	)
	res, err := GC(st, GCOptions{MinWeight: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 || len(res.LowScore) != 1 || res.LowScore[0] != "scored-low" {
		t.Fatalf("res = %+v, want only scored-low removed", res)
	}
	if stGetOrNil(t, st, "legacy") == nil {
		t.Fatal("legacy zero-weight slice was removed by the score criterion")
	}
	if stGetOrNil(t, st, "scored-low") != nil {
		t.Fatal("scored-low slice was not removed")
	}
}
