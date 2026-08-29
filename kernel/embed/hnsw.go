package embed

import (
	"math"
	"math/rand"
	"sort"
	"sync"
)

// HNSW is a dependency-free approximate nearest-neighbor index over
// L2-normalized vectors (cosine == dot product), implementing the same
// contract as VectorIndex. It exists because the brute-force scan in
// VectorIndex is O(n) per query — fine at hundreds of slices, the roof at
// tens of thousands — while the slice library is expected to grow past that
// once real harness traffic accumulates (Issue #263 load projection).
//
// Determinism contract: retrieval must be deterministic for a fixed
// insertion sequence, because L2 injection freezes whole slices and any
// retrieval nondeterminism would destabilize the prompt bytes the provider
// prefix cache keys on. Three rules enforce it:
//   - the level RNG is a seeded PRPG driven by an insert counter (never time);
//   - neighbor pruning and beam ties break on (score desc, id asc);
//   - Search output is always re-sorted by (score desc, id asc) before return.
//
// Removals are tombstones (the entry stays in the graph but is skipped by
// traversal and Len/Search), matching VectorIndex.Remove semantics without
// graph surgery on the hot path.

// HNSWConfig tunes the graph. Zero fields fall back to the Defaults below
// (Malkov & Yashunin §4: M=16, efConstruction=200, efSearch>=k is the
// common operating point for recall >= 0.95 at 10^4–10^5 scale).
type HNSWConfig struct {
	M              int // max out-links per node on upper layers
	M0             int // max out-links on layer 0 (defaults to 2*M)
	EfConstruction int // beam width during insert
	EfSearch       int // beam width during search (raised to >= k)
	Seed           int64
}

// DefaultHNSWConfig is the zero-thought-good operating point.
func DefaultHNSWConfig() HNSWConfig {
	return HNSWConfig{M: 16, EfConstruction: 200, EfSearch: 64, Seed: 1}
}

type hnswNode struct {
	vec  []float32
	links [][]string // links[level] = neighbor ids, ascending by (score desc, id asc) invariant
}

// HNSW implements the vector side of slice retrieval.
type HNSW struct {
	mu   sync.RWMutex
	cfg  HNSWConfig
	m0   int
	nodes map[string]*hnswNode
	dead  map[string]bool
	entry string
	top   int // top level of the entry point
	seq   uint64
}

// NewHNSW builds an empty index.
func NewHNSW(cfg HNSWConfig) *HNSW {
	if cfg.M <= 0 {
		cfg.M = 16
	}
	if cfg.EfConstruction <= 0 {
		cfg.EfConstruction = 200
	}
	if cfg.EfSearch <= 0 {
		cfg.EfSearch = 64
	}
	if cfg.M0 <= 0 {
		cfg.M0 = 2 * cfg.M
	}
	return &HNSW{
		cfg:   cfg,
		m0:    cfg.M0,
		nodes: map[string]*hnswNode{},
		dead:  map[string]bool{},
		top:   -1,
	}
}

// Insert stores (or replaces) the vector for id. Replacement keeps the
// original graph position and only swaps the vector — the graph topology
// stays deterministic w.r.t. first-insertion order.
func (h *HNSW) Insert(id string, vec []float32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n, ok := h.nodes[id]; ok {
		n.vec = append([]float32(nil), vec...)
		delete(h.dead, id)
		return
	}
	level := h.randomLevel()
	h.nodes[id] = &hnswNode{vec: append([]float32(nil), vec...), links: make([][]string, level+1)}
	delete(h.dead, id)
	if h.entry == "" {
		h.entry = id
		h.top = level
		return
	}
	// Descend greedily above the new node's level.
	cur := h.entry
	for l := h.top; l > level; l-- {
		cur = h.greedyStep(vec, cur, l)
	}
	// Beam insert on each layer from min(top, level) down to 0.
	visited := map[string]bool{}
	cands := []candidate{{id: cur, score: h.dot(vec, cur)}}
	for l := min(level, h.top); l >= 0; l-- {
		cands = h.beam(vec, cands, h.cfg.EfConstruction, l, visited)
		selected := h.selectNeighbors(cands, h.layerCap(l))
		for _, c := range selected {
			h.link(id, c.id, l)
			h.link(c.id, id, l)
		}
	}
	if level > h.top {
		h.top = level
		h.entry = id
	}
}

// Remove tombstones id (idempotent). Entries stay in the graph; traversal
// and Len/Search skip them.
func (h *HNSW) Remove(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dead[id] = true
}

// Len reports the number of live indexed vectors.
func (h *HNSW) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.nodes) - len(h.dead)
}

