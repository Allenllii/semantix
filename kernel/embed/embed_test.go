package embed

import (
	"math"
	"testing"
)

func TestHashEmbedderDeterministicAndNormalized(t *testing.T) {
	h := HashEmbedder{Dim: 256}
	v1, err := h.Embed([]string{"修复 go 测试失败"})
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := h.Embed([]string{"修复 go 测试失败"})
	if len(v1) != 1 || len(v1[0]) != 256 {
		t.Fatalf("dim = %d, want 256", len(v1[0]))
	}
	for i := range v1[0] {
		if v1[0][i] != v2[0][i] {
			t.Fatal("embedding not deterministic")
		}
	}
	var norm float64
	for _, x := range v1[0] {
		norm += float64(x) * float64(x)
	}
	if math.Abs(norm-1) > 1e-5 {
		t.Fatalf("vector not L2-normalized: norm^2 = %f", norm)
	}
}

func TestHashEmbedderSimilarityOrdering(t *testing.T) {
	h := HashEmbedder{Dim: 256}
	base := "修复 go 测试失败"
	similar := "修复 go 测试失败 的问题"
	unrelated := "部署 kubernetes 集群"
	vecs, err := h.Embed([]string{base, similar, unrelated})
	if err != nil {
		t.Fatal(err)
	}
	sim := Cosine(vecs[0], vecs[1])
	unrel := Cosine(vecs[0], vecs[2])
	if sim <= unrel {
		t.Fatalf("similar pair (%f) not above unrelated pair (%f)", sim, unrel)
	}
	if sim < 0.5 {
		t.Fatalf("similar pair cosine = %f, want ≥ 0.5 (shared tokens)", sim)
	}
}

func TestVectorIndexSearch(t *testing.T) {
	vi := NewVectorIndex()
	h := HashEmbedder{Dim: 64}
	texts := []string{"修复 go 测试失败", "配置 CI 流水线", "部署到服务器"}
	vecs, _ := h.Embed(texts)
	vi.Insert("a", vecs[0])
	vi.Insert("b", vecs[1])
	vi.Insert("c", vecs[2])

	q, _ := h.Embed([]string{"修复测试失败"})
	hits := vi.Search(q[0], 2)
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].ID != "a" {
		t.Fatalf("top hit = %q, want a (repair-testing slice)", hits[0].ID)
	}
	if hits[0].Score <= hits[1].Score {
		t.Fatalf("not sorted desc: %v", hits)
	}

	vi.Remove("a")
	if vi.Len() != 2 {
		t.Fatalf("Len after remove = %d, want 2", vi.Len())
	}
	hits = vi.Search(q[0], 5)
	if hits[0].ID == "a" {
		t.Fatal("removed id still returned")
	}
}

func TestVectorIndexDimensionMismatch(t *testing.T) {
	vi := NewVectorIndex()
	vi.Insert("a", make([]float32, 64))
	vi.Insert("b", make([]float32, 128))
	hits := vi.Search(make([]float32, 64), 5)
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1 (mismatched dims skipped)", len(hits))
	}
}
