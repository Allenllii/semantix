package slice

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func reopen(t *testing.T, path string) Store {
	t.Helper()
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// Journal replay must reproduce the exact in-memory state: put overwrites,
// deletes remove, stat deltas merge — across a close/reopen boundary, with
// the base file never touched before compaction.
func TestJournalReplayAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	if err := st.Put(&Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("one")}); err != nil {
		t.Fatal(err)
	}
	if err := st.Put(&Slice{ID: "b", Type: Result, Scope: Project, Content: []byte("two")}); err != nil {
		t.Fatal(err)
	}
	if err := st.Put(&Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("one-v2"), Weight: 0.5}); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("b"); err != nil {
		t.Fatal(err)
	}
	if base, err := os.ReadFile(path); err != nil || len(base) != 0 {
		t.Fatalf("base rewritten before compaction: %d bytes, err %v", len(base), err)
	}

	st2 := reopen(t, path)
	all, err := st2.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != "a" || string(all[0].Content) != "one-v2" || all[0].Weight != 0.5 {
		t.Fatalf("replayed state wrong: %+v", all)
	}
	if got, _ := st2.Get("b"); got != nil {
		t.Fatalf("deleted slice resurrected: %+v", got)
	}
}

// LastUsed merges by max, counters by accumulation — live and via replay.
func TestJournalStatMaxMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	if err := st.Put(&Slice{ID: "a", Type: Result, Scope: Project, Content: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateStats("a", SliceStats{Hits: 1, LastUsed: 100}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateStats("a", SliceStats{Hits: 2, LastUsed: 50}); err != nil {
		t.Fatal(err)
	}
	check := func(st Store, label string) {
		t.Helper()
		got, err := st.Get("a")
		if err != nil || got == nil {
			t.Fatalf("%s: get: %v %v", label, got, err)
		}
		if got.Stats.Hits != 3 || got.Stats.LastUsed != 100 {
			t.Fatalf("%s: stats = %+v, want Hits 3 LastUsed 100 (max-merge)", label, got.Stats)
		}
	}
	check(st, "live")
	check(reopen(t, path), "replayed")
}

// A torn journal tail (crash mid-append) loses at most the torn record;
// everything before it survives and the open never errors.
func TestJournalTornTailTolerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	if err := st.Put(&Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("keep")}); err != nil {
		t.Fatal(err)
	}
	if err := st.Put(&Slice{ID: "b", Type: Prompt, Scope: Project, Content: []byte("torn")}); err != nil {
		t.Fatal(err)
	}
	if c, ok := st.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	jpath := path + ".journal"
	raw, err := os.ReadFile(jpath)
	if err != nil {
		t.Fatal(err)
	}
	for _, cut := range []int{1, 3, 10, 25} {
		if cut >= len(raw) {
			continue
		}
		if err := os.WriteFile(jpath, raw[:len(raw)-cut], 0o600); err != nil {
			t.Fatal(err)
		}
		st2 := reopen(t, path)
		got, err := st2.Get("a")
		if err != nil || got == nil || string(got.Content) != "keep" {
			t.Fatalf("cut %d: slice before torn record lost: %v %v", cut, got, err)
		}
		// The invariant is all-or-nothing: a record either applies fully
		// (cut=1 only removes the trailing newline, the JSON is intact) or
		// is dropped whole — never half-applied, never an open error.
		if b, _ := st2.Get("b"); b != nil && string(b.Content) != "torn" {
			t.Fatalf("cut %d: torn record half-applied: %+v", cut, b)
		}
	}
}

