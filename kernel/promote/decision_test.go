package promote

import (
	"path/filepath"
	"testing"
)

func newDecision(t *testing.T, ttl, limit int64) (*Decision, *MemStore, *MemRejectionStore) {
	t.Helper()
	entries := NewMemStore()
	rej := NewMemRejectionStore()
	return NewDecision(entries, rej, ttl, limit), entries, rej
}

// Lookup hits only on (query, version) match inside the TTL window.
func TestDecisionLookupVersionAndTTL(t *testing.T) {
	d, _, _ := newDecision(t, 100, 0)
	v1 := ContentVersion([]byte("answer v1"))
	v2 := ContentVersion([]byte("answer v2"))
	if err := d.Promote(Entry{SourceSliceID: "s1", ContentVersion: v1, Query: "q1", PromotedAt: 10}); err != nil {
		t.Fatal(err)
	}
	if !d.Lookup("s1", "q1", v1, 50) {
		t.Fatal("same query+version must hit")
	}
	if d.Lookup("s1", "q1", v2, 50) {
		t.Fatal("stale version must miss (cascade semantics)")
	}
	if d.Lookup("s1", "q2", v1, 50) {
		t.Fatal("different query must miss")
	}
	// TTL expiry: promoted at 10, window 100 → expired by 150.
	if d.Lookup("s1", "q1", v1, 150) {
		t.Fatal("expired entry must miss")
	}
}

// Blacklist: rejectLimit rejections block Promote with ErrBlacklisted.
func TestDecisionBlacklistBlocksPromote(t *testing.T) {
	d, _, _ := newDecision(t, 1000, 2)
	v1 := ContentVersion([]byte("a"))
	d.Rejected("s1", "q1", "judge_declined", 10)
	if err := d.Promote(Entry{SourceSliceID: "s1", ContentVersion: v1, Query: "q1", PromotedAt: 20}); err != nil {
		t.Fatalf("below-limit promote must succeed, got %v", err)
	}
	d.Rejected("s1", "q1", "judge_declined", 30)
	if !d.Blacklisted("s1", "q1", 40) {
		t.Fatal("second rejection must blacklist")
	}
	if err := d.Promote(Entry{SourceSliceID: "s1", ContentVersion: v1, Query: "q1", PromotedAt: 40}); err != ErrBlacklisted {
		t.Fatalf("blacklisted promote must fail with ErrBlacklisted, got %v", err)
	}
	// Other candidates unaffected.
	if err := d.Promote(Entry{SourceSliceID: "s2", ContentVersion: v1, Query: "q1", PromotedAt: 40}); err != nil {
		t.Fatalf("unrelated candidate must promote, got %v", err)
	}
}

// Rejection expiry: old rejections stop counting (bounded memory, no
// permanent blacklist injustice).
func TestDecisionRejectionExpiry(t *testing.T) {
	d, _, _ := newDecision(t, 100, 2)
	d.Rejected("s1", "q1", "judge_declined", 10)
	d.Rejected("s1", "q1", "judge_declined", 20)
	if !d.Blacklisted("s1", "q1", 30) {
		t.Fatal("two in-window rejections must blacklist")
	}
	// After the window (ttl 100 from now=30 → 130), count resets.
	if d.Blacklisted("s1", "q1", 200) {
		t.Fatal("expired rejections must not blacklist")
	}
	if err := d.Promote(Entry{SourceSliceID: "s1", ContentVersion: ContentVersion([]byte("a")), Query: "q1", PromotedAt: 200}); err != nil {
		t.Fatalf("post-expiry promote must succeed, got %v", err)
	}
}

// rejectLimit 0 disables the blacklist entirely.
func TestDecisionBlacklistDisabled(t *testing.T) {
	d, _, _ := newDecision(t, 1000, 0)
	for i := 0; i < 5; i++ {
		d.Rejected("s1", "q1", "judge_declined", int64(10+i))
	}
	if d.Blacklisted("s1", "q1", 100) {
		t.Fatal("rejectLimit 0 must never blacklist")
	}
}

// The file-backed rejection store persists and survives restart, expires
// lazily and tolerates corrupt lines.
func TestRejectionFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rejections.jsonl")
	fs, err := NewRejectionFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Add(Rejection{SourceSliceID: "s1", Query: "q1", Reason: "judge_declined", RejectedAt: 10}); err != nil {
		t.Fatal(err)
	}
	if err := fs.Add(Rejection{SourceSliceID: "s1", Query: "q2", Reason: "consensus_failed", RejectedAt: 20}); err != nil {
		t.Fatal(err)
	}

	fs2, err := NewRejectionFileStore(path) // fresh handle = restart
	if err != nil {
		t.Fatal(err)
	}
	n, err := fs2.Count("s1", "q1", 30, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	// Expiry prunes the file.
	if _, err := fs2.Count("s1", "q1", 2000, 100); err != nil {
		t.Fatal(err)
	}
	n, err = fs2.Count("s1", "q1", 2000, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("post-expiry count = %d, want 0", n)
	}
}
