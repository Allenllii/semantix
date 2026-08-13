package promote

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func newTestFileStore(t *testing.T) (Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "promote.db")
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return st, path
}

func sampleEntries() []Entry {
	return []Entry{
		{SourceSliceID: "s1", ContentVersion: "v1", Query: "q1", PromotedAt: 1},
		{SourceSliceID: "s1", ContentVersion: "v2", Query: "q2", PromotedAt: 2},
		{SourceSliceID: "s2", ContentVersion: "v1", Query: "q3", PromotedAt: 3},
	}
}

func TestFileStorePutListDelete(t *testing.T) {
	st, _ := newTestFileStore(t)
	for _, e := range sampleEntries() {
		if err := st.Put(e); err != nil {
			t.Fatal(err)
		}
	}
	// Duplicate put (same triple) is a no-op.
	if err := st.Put(sampleEntries()[0]); err != nil {
		t.Fatal(err)
	}
	list, err := st.List("s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list(s1) len = %d, want 2 (dedup by triple)", len(list))
	}
	// Delete removes the exact triple only.
	if err := st.Delete("s1", "v1", "q1"); err != nil {
		t.Fatal(err)
	}
	list, _ = st.List("s1")
	if len(list) != 1 || list[0].ContentVersion != "v2" {
		t.Fatalf("after delete list(s1) = %+v, want only v2", list)
	}
	// Deleting a missing triple is a no-op, not an error.
	if err := st.Delete("s1", "missing", "q"); err != nil {
		t.Fatal(err)
	}
}

// TestFileStorePersistRoundTrip: put entries, reopen the store from the same
// path, and confirm every entry survives (cross-process persistence).
func TestFileStorePersistRoundTrip(t *testing.T) {
	_, path := newTestFileStore(t)
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range sampleEntries() {
		if err := st.Put(e); err != nil {
			t.Fatal(err)
		}
	}
	// Reopen from disk (fresh Store instance = fresh process state).
	st2, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"s1": 2, "s2": 1}
	for id, n := range want {
		got, err := st2.List(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != n {
			t.Fatalf("reopened list(%s) len = %d, want %d (persisted)", id, len(got), n)
		}
	}
	// Entries match the originals exactly (round-trip fidelity).
	orig := map[string][]Entry{}
	for _, e := range sampleEntries() {
		orig[e.SourceSliceID] = append(orig[e.SourceSliceID], e)
	}
	for id, es := range orig {
		got, _ := st2.List(id)
		for i := range es {
			if got[i] != es[i] {
				t.Fatalf("reopened entry[%d] = %+v, want %+v", i, got[i], es[i])
			}
		}
	}
}

// TestFileStoreRestartRecovery: put, mutate (delete one), reopen again —
// the store reflects the last durable state, including deletions.
func TestFileStoreRestartRecovery(t *testing.T) {
	_, path := newTestFileStore(t)
	st, _ := NewFileStore(path)
	for _, e := range sampleEntries() {
		_ = st.Put(e)
	}
	if err := st.Delete("s2", "v1", "q3"); err != nil {
		t.Fatal(err)
	}
	st2, _ := NewFileStore(path)
	if list, _ := st2.List("s2"); len(list) != 0 {
		t.Fatalf("s2 entries after restart = %d, want 0 (delete persisted)", len(list))
	}
	if list, _ := st2.List("s1"); len(list) != 2 {
		t.Fatalf("s1 entries after restart = %d, want 2", len(list))
	}
}

// TestFileStoreConcurrentWrites exercises Put/Delete under -race from
// multiple goroutines; no data loss and no panic.
func TestFileStoreConcurrentWrites(t *testing.T) {
	st, _ := newTestFileStore(t)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for n := 0; n < 25; n++ {
				q := strings.Repeat("q", g+1) + fmt.Sprint(n)
				if err := st.Put(Entry{SourceSliceID: "s", ContentVersion: "v", Query: q, PromotedAt: int64(n)}); err != nil {
					t.Errorf("put: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	list, err := st.List("s")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("concurrent puts must be recorded")
	}
	// Reopen and confirm the file is still fully decodable.
	st2, _ := NewFileStore(st.(*fileStore).path)
	if list2, _ := st2.List("s"); len(list2) != len(list) {
		t.Fatalf("reopened count = %d, want %d (no lost writes)", len(list2), len(list))
	}
}

// TestFileStoreToleratesCorruptLines: a corrupt line in the middle of the
// store must be skipped, not fatal (partial write / hand-edit resilience).
func TestFileStoreToleratesCorruptLines(t *testing.T) {
	_, path := newTestFileStore(t)
	st, _ := NewFileStore(path)
	for _, e := range sampleEntries() {
		_ = st.Put(e)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"broken\": not json\n\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	st2, _ := NewFileStore(path)
	want := map[string]int{"s1": 2, "s2": 1}
	for id, n := range want {
		got, err := st2.List(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != n {
			t.Fatalf("after corrupt line list(%s) len = %d, want %d", id, len(got), n)
		}
	}
}

// TestFileStorePerm0600 asserts the store file is not world-readable.
// Windows does not implement Unix permission bits meaningfully (os.Chmod is
// a no-op for mode bits), so the assertion runs on Unix only.
func TestFileStorePerm0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on windows")
	}
	st, path := newTestFileStore(t)
	_ = st.Put(Entry{SourceSliceID: "s", ContentVersion: "v", Query: "q"})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("store file perm = %o, want 600", perm)
	}
	// A reopened store must not widen permissions either.
	st2, _ := NewFileStore(path)
	_ = st2.Put(Entry{SourceSliceID: "s", ContentVersion: "v2", Query: "q2"})
	info, _ = os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("store file perm after rewrite = %o, want 600", perm)
	}
}

// TestCascadeInvalidateFileStore: cascade semantics against the disk store —
// idempotent, version-scoped, survives restart.
func TestCascadeInvalidateFileStore(t *testing.T) {
	_, path := newTestFileStore(t)
	st, _ := NewFileStore(path)
	old := []byte("old answer")
	new := []byte("new answer")
	for _, e := range []Entry{
		{SourceSliceID: "s1", ContentVersion: ContentVersion(old), Query: "q1", PromotedAt: 1},
		{SourceSliceID: "s1", ContentVersion: ContentVersion(old), Query: "q2", PromotedAt: 2},
		{SourceSliceID: "s2", ContentVersion: ContentVersion(new), Query: "q3", PromotedAt: 3},
	} {
		_ = st.Put(e)
	}
	n, err := CascadeInvalidate(st, "s1", new)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("invalidated = %d, want 2", n)
	}
	if list, _ := st.List("s1"); len(list) != 0 {
		t.Fatal("s1 entries must all be gone")
	}
	if list, _ := st.List("s2"); len(list) != 1 {
		t.Fatal("s2 entry must survive (version matches)")
	}
	if n, _ := CascadeInvalidate(st, "s1", new); n != 0 {
		t.Fatalf("second cascade invalidated %d, want 0 (idempotent)", n)
	}
	// Deletions are durable across restart.
	st2, _ := NewFileStore(path)
	if list, _ := st2.List("s1"); len(list) != 0 {
		t.Fatal("cascade deletions must survive restart")
	}
	if list, _ := st2.List("s2"); len(list) != 1 {
		t.Fatal("surviving entry must survive restart")
	}
}
