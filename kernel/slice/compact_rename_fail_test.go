package slice

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestCompactBaseRenameFailurePreservesJournal is the Issue #367 regression:
// compactLocked retires the journal fd before the base-swap rename. If that
// rename fails in-process (disk full, permissions, quota — not a crash), the
// old journal still holds live, un-folded records. The bug was that s.jw was
// left nil, so the next write called ensureJournalLocked and replaced the still-
// valid journal with a fresh empty one, permanently losing those records. The
// fix reopens the existing journal on the rename-error path.
func TestCompactBaseRenameFailurePreservesJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	st := reopen(t, path)
	for i := 0; i < 5; i++ {
		if err := st.Put(&Slice{ID: fmt.Sprintf("s%02d", i), Type: Result, Scope: Project,
			Content: []byte(fmt.Sprintf("content-%d", i))}); err != nil {
			t.Fatal(err)
		}
	}
	fs := st.(*fileStore)

	// Inject a failure at exactly the compaction base-swap rename.
	orig := osRename
	osRename = func(string, string) error { return fmt.Errorf("injected rename failure") }
	err := fs.Compact()
	osRename = orig
	if err == nil {
		t.Fatal("expected Compact to fail with the injected rename error")
	}

	// A write after the failed compaction must land durably in the recovered
	// journal, not a fresh empty one that clobbers the old records.
	if err := st.Put(&Slice{ID: "after", Type: Result, Scope: Project, Content: []byte("x")}); err != nil {
		t.Fatalf("write after failed compaction: %v", err)
	}

	// Reopen from disk: every record must survive.
	st2 := reopen(t, path)
	all, err := st2.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]bool, len(all))
	for _, s := range all {
		ids[s.ID] = true
	}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("s%02d", i)
		if !ids[id] {
			t.Fatalf("record %s lost after failed compaction + write + reopen (Issue #367)", id)
		}
	}
	if !ids["after"] {
		t.Fatal("post-failure write lost after reopen")
	}
}
