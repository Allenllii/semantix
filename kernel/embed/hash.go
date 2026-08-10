package embed

import (
	"math"
	"unicode"
)

// HashEmbedder is a zero-dependency, deterministic embedder based on feature
// hashing of character n-grams (CJK-aware). It provides lexical-semantic
// similarity without any external model — a stand-in until a real embedding
// model (local bge-m3 / remote API) is wired into the same Embedder
// interface. Vectors are L2-normalized; the same text always yields the same
// vector.
//
// Limits (honest): no true semantics beyond token overlap; dimension and
// hash collisions bound discrimination. Use it to validate the vector
// retrieval path end-to-end; swap the implementation for a model-backed
// embedder later without touching callers.
type HashEmbedder struct {
	// Dim is the vector dimension (default 256).
	Dim int
}

// Embed implements Embedder.
func (h HashEmbedder) Embed(texts []string) ([][]float32, error) {
	dim := h.Dim
	if dim <= 0 {
		dim = 256
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		vec := make([]float32, dim)
		for _, tok := range tokenize(t) {
			// FNV-1a 32-bit hash → bucket and sign (unbiased hashing)
			hash := fnv1a(tok)
			idx := int(hash % uint32(dim))
			if hash&1 == 0 {
				vec[idx] += 1
			} else {
				vec[idx] -= 1
			}
		}
		normalize(vec)
		out[i] = vec
	}
	return out, nil
}

// tokenize splits into CJK chars (one token each) and ASCII alnum runs.
func tokenize(s string) []string {
	var toks []string
	var run []rune
	flush := func() {
		if len(run) > 0 {
			toks = append(toks, string(run))
			run = nil
		}
	}
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			flush()
			toks = append(toks, string(r)) // single CJK char token
			continue
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			run = append(run, r)
			continue
		}
		flush()
	}
	flush()
	return toks
}

func fnv1a(s string) uint32 {
	const offset = 2166136261
	const prime = 16777619
	h := uint32(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}

func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}
