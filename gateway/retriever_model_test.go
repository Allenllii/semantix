package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"semantix/kernel/slice"
)

// embeddingsStub serves an OpenAI-compatible /embeddings endpoint that
// returns a deterministic unit vector per input text: the first character's
// code mapped onto a dim-8 vector. Tests use it as a stand-in for a real
// model backend.
func embeddingsStub(t *testing.T, calls *atomic.Int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/embeddings") {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		calls.Add(1)
		type emb struct {
			Embedding []float32 `json:"embedding"`
		}
		out := struct {
			Data []emb `json:"data"`
		}{}
		for _, text := range req.Input {
			v := make([]float32, 8)
			if len(text) > 0 {
				v[int(text[0])%8] = 1
			}
			out.Data = append(out.Data, emb{Embedding: v})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
}

// TestModelBackendBatchesStartupEmbeds: under embed_backend=model the
// startup rebuild must use the batch path — one embeddings call per chunk,
// not one per slice (the whole point of BatchInserter).
func TestModelBackendBatchesStartupEmbeds(t *testing.T) {
	t.Setenv("SEMANTIX_EMBED_API_KEY", "test-key")
	var calls atomic.Int64
	srv := embeddingsStub(t, &calls)
	defer srv.Close()

	var slices []*slice.Slice
	for i := 0; i < 10; i++ {
		slices = append(slices, projectSlice("s"+string(rune('a'+i)), "deploy golang service number "+string(rune('a'+i))))
	}
	idx := newRetriever(RetrieverSettings{
		Kind:         "hybrid",
		Dim:          8,
		EmbedBackend: "model",
		EmbedBaseURL: srv.URL,
		EmbedModel:   "test-embed",
	})
	bi, ok := idx.(BatchInserter)
	if !ok {
		t.Fatal("model-backed index must implement BatchInserter")
	}
	if err := bi.InsertBatch(slices); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("embeddings calls = %d, want 1 (single batch of 10)", got)
	}
	got, err := idx.Search("deploy golang service number a", 3, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Slice.ID != "sa" {
		t.Fatalf("batched search top hit = %+v, want sa", got)
	}
}

// TestModelBackendWithoutKeyDegradesToHash: "model" without an API key must
// degrade to the hash embedder (fail-open) — never error, never leave the
// gateway unusable.
func TestModelBackendWithoutKeyDegradesToHash(t *testing.T) {
	t.Setenv("SEMANTIX_EMBED_API_KEY", "")
	idx := newRetriever(RetrieverSettings{
		Kind:         "vector",
		Dim:          8,
		EmbedBackend: "model",
		EmbedBaseURL: "http://127.0.0.1:1",
		EmbedModel:   "test-embed",
	})
	seedRetriever(t, idx, projectSlice("a", "deploy golang binary"))
	got, err := idx.Search("deploy golang", 3, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Slice.ID != "a" {
		t.Fatalf("degraded hash search = %+v, want hit a", got)
	}
}

// TestModelBackendFailSoftMidFlight: an embeddings endpoint that dies after
// startup must degrade to hash vectors per call (fail-soft), not error the
// search.
func TestModelBackendFailSoftMidFlight(t *testing.T) {
	t.Setenv("SEMANTIX_EMBED_API_KEY", "test-key")
	var up atomic.Bool
	up.Store(true)
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !up.Load() {
			http.Error(w, "gone", 500)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1,0,0,0,0,0,0,0]}]}`))
	}))
	defer srv.Close()

	idx := newRetriever(RetrieverSettings{
		Kind:         "vector",
		Dim:          8,
		EmbedBackend: "model",
		EmbedBaseURL: srv.URL,
		EmbedModel:   "test-embed",
	})
	seedRetriever(t, idx, projectSlice("a", "deploy golang binary"))
	up.Store(false)
	// Search embeds the query; the dead endpoint forces hash fallback.
	if _, err := idx.Search("deploy golang", 3, slice.Project); err != nil {
		t.Fatalf("search after endpoint death: %v (want fail-soft)", err)
	}
}