// An old binary rewriting the base underneath a journal orphans it: on open
// the journal is stashed (bytes kept for forensics), base wins, no replay of
// unknown-ordering records.
func TestJournalOrphanStashed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	st := reopen(t, path)
	if err := st.Put(&Slice{ID: "j", Type: Prompt, Scope: Project, Content: []byte("in-journal")}); err != nil {
		t.Fatal(err)
	}
	if c, ok := st.(interface{ Close() error }); ok {
		_ = c.Close()
	}
	// Simulate an old binary's full rewrite: replace base content (size and
	// mtime change) the way v1 writeAll did.
	line, _ := json.Marshal(&Slice{ID: "base-only", Type: Result, Scope: Project, Content: []byte("v1")})
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	st2 := reopen(t, path)
	all, err := st2.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != "base-only" {
		t.Fatalf("base must win after orphaning, got %+v", all)
	}
	matches, err := filepath.Glob(path + ".journal.stash-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly one stash file, got %v (%v)", matches, err)
	}
	if _, err := os.Stat(path + ".journal"); !os.IsNotExist(err) {
		t.Fatalf("orphaned journal must be moved away, stat err = %v", err)
	}
}

// Unknown ops and a future journal version must never brick the store:
// unknown records are skipped (surfaced through Export's skipped count),
// an unknown version stashes the whole journal.
func TestJournalForwardCompat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	reopen(t, path) // create empty base

	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	hdr, _ := json.Marshal(journalHeader{J: journalVersion, BSize: stat.Size(), BMtime: stat.ModTime().UnixNano()})
	putRec := `{"op":"put","s":{"ID":"ok","Type":0,"Scope":1,"Content":"eA=="}}`
	unknown := `{"op":"vacuum","id":"whatever"}`
	journal := string(hdr) + "\n" + unknown + "\n" + putRec + "\n"
	if err := os.WriteFile(path+".journal", []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	st := reopen(t, path)
	if got, _ := st.Get("ok"); got == nil {
		t.Fatal("valid record after unknown op must still apply")
	}
	var buf bytes.Buffer
	if _, skipped, err := Export(st, &buf); err != nil || skipped != 1 {
		t.Fatalf("unknown op must count as skipped: %d, %v", skipped, err)
	}

	// Future version: stash, do not guess.
	future, _ := json.Marshal(journalHeader{J: 99, BSize: stat.Size(), BMtime: stat.ModTime().UnixNano()})
	if err := os.WriteFile(path+".journal", append(future, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	st2 := reopen(t, path)
	if all, _ := st2.ListAll(); len(all) != 0 {
		t.Fatalf("future-version journal must not replay, got %d slices", len(all))
	}
	if matches, _ := filepath.Glob(path + ".journal.stash-*"); len(matches) != 1 {
		t.Fatalf("future-version journal must be stashed, got %v", matches)
	}
}

// The journal file must carry the same 0600 as the store (secrets discipline).
func TestJournalPerm0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	if err := st.Put(&Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX permission bits on windows")
	}
	info, err := os.Stat(path + ".journal")
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("journal perm = %o, want 600", perm)
	}
}

// Read-only usage must leave zero write side effects: no journal file, base
// mtime untouched (dashboard relies on stat-before-open staying meaningful).
func TestReadOnlyLeavesNoJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	line, _ := json.Marshal(&Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("x")})
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	st := reopen(t, path)
	if _, err := st.Get("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.List(Project); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".journal"); !os.IsNotExist(err) {
		t.Fatalf("read-only ops created a journal: %v", err)
	}
}

