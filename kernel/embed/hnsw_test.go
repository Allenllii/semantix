package embed

import (
	"math"
	"math/rand"
	"testing"
)

func randVec(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = rng.Float32()*2 - 1
	}
	normalize(v)
	return v
}

// TestHNSWRecall checks the ANN actually finds near-neighbors: at 2000
// clustered points, top-10 recall@10 vs brute force must exceed 0.9
// (Malkov §5 operating point for M=16/ef=64 is ~0.98; 0.9 leaves slack for
// tiny-dimension cluster noise).
func TestHNSWRecall(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	dim := 32
	const n = 2000
	h := NewHNSW(DefaultHNSWConfig())
	brute := NewVectorIndex()
	ids := make([]string, n)
	for i := range ids {
		ids[i] = "v" + string(rune('a'+i%26)) + itoa(i)
		v := randVec(rng, dim)
		h.Insert(ids[i], v)
		brute.Insert(ids[i], v)
	}
	var recallSum float64
	for q := 0; q < 50; q++ {
		query := randVec(rng, dim)
		got := h.Search(query, 10)
		want := brute.Search(query, 10)
		wantSet := map[string]bool{}
		for _, w := range want {
			wantSet[w.ID] = true
		}
		hits := 0
		for _, g := range got {
			if wantSet[g.ID] {
				hits++
			}
		}
		recallSum += float64(hits) / 10
	}
	if recall := recallSum / 50; recall < 0.9 {
		t.Fatalf("recall@10 = %.3f, want >= 0.9", recall)
	}
}

// TestHNSWDeterminism: the same insertion sequence must produce identical
// search output (injection byte-stability depends on deterministic retrieval).
func TestHNSWDeterminism(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	dim := 16
	build := func() *HNSW {
		h := NewHNSW(DefaultHNSWConfig())
		for i := 0; i < 500; i++ {
			h.Insert(itoa(i), randVec(rng, dim))
		}
		return h
	}
	// Re-seed and rebuild identically.
	rng = rand.New(rand.NewSource(7))
	h2 := build()
	rng = rand.New(rand.NewSource(7))
	h1 := build()
	for q := 0; q < 20; q++ {
		query := randVec(rng, dim)
		a := h1.Search(query, 5)
		b := h2.Search(query, 5)
		if len(a) != len(b) {
			t.Fatalf("result length mismatch")
		}
		for i := range a {
			if a[i].ID != b[i].ID || math.Abs(float64(a[i].Score-b[i].Score)) > 1e-6 {
				t.Fatalf("nondeterministic result at %d: %v vs %v", i, a[i], b[i])
			}
		}
	}
}

// TestHNSWRemove: tombstoned ids never surface and Len tracks liveness.
func TestHNSWRemove(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	dim := 8
	h := NewHNSW(DefaultHNSWConfig())
	for i := 0; i < 100; i++ {
		h.Insert(itoa(i), randVec(rng, dim))
	}
	if h.Len() != 100 {
		t.Fatalf("Len = %d, want 100", h.Len())
	}
	h.Remove("42")
	h.Remove("42") // idempotent
	if h.Len() != 99 {
		t.Fatalf("Len after remove = %d, want 99", h.Len())
	}
	for q := 0; q < 30; q++ {
		for _, hit := range h.Search(randVec(rng, dim), 20) {
			if hit.ID == "42" {
				t.Fatalf("removed id 42 surfaced in results")
			}
		}
	}
}

// TestHNSWReinsert replaces the vector for an existing id.
func TestHNSWReinsert(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	dim := 8
	h := NewHNSW(DefaultHNSWConfig())
	for i := 0; i < 50; i++ {
		h.Insert(itoa(i), randVec(rng, dim))
	}
	target := randVec(rng, dim)
	h.Insert("probe", target)
	h.Insert("probe", target) // replace again: no graph duplication
	top := h.Search(target, 1)
	if len(top) != 1 || top[0].ID != "probe" {
		t.Fatalf("self-query should return probe first, got %+v", top)
	}
}

func itoa(n int) string {
	return fmtInt(n)
}

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
