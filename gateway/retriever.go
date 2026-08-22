package gateway

import (
	"strings"
	"sync"

	"semantix/kernel/bm25"
	"semantix/kernel/embed"
	"semantix/kernel/fuse"
	"semantix/kernel/slice"
)

// This file wires the `[retrieval] retriever` config key (Issue #186 / GW6).
// `bm25` keeps the historical BM25 index; `vector` retrieves over
// HashEmbedder cosine vectors (kernel/embed, no external embedding API);
// `hybrid` runs both and fuses the scores. All three implement slice.Index,
// so the L2 injector and L3 decider are untouched — only the construction in
// New() changes.
//
// Score scale contract: the zone classifier (kernel/zone) was designed for
// two coexisting scales — BM25 scores (typically >> 1, only the relative
// confidence binds) and the bounded cosine scale (absolute floors also
// bind). vector keeps the raw cosine in [-1,1]; hybrid normalizes both
// routes to [0,1] relative to each route's top-1 and averages them, so the
// fused score stays on the bounded scale the zone floors were tuned for.

const defaultVectorDim = 256

// newRetriever builds the retrieval index selected by [retrieval] retriever.
// Unknown kinds fall back to bm25 (config.validate rejects them before New,
// so this default is defensive only). dim seeds the HashEmbedder (<=0 → 256).
// fusion configures the hybrid fusion strategy (Issue #274); it is ignored
// by the single-route indexes.
func newRetriever(kind string, dim int, fusion fuse.Config) slice.Index {
	if dim <= 0 {
		dim = defaultVectorDim
	}
	switch kind {
	case "vector":
		return newVectorIndex(dim)
	case "hybrid":
		return &hybridIndex{bm: bm25.New(), vec: newVectorIndex(dim), fusion: fusion}
	default:
		return bm25.New()
	}
}

// vectorIndex adapts embed.VectorIndex to slice.Index: slices are embedded
// on Insert, the query is embedded on Search, and hits are filtered by scope
// then mapped back to slice.Hit. Cosine similarity is the score.
type vectorIndex struct {
	emb  embed.HashEmbedder
	vec  *embed.VectorIndex
	mu   sync.RWMutex
	byID map[string]*slice.Slice
}

func newVectorIndex(dim int) *vectorIndex {
	if dim <= 0 {
		dim = defaultVectorDim
	}
	return &vectorIndex{
		emb:  embed.HashEmbedder{Dim: dim},
		vec:  embed.NewVectorIndex(),
		byID: map[string]*slice.Slice{},
	}
}

func (v *vectorIndex) Insert(s *slice.Slice) error {
	vecs, err := v.emb.Embed([]string{string(s.Content)})
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.vec.Insert(s.ID, vecs[0])
	v.byID[s.ID] = s
	return nil
}

func (v *vectorIndex) Remove(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.vec.Remove(id)
	delete(v.byID, id)
	return nil
}

func (v *vectorIndex) Search(query string, k int, scope slice.Scope) ([]slice.Hit, error) {
	if k <= 0 {
		return []slice.Hit{}, nil
	}
	vecs, err := v.emb.Embed([]string{query})
	if err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	// Over-fetch then filter by scope, mirroring bm25's per-scope doc set.
	hits := v.vec.Search(vecs[0], k*4)
	out := make([]slice.Hit, 0, k)
	for _, h := range hits {
		s := v.byID[h.ID]
		if s == nil || s.Scope != scope {
			continue
		}
		// Pure-vector route: no fused BM25 contribution exists, so lexical
	// support degrades to query-token coverage (Issue #260). Zero overlap
	// is the "pure-vector hit with no term support" signal for the L3 gate.
	out = append(out, slice.Hit{Slice: s, Score: float64(h.Score), Lexical: bm25.QueryCoverage(query, string(s.Content)), LexicalValid: true})
		if len(out) >= k {
			break
		}
	}
	return out, nil
}

// hybridIndex runs BM25 and vector retrieval and fuses the top-k per route
// through kernel/fuse (Issue #274 single source of truth).
type hybridIndex struct {
	bm      slice.Index // *bm25.Index
	vec     *vectorIndex
	fusion  fuse.Config
}

func (h *hybridIndex) Insert(s *slice.Slice) error {
	if err := h.bm.Insert(s); err != nil {
		return err
	}
	return h.vec.Insert(s)
}

func (h *hybridIndex) Remove(id string) error {
	_ = h.bm.Remove(id)
	return h.vec.Remove(id)
}

func (h *hybridIndex) Search(query string, k int, scope slice.Scope) ([]slice.Hit, error) {
	if k <= 0 {
		return []slice.Hit{}, nil
	}
	bmHits, err := h.bm.Search(query, k, scope)
	if err != nil && strings.TrimSpace(query) != "" {
		// bm25 rejects empty queries; a hybrid of nothing is an empty result,
		// not an error (mirrors the vector route).
		return nil, err
	}
	vecHits, err := h.vec.Search(query, k, scope)
	if err != nil {
		return nil, err
	}
	return fuse.Fuse(bmHits, vecHits, query, k, h.fusion), nil
}
