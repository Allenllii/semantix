package gateway

import (
	"log"
	"os"
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

// EmbedSettings selects the vector-route embedding backend (W2 of the
// efficiency research plan). Backend "" / "hash" keeps the deterministic
// offline HashEmbedder; "model" routes through a remote OpenAI-compatible
// embeddings API (kernel/embed.ModelEmbedder, key from
// SEMANTIX_EMBED_API_KEY, fail-open degrading to hash) and implies the
// HNSW ANN index replacing the O(n) brute-force scan — the bottleneck once
// the library reaches tens of thousands of slices.
type EmbedSettings struct {
	Backend string
	BaseURL string
	Model   string
}

// embedAPIKey reads the embeddings API key from the environment only —
// never from config files or code (security constraint: no usable
// credential literals in config).
func (s EmbedSettings) embedAPIKey() string {
	return strings.TrimSpace(os.Getenv("SEMANTIX_EMBED_API_KEY"))
}

// newRetriever builds the retrieval index selected by [retrieval] retriever.
// Unknown kinds fall back to bm25 (config.validate rejects them before New,
// so this default is defensive only). dim seeds the HashEmbedder (<=0 → 256).
// fusion configures the hybrid fusion strategy (Issue #274); it is ignored
// by the single-route indexes. es selects the embedding backend; the zero
// value keeps the deterministic hash route with the brute-force vector
// index (its vectors are recomputable and libraries at ANN scale are rare
// there).
func newRetriever(kind string, dim int, fusion fuse.Config, es EmbedSettings) slice.Index {
	if dim <= 0 {
		dim = defaultVectorDim
	}
	emb, ann := buildEmbedder(es, dim)
	switch kind {
	case "vector":
		return newVectorIndex(dim, emb, ann)
	case "hybrid":
		return &hybridIndex{bm: bm25.New(), vec: newVectorIndex(dim, emb, ann), fusion: fusion}
	default:
		return bm25.New()
	}
}

// NewFusedIndex exposes the retriever construction to external consumers
// (the CLI probe / offline calibration harnesses) so they measure retrieval
// exactly as the gateway builds it — one code path, no drift.
func NewFusedIndex(kind string, dim int, fusion fuse.Config, es EmbedSettings) slice.Index {
	return newRetriever(kind, dim, fusion, es)
}

// buildEmbedder resolves the embedding backend and whether to use the ANN
// index. "model" without a usable config degrades to hash (fail-open).
func buildEmbedder(es EmbedSettings, dim int) (embed.Embedder, bool) {
	if es.Backend != "model" {
		return embed.HashEmbedder{Dim: dim}, false
	}
	key := es.embedAPIKey()
	if key == "" || es.BaseURL == "" || es.Model == "" {
		log.Printf("gateway: [retrieval] embed_backend=model needs embed_base_url, embed_model and SEMANTIX_EMBED_API_KEY; degrading to hash embedder")
		return embed.HashEmbedder{Dim: dim}, false
	}
	me, err := embed.NewModelEmbedder(embed.ModelEmbedderConfig{
		BaseURL: es.BaseURL,
		Model:   es.Model,
		APIKey:  key,
		// When [retrieval] vector_dim is set it doubles as the expected
		// embedding dimension: a mismatch is the mixed-dimension library
		// failure mode (Issue #63), caught here instead of poisoning the
		// index. 0 keeps the check disabled.
		Dim:        dim,
		OnFallback: func(err error) { log.Printf("gateway: embed fallback: %v", err) },
	})
	if err != nil {
		log.Printf("gateway: model embedder unavailable (%v); degrading to hash embedder", err)
		return embed.HashEmbedder{Dim: dim}, false
	}
	return me, true
}

// BatchInserter is implemented by indexes that can amortize embedding cost
// across inserts: the startup index rebuild (loadIndex) embeds thousands of
// slices, and one HTTP round-trip per slice would dominate gateway startup
// under the model backend. Consumers type-assert and fall back to Insert.
type BatchInserter interface {
	InsertBatch(slices []*slice.Slice) error
}

// embedBatchSize bounds one embeddings API request (tokens per request stay
// modest; 64 whole slices keeps payloads well under typical 8k-token limits).
const embedBatchSize = 64

// vecIndex is the contract shared by the brute-force VectorIndex and the
// HNSW ANN index.
type vecIndex interface {
	Insert(id string, vec []float32)
	Remove(id string)
	Len() int
	Search(query []float32, k int) []embed.Hit
}

// vectorIndex adapts an embed vector index to slice.Index: slices are embedded
// on Insert, the query is embedded on Search, and hits are filtered by scope
// then mapped back to slice.Hit. Cosine similarity is the score.
type vectorIndex struct {
	emb  embed.Embedder
	vec  vecIndex
	mu   sync.RWMutex
	byID map[string]*slice.Slice
}

func newVectorIndex(dim int, emb embed.Embedder, ann bool) *vectorIndex {
	if dim <= 0 {
		dim = defaultVectorDim
	}
	if emb == nil {
		emb = embed.HashEmbedder{Dim: dim}
	}
	var vec vecIndex
	if ann {
		vec = embed.NewHNSW(embed.DefaultHNSWConfig())
	} else {
		vec = embed.NewVectorIndex()
	}
	return &vectorIndex{
		emb:  emb,
		vec:  vec,
		byID: map[string]*slice.Slice{},
	}
}

// InsertBatch embeds in chunks — one API call per chunk under the model
// backend, a no-op win under hash.
func (v *vectorIndex) InsertBatch(slices []*slice.Slice) error {
	for start := 0; start < len(slices); start += embedBatchSize {
		end := start + embedBatchSize
		if end > len(slices) {
			end = len(slices)
		}
		chunk := slices[start:end]
		texts := make([]string, len(chunk))
		for i, s := range chunk {
			texts[i] = string(s.Content)
		}
		vecs, err := v.emb.Embed(texts)
		if err != nil {
			return err
		}
		v.mu.Lock()
		for i, s := range chunk {
			if i < len(vecs) && vecs[i] != nil {
				v.vec.Insert(s.ID, vecs[i])
			}
			v.byID[s.ID] = s
		}
		v.mu.Unlock()
	}
	return nil
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

func (h *hybridIndex) InsertBatch(slices []*slice.Slice) error {
	for _, s := range slices {
		if err := h.bm.Insert(s); err != nil {
			return err
		}
	}
	return h.vec.InsertBatch(slices)
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