// v1 base files stay readable: lowercase keys, "Embedding":null, dense
// vectors, phantom ID-less objects. Reads return Embedding nil (nothing in
// the repo consumes persisted vectors); Export preserves the raw vector
// bytes anyway (lossless backup).
func TestV1BaseCompat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	base := `{"id":"lower","type":0,"scope":1,"content":"Z29vZA=="}` + "\n" +
		`{"ID":"nullemb","Type":2,"Scope":1,"Content":"eA==","Embedding":null,"Stats":{"Hits":4},"Weight":0.7,"created_at":1700000000}` + "\n" +
		`{"ID":"vec","Type":0,"Scope":1,"Content":"eQ==","Embedding":[0,0.25,-0.5],"Meta":{"embed_model":"m","embed_dim":3}}` + "\n" +
		`{"phantom":"no id here"}` + "\n"
	if err := os.WriteFile(path, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	st := reopen(t, path)
	all, err := st.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("ListAll = %d, want 3 (phantom skipped)", len(all))
	}
	byID := map[string]*Slice{}
	for _, sl := range all {
		byID[sl.ID] = sl
		if sl.Embedding != nil {
			t.Fatalf("%s: reads must return Embedding nil, got %d floats", sl.ID, len(sl.Embedding))
		}
	}
	if got := byID["nullemb"]; got == nil || got.Stats.Hits != 4 || got.Weight != 0.7 || got.CreatedAt != 1700000000 {
		t.Fatalf("v1 fields lost: %+v", byID["nullemb"])
	}
	if got := byID["vec"]; got == nil || got.Meta.EmbedModel != "m" || got.Meta.EmbedDim != 3 {
		t.Fatalf("meta lost: %+v", byID["vec"])
	}

	var buf bytes.Buffer
	n, skipped, err := Export(st, &buf)
	if err != nil || n != 3 || skipped != 1 {
		t.Fatalf("export = %d/%d/%v, want 3 lines, 1 skipped", n, skipped, err)
	}
	if !strings.Contains(buf.String(), `"Embedding":[0,0.25,-0.5]`) {
		t.Fatalf("export lost raw vector bytes:\n%s", buf.String())
	}
}

// Batch stats == sequential stats; missing IDs are skipped, not errors.
func TestUpdateStatsBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	for _, id := range []string{"a", "b"} {
		if err := st.Put(&Slice{ID: id, Type: Result, Scope: Project, Content: []byte(id)}); err != nil {
			t.Fatal(err)
		}
	}
	deltas := map[string]SliceStats{
		"a":       {Hits: 2, LastUsed: 10},
		"b":       {Injected: 1, LastUsed: 20},
		"missing": {Hits: 9},
	}
	if err := ApplyStats(st, deltas); err != nil {
		t.Fatal(err)
	}
	a, _ := st.Get("a")
	b, _ := st.Get("b")
	if a.Stats.Hits != 2 || a.Stats.LastUsed != 10 || b.Stats.Injected != 1 || b.Stats.LastUsed != 20 {
		t.Fatalf("batch stats wrong: a=%+v b=%+v", a.Stats, b.Stats)
	}
	// Replay agreement.
	st2 := reopen(t, path)
	a2, _ := st2.Get("a")
	if a2.Stats.Hits != 2 || a2.Stats.LastUsed != 10 {
		t.Fatalf("replayed batch stats wrong: %+v", a2.Stats)
	}
}

// Callers may mutate their inputs and outputs freely; the store's memory
// must stay isolated (v1 re-parsed the file per call, so every caller
// historically got fresh objects).
func TestStoreIsolatesCallerMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	in := &Slice{ID: "a", Type: Result, Scope: Project, Content: []byte("orig"),
		Meta: SliceMeta{Deps: map[string]string{"f": "h1"}, Mtimes: map[string]int64{"f": 1}}}
	if err := st.Put(in); err != nil {
		t.Fatal(err)
	}
	in.Content[0] = 'X'
	in.Meta.Deps["f"] = "tampered"
	in.Meta.Mtimes["f"] = 99

	out, _ := st.Get("a")
	if string(out.Content) != "orig" || out.Meta.Deps["f"] != "h1" || out.Meta.Mtimes["f"] != 1 {
		t.Fatalf("input mutation leaked into store: %+v", out)
	}
	out.Content[0] = 'Y'
	out.Meta.Deps["f"] = "again"
	out2, _ := st.Get("a")
	if string(out2.Content) != "orig" || out2.Meta.Deps["f"] != "h1" {
		t.Fatalf("output mutation leaked into store: %+v", out2)
	}
}
