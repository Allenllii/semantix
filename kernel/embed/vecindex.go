package embed

import (
	"math"
	"sort"
	"sync"
)

// Hit is one vector-retrieval result.
type Hit struct {
	ID    string
	Score float32 // cosine similarity in [-1, 1]
}

// VectorIndex is an in-memory cosine-similarity index over normalized
// vectors. Zero dependencies, mutex-guarded; vectors must be L2-normalized
// (cosine then equals the dot product).
type VectorIndex struct {
	mu   sync.RWMutex
	vecs map[string][]float32
}

// NewVectorIndex creates an empty index.
func NewVectorIndex() *VectorIndex {
	return &VectorIndex{vecs: map[string][]float32{}}
}

// Insert stores (or replaces) the vector for id. The vector is copied so
// later caller-side mutation cannot race or corrupt the index.
func (vi *VectorIndex) Insert(id string, vec []float32) {
	vi.mu.Lock()
	defer vi.mu.Unlock()
	vi.vecs[id] = append([]float32(nil), vec...)
}

// Remove deletes id (idempotent).
func (vi *VectorIndex) Remove(id string) {
	vi.mu.Lock()
	defer vi.mu.Unlock()
	delete(vi.vecs, id)
}

// Len reports the number of indexed vectors.
func (vi *VectorIndex) Len() int {
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	return len(vi.vecs)
}

// Search returns the top-k most similar ids to query (normalized vector).
func (vi *VectorIndex) Search(query []float32, k int) []Hit {
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	if k <= 0 {
		k = 5
	}
	hits := make([]Hit, 0, len(vi.vecs))
	for id, v := range vi.vecs {
		if len(v) != len(query) {
			continue // dimension mismatch: skip
		}
		var dot float32
		for i := range query {
			dot += query[i] * v[i]
		}
		hits = append(hits, Hit{ID: id, Score: dot})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits
}

// Cosine reports cosine similarity between two vectors (handles unnormalized
// inputs defensively). Used by tests and hybrid scoring.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
