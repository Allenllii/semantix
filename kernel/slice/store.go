package slice

// Store persists slices. MVP implementation: zero-dependency JSONL file store
// (NewFileStore); a bbolt-backed store can replace it later if volume demands.
type Store interface {
	Put(s *Slice) error
	Get(id string) (*Slice, error)
	List(scope Scope) ([]*Slice, error)
	UpdateStats(id string, delta SliceStats) error
	// ListAll returns every slice in the store regardless of scope
	// (maintenance commands: export/gc).
	ListAll() ([]*Slice, error)
	// Delete removes the slice with id. Removing a missing id is a no-op.
	Delete(id string) error
}

// Index provides similarity retrieval over the slice corpus (BM25 lands in U5).
type Index interface {
	Search(query string, k int, scope Scope) ([]Hit, error)
	Insert(s *Slice) error
	Remove(id string) error
}
