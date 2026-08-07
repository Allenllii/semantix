package slice

// Store persists slices (bbolt-backed; MVP implementation lands in U4).
type Store interface {
	Put(s *Slice) error
	Get(id string) (*Slice, error)
	List(scope Scope) ([]*Slice, error)
	UpdateStats(id string, delta SliceStats) error
}

// Index provides similarity retrieval over the slice corpus (BM25 lands in U5).
type Index interface {
	Search(query string, k int, scope Scope) ([]Hit, error)
	Insert(s *Slice) error
	Remove(id string) error
}
