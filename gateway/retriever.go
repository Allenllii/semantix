package gateway

import (
	"log"
	"os"
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

// RetrieverSettings parameterizes newRetriever.
type RetrieverSettings struct {
	// Kind is the retriever kind: bm25 | vector | hybrid.
	Kind string
	// Dim is the HashEmbedder dimension (<=0 -> 256).
	Dim int
	// EmbedBackend selects the embedding backend for vector/hybrid routes:
	// "" or "hash" keeps the deterministic HashEmbedder (no network);
	// "model" uses a remote OpenAI-compatible embeddings API via
	// kernel/embed.ModelEmbedder (fail-soft to hash). The API key is read
	// from SEMANTIX_EMBED_API_KEY; "model" without a key degrades to hash
	// with a startup log line, never an error (fail-open, README design
	// principle: optimization failure must not become agent failure).
	EmbedBackend string
	// EmbedBaseURL / EmbedModel configure the remote embeddings endpoint
	// (e.g. https://api.openai.com/v1 + text-embedding-3-small, or a local
	// OpenAI-compatible server).
	EmbedBaseURL string
	EmbedModel   string
}

// NewFusedIndex exposes the retriever construction to external consumers
// (the CLI probe / offline calibration harnesses) so they measure retrieval
// exactly as the gateway builds it — one code path, no drift.
func NewFusedIndex(s RetrieverSettings) slice.Index { return newRetriever(s) }

// newRetriever builds the retrieval index selected by [retrieval] retriever.
// Unknown kinds fall back to bm25 (config.validate rejects them before New,
// so this default is defensive only).
//
// Backend selection: the model backend implies two upgrades over the hash
// route — real semantic vectors (ModelEmbedder) and the HNSW ANN index
// (kernel/embed.HNSW) replacing the O(n) brute-force scan, which is the
// bottleneck once the library reaches tens of thousands of slices. The hash
// backend keeps the original brute-force VectorIndex: its vectors are
// recomputable and libraries at that scale are still rare there.
func newRetriever(s RetrieverSettings) slice.Index {
	dim := s.Dim
	if dim <= 0 {
		dim = defaultVectorDim
	}
	emb, ann := buildEmbedder(s, dim)
	switch s.Kind {
	case "vector":
		return newVectorIndex(dim, emb, ann)
	case "hybrid":
		return &hybridIndex{bm: bm25.New(), vec: newVectorIndex(dim, emb, ann)}
	default:
		return bm25.New()
	}
}

// buildEmbedder resolves the embedding backend and whether to use the ANN
// index. "model" without a usable config degrades to hash (fail-open).
func buildEmbedder(s RetrieverSettings, dim int) (embed.Embedder, bool) {
	if s.EmbedBackend != "model" {
		return embed.HashEmbedder{Dim: dim}, false
	}
	key := s.embedAPIKey()
	if key == "" || s.EmbedBaseURL == "" || s.EmbedModel == "" {
		log.Printf("gateway: [retrieval] embed_backend=model needs embed_base_url, embed_model and SEMANTIX_EMBED_API_KEY; degrading to hash embedder")
		return embed.HashEmbedder{Dim: dim}, false
	}
	me, err := embed.NewModelEmbedder(embed.ModelEmbedderConfig{
		BaseURL: s.EmbedBaseURL,
		Model:   s.EmbedModel,
		APIKey:  key,
		// When [retrieval] vector_dim is set it doubles as the expected
		// embedding dimension: a mismatch is the mixed-dimension library
		// failure mode (Issue #63), caught here instead of poisoning the
		// index. 0 keeps the check disabled (accept whatever the endpoint
		// returns).
		Dim:        s.Dim,
		OnFallback: func(err error) { log.Printf("gateway: embed fallback: %v", err) },
	})
	if err != nil {
		log.Printf("gateway: model embedder unavailable (%v); degrading to hash embedder", err)
		return embed.HashEmbedder{Dim: dim}, false
	}
	return me, true
}

// embedAPIKey reads the embeddings API key from the environment only —
// never from config files or code (security constraint: no usable
// credential literals in config).
func (s RetrieverSettings) embedAPIKey() string {
	return strings.TrimSpace(os.Getenv("SEMANTIX_EMBED_API_KEY"))
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

// vecIndex is the contract shared by the brute-force VectorIndex and the
// HNSW ANN index.
type vecIndex interface {
	Insert(id string, vec []float32)
	Remove(id string)
	Len() int
	Search(query []float32, k int) []embed.Hit
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

// InsertBatch embeds in chunks — one API call per chunk under the model
// backend, a no-op win under hash.
func (v *vectorIndex) InsertBatch(slices []*slice.Slice) error {
	for start := 0; start < len(slices); start += embedBatchSize {
		end := min(start+embedBatchSize, len(slices))
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

func (h *hybridIndex) InsertBatch(slices []*slice.Slice) error {
	for _, s := range slices {
		if err := h.bm.Insert(s); err != nil {
			return err
		}
	}
	return h.vec.InsertBatch(slices)
}

// Insert stores one slice (incremental path; embedding cost is one call).
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
