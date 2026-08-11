package promote

import (
	"strings"
	"testing"
)

func TestContentVersionDeterministic(t *testing.T) {
	a := ContentVersion([]byte("answer"))
	b := ContentVersion([]byte("answer"))
	if a != b {
		t.Fatal("same content must produce the same version")
	}
	c := ContentVersion([]byte("answer2"))
	if a == c {
		t.Fatal("different content must produce a different version")
	}
	if len(a) != 64 {
		t.Fatalf("version length = %d, want 64 (sha256 hex)", len(a))
	}
}

func TestMemStorePutListDelete(t *testing.T) {
	st := NewMemStore()
	e := Entry{SourceSliceID: "s1", ContentVersion: "v1", Query: "q", PromotedAt: 1}
	if err := st.Put(e); err != nil {
		t.Fatal(err)
	}
	// Duplicate put is a no-op.
	if err := st.Put(e); err != nil {
		t.Fatal(err)
	}
	list, err := st.List("s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1 (dedup)", len(list))
	}
	if err := st.Delete("s1", "v1", "q"); err != nil {
		t.Fatal(err)
	}
	list, _ = st.List("s1")
	if len(list) != 0 {
		t.Fatal("delete must remove the entry")
	}
}

func TestCascadeInvalidate(t *testing.T) {
	st := NewMemStore()
	old := []byte("old answer")
	new := []byte("new answer")
	for _, e := range []Entry{
		{SourceSliceID: "s1", ContentVersion: ContentVersion(old), Query: "q1", PromotedAt: 1},
		{SourceSliceID: "s1", ContentVersion: ContentVersion(old), Query: "q2", PromotedAt: 2},
		{SourceSliceID: "s2", ContentVersion: ContentVersion(new), Query: "q3", PromotedAt: 3},
	} {
		if err := st.Put(e); err != nil {
			t.Fatal(err)
		}
	}
	// Source s1 changed old → new: both s1 entries (old version) invalidate.
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
	// s2 entry (already current version) survives.
	if list, _ := st.List("s2"); len(list) != 1 {
		t.Fatal("s2 entry must survive (version matches)")
	}
	// Idempotent: second cascade invalidates nothing.
	if n, _ := CascadeInvalidate(st, "s1", new); n != 0 {
		t.Fatalf("second cascade invalidated %d, want 0 (idempotent)", n)
	}
}

func TestCascadeInvalidateUnknownSource(t *testing.T) {
	st := NewMemStore()
	n, err := CascadeInvalidate(st, "missing", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("unknown source invalidated %d, want 0", n)
	}
}

func TestMemStoreConcurrent(t *testing.T) {
	st := NewMemStore()
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func(g int) {
			defer func() { done <- struct{}{} }()
			for n := 0; n < 100; n++ {
				q := strings.Repeat("q", g) + string(rune('0'+n%10))
				_ = st.Put(Entry{SourceSliceID: "s", ContentVersion: "v", Query: q, PromotedAt: int64(n)})
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	if list, _ := st.List("s"); len(list) == 0 {
		t.Fatal("concurrent puts must be recorded")
	}
}
