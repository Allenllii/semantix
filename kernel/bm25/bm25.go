package bm25

import "semantix/kernel/slice"

// Index is a BM25 retrieval index over slices (implementation lands in U5).
// Parameters follow the reference: k1=1.2, b=0.75, CJK split per rune.
type Index struct {
	k1 float64
	b  float64
}

// New returns an Index with the reference parameters.
func New() *Index { return &Index{k1: 1.2, b: 0.75} } // U5 wires the implementation

// Search returns the top-k matches for query within scope.
func (ix *Index) Search(query string, k int, scope slice.Scope) ([]slice.Hit, error) {
	return nil, nil // placeholder until U5
}

// Insert adds s to the index.
func (ix *Index) Insert(s *slice.Slice) error { return nil } // placeholder until U5

// Remove deletes s from the index.
func (ix *Index) Remove(id string) error { return nil } // placeholder until U5
