package slice

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func sortSlices(all []*Slice) {
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
}

// Compaction is an identity fold: the visible state before and after must be
// field-for-field equal, the base must become plain v1 JSONL (every line
// independently parseable), and the journal must shrink to a bare header.
func TestCompactionEquivalence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	for i := 0; i < 20; i++ {
		if err := st.Put(&Slice{ID: fmt.Sprintf("s%02d", i), Type: Result, Scope: Project,
			Content: []byte(fmt.Sprintf("content-%d", i)), Weight: float64(i) / 20}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Delete("s03"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateStats("s05", SliceStats{Hits: 7, LastUsed: 42}); err != nil {
		t.Fatal(err)
	}
	before, err := st.ListAll()
	if err != nil {
		t.Fatal(err)
	}

	fs := st.(*fileStore)
	if err := fs.Compact(); err != nil {
		t.Fatal(err)
	}
	after, err := st.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	sortSlices(before)
	sortSlices(after)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("compaction changed visible state:\nbefore %+v\nafter  %+v", before, after)
	}

	// Base: every line a self-contained v1 slice; deleted one gone.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	if len(lines) != 19 {
		t.Fatalf("base lines = %d, want 19", len(lines))
	}
	for _, line := range lines {
		var sl Slice
		if err := json.Unmarshal(line, &sl); err != nil || sl.ID == "" {
			t.Fatalf("base line not independently parseable: %q (%v)", line, err)
		}
		if sl.ID == "s03" {
			t.Fatal("deleted slice survived compaction in base")
		}
		if sl.ID == "s05" && (sl.Stats.Hits != 7 || sl.Stats.LastUsed != 42) {
			t.Fatalf("stats not folded into base: %+v", sl.Stats)
		}
	}

	// Journal: header only.
	jraw, err := os.ReadFile(path + ".journal")
	if err != nil {
		t.Fatal(err)
	}
	jlines := bytes.Split(bytes.TrimSpace(jraw), []byte("\n"))
	if len(jlines) != 1 {
		t.Fatalf("journal lines after compaction = %d, want 1 (header)", len(jlines))
	}

	// And a fresh open agrees with the folded state.
	again, err := reopen(t, path).ListAll()
	if err != nil {
		t.Fatal(err)
	}
	sortSlices(again)
	if !reflect.DeepEqual(before, again) {
		t.Fatal("state after reopen of compacted store diverges")
	}
}

// The geometric trigger keeps total rewrite work O(n): 5000 puts must fold
// only a handful of times (each fold is preceded by >= liveCount appends).
// Deterministic counter assertion, no timing.
func TestCompactionGeometricBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	fs := st.(*fileStore)
	const n = 5000
	for i := 0; i < n; i++ {
		if err := st.Put(&Slice{ID: fmt.Sprintf("s%04d", i), Type: Prompt, Scope: Project,
			Content: []byte("x")}); err != nil {
			t.Fatal(err)
		}
	}
	if fs.compactions == 0 {
		t.Fatal("trigger never fired over 5000 puts")
	}
	// log2(5000/1024) ≈ 2.3 → at most a few folds; 6 is a generous ceiling
	// that still fails hard if the trigger degrades to per-write rewrites.
	if fs.compactions > 6 {
		t.Fatalf("compactions = %d over %d puts — trigger is not geometric", fs.compactions, n)
	}
}

// Compaction is where corrupt/oversized base lines are finally dropped
// (pre-compaction they are skipped-but-preserved; Export surfaces the count).
func TestCompactionDropsCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	big := make([]byte, 9*1024*1024)
	for i := range big {
		big[i] = 'x'
	}
	base := `{"id":"ok","type":0,"scope":1,"content":"eA=="}` + "\n" +
		`{corrupt` + "\n" + string(big) + "\n"
	if err := os.WriteFile(path, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	st := reopen(t, path)
	var buf bytes.Buffer
	if _, skipped, err := Export(st, &buf); err != nil || skipped != 2 {
		t.Fatalf("pre-compaction skipped = %d (%v), want 2", skipped, err)
	}
	if err := st.(*fileStore).Compact(); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if n, skipped, err := Export(st, &buf); err != nil || n != 1 || skipped != 0 {
		t.Fatalf("post-compaction export = %d/%d/%v, want 1/0/nil", n, skipped, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("{corrupt")) || len(raw) > 4096 {
		t.Fatalf("corrupt/oversized bytes survived compaction (%d bytes)", len(raw))
	}
}

// CompactWith lets the rescore hook mutate weights and evict by omission,
// while raw embedding bytes survive for kept slices (the hook only ever sees
// Embedding=nil clones).
func TestCompactWithRescore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	base := `{"ID":"vec","Type":0,"Scope":1,"Content":"eA==","Embedding":[0,0.5,-0.25]}` + "\n" +
		`{"ID":"evict","Type":0,"Scope":1,"Content":"eQ=="}` + "\n"
	if err := os.WriteFile(path, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	st := reopen(t, path)
	err := st.(*fileStore).CompactWith(func(all []*Slice) []*Slice {
		kept := all[:0]
		for _, sl := range all {
			if sl.Embedding != nil {
				t.Errorf("rescore hook must see Embedding=nil, got %v", sl.Embedding)
			}
			if sl.ID == "evict" {
				continue
			}
			sl.Weight = 0.42
			kept = append(kept, sl)
		}
		return kept
	})
	if err != nil {
		t.Fatal(err)
	}
	all, err := st.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != "vec" || all[0].Weight != 0.42 {
		t.Fatalf("rescore result wrong: %+v", all)
	}
	var buf bytes.Buffer
	if _, _, err := Export(st, &buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"Embedding":[0,0.5,-0.25]`)) {
		t.Fatalf("kept slice lost its raw vector through CompactWith:\n%s", buf.String())
	}
	if bytes.Contains(buf.Bytes(), []byte("evict")) {
		t.Fatal("evicted slice survived")
	}
}

// A clean store (no journal, no corrupt lines, nothing to fold) must be a
// true no-op: same base bytes, still no journal file.
func TestCompactNoOpOnCleanStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	line, _ := json.Marshal(&Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("x")})
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st := reopen(t, path)
	if err := st.(*fileStore).Compact(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("no-op compaction rewrote the base")
	}
	if _, err := os.Stat(path + ".journal"); !os.IsNotExist(err) {
		t.Fatal("no-op compaction created a journal")
	}
}

// After gc-style folding, an old (v1) reader sees exactly the live state —
// the documented downgrade path. Simulated with a v1-equivalent line parse.
func TestCompactionDowngradePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	for _, id := range []string{"a", "b", "c"} {
		if err := st.Put(&Slice{ID: id, Type: Result, Scope: Project, Content: []byte(id)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Delete("b"); err != nil {
		t.Fatal(err)
	}
	if err := st.(*fileStore).Compact(); err != nil {
		t.Fatal(err)
	}
	// v1 reader = plain line-by-line Unmarshal of the base.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var ids []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var sl Slice
		if err := json.Unmarshal(sc.Bytes(), &sl); err != nil {
			t.Fatalf("v1 reader failed on compacted base: %v", err)
		}
		ids = append(ids, sl.ID)
	}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"a", "c"}) {
		t.Fatalf("v1 view = %v, want [a c]", ids)
	}
}