// Search returns the top-k most similar live ids to query (normalized).
func (h *HNSW) Search(query []float32, k int) []Hit {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if k <= 0 {
		k = 5
	}
	if h.entry == "" || len(h.nodes)-len(h.dead) == 0 {
		return []Hit{}
	}
	cur := h.entry
	for l := h.top; l > 0; l-- {
		cur = h.greedyStep(query, cur, l)
	}
	ef := max(k, h.cfg.EfSearch)
	cands := h.beam(query, []candidate{{id: cur, score: h.dot(query, cur)}}, ef, 0, map[string]bool{})
	out := make([]Hit, 0, len(cands))
	for _, c := range cands {
		if h.dead[c.id] {
			continue
		}
		if v := h.nodes[c.id]; v != nil && len(v.vec) == len(query) {
			out = append(out, Hit{ID: c.id, Score: c.score})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > k {
		out = out[:k]
	}
	return out
}

type candidate struct {
	id    string
	score float32
}

func (h *HNSW) dot(query []float32, id string) float32 {
	n := h.nodes[id]
	if n == nil || len(n.vec) != len(query) {
		return -2 // dimension mismatch sorts last
	}
	var dot float32
	for i := range query {
		dot += query[i] * n.vec[i]
	}
	return dot
}

func (h *HNSW) layerCap(l int) int {
	if l == 0 {
		return h.m0
	}
	return h.cfg.M
}

// randomLevel draws the node level from the exponential distribution with
// the classic 1/ln(M) factor, from the counter-seeded PRPG (deterministic).
func (h *HNSW) randomLevel() int {
	h.seq++
	r := rand.New(rand.NewSource(h.cfg.Seed ^ int64(h.seq)*0x9E3779B1)).Float64()
	if r <= 0 {
		r = 1e-12
	}
	return int(-math.Log(r) / math.Log(float64(h.cfg.M)))
}

// greedyStep moves to the best live neighbor at layer l (one hop evaluation,
// repeated until no improvement).
func (h *HNSW) greedyStep(query []float32, cur string, l int) string {
	for {
		best, bestScore := cur, h.dot(query, cur)
		node := h.nodes[cur]
		if node == nil || l >= len(node.links) {
			return best
		}
		for _, nb := range node.links[l] {
			if h.dead[nb] {
				continue
			}
			if s := h.dot(query, nb); s > bestScore {
				best, bestScore = nb, s
			}
		}
		if best == cur {
			return best
		}
		cur = best
	}
}

// beam expands from cands at layer l and returns the top-ef of ALL evaluated
// candidates (the standard ef-search result set W — returning the unexpanded
// frontier would starve the graph: the entry point's first expansion empties
// it). visited is shared across layers of one operation so each node is
// evaluated once.
func (h *HNSW) beam(query []float32, cands []candidate, ef int, l int, visited map[string]bool) []candidate {
	frontier := append([]candidate(nil), cands...)
	sortCands(frontier)
	results := make([]candidate, 0, ef)
	seen := map[string]bool{}
	for _, c := range frontier {
		seen[c.id] = true
		visited[c.id] = true
	}
	addResult := func(c candidate) {
		results = insertCand(results, c, ef)
	}
	for _, c := range frontier {
		addResult(c)
	}
	for len(frontier) > 0 {
		cur := frontier[0]
		frontier = frontier[1:]
		node := h.nodes[cur.id]
		if node == nil || l >= len(node.links) {
			continue
		}
		for _, nb := range node.links[l] {
			if h.dead[nb] || seen[nb] {
				continue
			}
			seen[nb] = true
			visited[nb] = true
			c := candidate{id: nb, score: h.dot(query, nb)}
			frontier = insertCand(frontier, c, ef)
			addResult(c)
		}
	}
	sortCands(results)
	return results
}

// selectNeighbors picks the ef closest candidates (simple selection — the
// heuristic prune variant trades recall determinism for graph sparsity and
// the simple one is the standard choice at these scales).
func (h *HNSW) selectNeighbors(cands []candidate, m int) []candidate {
	sortCands(cands)
	if len(cands) > m {
		cands = cands[:m]
	}
	return cands
}

func (h *HNSW) link(from, to string, l int) {
	node := h.nodes[from]
	if node == nil {
		return
	}
	for l >= len(node.links) {
		node.links = append(node.links, nil)
	}
	for _, nb := range node.links[l] {
		if nb == to {
			return
		}
	}
	node.links[l] = append(node.links[l], to)
	// Cap out-degree, evicting the worst (lowest score relative to from).
	if len(node.links[l]) > h.layerCap(l) {
		fc := h.nodes[from].vec
		worst := 0
		worstScore := h.dot(fc, node.links[l][0])
		for i, nb := range node.links[l] {
			if s := h.dot(fc, nb); s < worstScore {
				worst, worstScore = i, s
			}
		}
		node.links[l] = append(node.links[l][:worst], node.links[l][worst+1:]...)
	}
}

func sortCands(c []candidate) {
	sort.Slice(c, func(i, j int) bool {
		if c[i].score == c[j].score {
			return c[i].id < c[j].id
		}
		return c[i].score > c[j].score
	})
}

func insertCand(sorted []candidate, c candidate, cap int) []candidate {
	i := sort.Search(len(sorted), func(i int) bool {
		return sorted[i].score < c.score || (sorted[i].score == c.score && sorted[i].id > c.id)
	})
	sorted = append(sorted, candidate{})
	copy(sorted[i+1:], sorted[i:])
	sorted[i] = c
	if len(sorted) > cap {
		sorted = sorted[:cap]
	}
	return sorted
}
