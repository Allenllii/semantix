package gateway

import (
	"testing"

	"semantix/kernel/embed"
)

// TestNewWiresGreyAuditMode: grey_mode=audit must set injector.AllowGrey;
// the default (empty/"drop") must leave it off.
func TestNewWiresGreyAuditMode(t *testing.T) {
	g := newTestGatewayCfg(t, "http://127.0.0.1:1", func(c *Config) {
		c.Retrieval.GreyMode = "audit"
	})
	if !g.injector.AllowGrey {
		t.Fatal("grey_mode=audit must enable AllowGrey on the injector")
	}

	g2 := newTestGatewayCfg(t, "http://127.0.0.1:1", func(*Config) {})
	if g2.injector.AllowGrey {
		t.Fatal("default grey policy must keep AllowGrey off")
	}
}

// TestIndexKindPerBackend: the hash backend keeps the brute-force vector
// index; the model backend implies the HNSW ANN index (buildEmbedder's
// contract: real vectors at 10^4+ scale are where the O(n) scan breaks).
func TestIndexKindPerBackend(t *testing.T) {
	hashIdx := newRetriever(RetrieverSettings{Kind: "hybrid", Dim: 8})
	hh := hashIdx.(*hybridIndex)
	if _, ok := hh.vec.vec.(*embed.VectorIndex); !ok {
		t.Fatalf("hash backend vector index = %T, want *embed.VectorIndex", hh.vec.vec)
	}

	modelIdx := newRetriever(RetrieverSettings{
		Kind:         "hybrid",
		Dim:          8,
		EmbedBackend: "model",
		EmbedBaseURL: "http://127.0.0.1:1",
		EmbedModel:   "m",
	})
	mh := modelIdx.(*hybridIndex)
	// No API key in this test env: buildEmbedder degrades to hash+brute
	// force (fail-open). Assert the degraded shape, then the ANN shape via
	// a directly-provided embedder is covered by retriever_model_test.go.
	if _, ok := mh.vec.vec.(*embed.VectorIndex); !ok {
		t.Fatalf("degraded model backend vector index = %T, want *embed.VectorIndex", mh.vec.vec)
	}
}
