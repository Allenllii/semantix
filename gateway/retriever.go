package gateway

import (
	"sort"
	"strings"
	"sync"

	"semantix/kernel/bm25"
	"semantix/kernel/embed"
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
func newRetriever(kind string, dim int) slice.Index {
	if dim <= 0 {
		dim = defaultVectorDim
	}
	switch kind {
	case "vector":
		return newVectorIndex(dim)
	case "hybrid":
		return &hybridIndex{bm: bm25.New(), vec: newVectorIndex(dim)}
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

// hybridIndex runs BM25 and vector retrieval and fuses the top-k per route.
type hybridIndex struct {
	bm  slice.Index // *bm25.Index
	vec *vectorIndex
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
	return fuseHits(bmHits, vecHits, k), nil
}

// fuseHits normalizes each route's score to [0,1] relative to that route's
// top-1 (a route with no results contributes nothing) and averages them.
// The fused score stays on the bounded scale, so the zone classifier's
// absolute floors behave like the pure-cosine case.
func fuseHits(bm, vec []slice.Hit, k int) []slice.Hit {
	norm := func(hits []slice.Hit) map[string]float64 {
		m := map[string]float64{}
		if len(hits) == 0 {
			return m
		}
		top1 := hits[0].Score
		if top1 <= 0 {
			return m
		}
		for _, h := range hits {
			if h.Score > 0 {
				m[h.Slice.ID] = h.Score / top1
			}
		}
		return m
	}
	bmN := norm(bm)
	vecN := norm(vec)
	if len(bmN) == 0 && len(vecN) == 0 {
		return []slice.Hit{}
	}

	merged := map[string]*slice.Slice{}
	score := map[string]float64{}
	for _, h := range bm {
		if _, ok := merged[h.Slice.ID]; !ok {
			merged[h.Slice.ID] = h.Slice
			score[h.Slice.ID] = 0
		}
	}
	for _, h := range vec {
		if _, ok := merged[h.Slice.ID]; !ok {
			merged[h.Slice.ID] = h.Slice
			score[h.Slice.ID] = 0
		}
	}
	for id := range merged {
		var s float64
		if v, ok := bmN[id]; ok {
			s += v
		}
		if v, ok := vecN[id]; ok {
			s += v
		}
		score[id] = s / 2
	}

	out := make([]slice.Hit, 0, len(merged))
	for id, s := range merged {
		if score[id] > 0 { // drop no-signal candidates, like bm25's score<=0 filter
			// Lexical support = the normalized BM25 route contribution; 0 means
			// the candidate was a pure-vector hit with no term overlap (Issue
			// #260). A fused index always evaluates it, so LexicalValid is set.
			lx := 0.0
			if v, ok := bmN[id]; ok {
				lx = v
			}
			out = append(out, slice.Hit{Slice: s, Score: score[id], Lexical: lx, LexicalValid: true})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Slice.ID < out[j].Slice.ID
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > k {
		out = out[:k]
	}
	return out
}
