package embed

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestModelEmbedderRequestFormat verifies the OpenAI-embeddings request
// shape (path, auth, model, input) against a mock server.
func TestModelEmbedderRequestFormat(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/embeddings" {
			t.Errorf("path = %q, want /embeddings", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"embedding":[0.5,0.5,0.0]}]}`))
	}))
	defer srv.Close()

	m, err := NewModelEmbedder(ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "text-embedding-3-small", APIKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	vecs, err := m.Embed([]string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 3 {
		t.Fatalf("vecs = %v, want 1 vector of dim 3", vecs)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Errorf("auth header = %q, want Bearer", gotAuth)
	}
	if gotBody["model"] != "text-embedding-3-small" {
		t.Errorf("model = %v", gotBody["model"])
	}
	inputs, _ := gotBody["input"].([]any)
	if len(inputs) != 1 || inputs[0] != "hello" {
		t.Errorf("input = %v, want [hello]", gotBody["input"])
	}
}

// TestModelEmbedderOrderingAndNormalization verifies vectors come back in
// input order and are L2-normalized (the VectorIndex contract).
func TestModelEmbedderOrderingAndNormalization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Deliberately unnormalized and reversed magnitude to catch order bugs.
		w.Write([]byte(`{"data":[{"embedding":[0.0,3.0,0.0,0.0]},{"embedding":[2.0,0.0,0.0,0.0]}]}`))
	}))
	defer srv.Close()

	m, err := NewModelEmbedder(ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	vecs, err := m.Embed([]string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if vecs[0][1] != 1.0 || vecs[0][0] != 0.0 {
		t.Errorf("first vector = %v, want [0 1 0 0]", vecs[0])
	}
	if vecs[1][0] != 1.0 {
		t.Errorf("second vector = %v, want [1 0 0 0]", vecs[1])
	}
	for i, v := range vecs {
		if norm(v) < 1-1e-6 || norm(v) > 1+1e-6 {
			t.Errorf("vector %d not normalized: norm=%f", i, norm(v))
		}
	}
}

func norm(v []float32) float64 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	return math.Sqrt(s)
}

// TestModelEmbedderFallbackOnHTTPError verifies a 500 response degrades to
// the fallback (fail-soft) without an error.
func TestModelEmbedderFallbackOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	var gotFallbackErr error
	m, err := NewModelEmbedder(ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
		OnFallback: func(err error) { gotFallbackErr = err },
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := HashEmbedder{}.Embed([]string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Embed([]string{"hello"})
	if err != nil {
		t.Fatalf("fallback must be fail-soft: %v", err)
	}
	if len(got) != len(want) || len(got[0]) != len(want[0]) {
		t.Fatalf("got %d vectors of dim %d, want %d of dim %d", len(got), len(got[0]), len(want), len(want[0]))
	}
	for i := range got[0] {
		if got[0][i] != want[0][i] {
			t.Fatalf("fallback vector differs from HashEmbedder output at %d", i)
		}
	}
	if gotFallbackErr == nil {
		t.Error("OnFallback not called")
	}
}

// TestModelEmbedderFallbackOnTimeout verifies a hanging server plus a short
// timeout degrades to the fallback instead of hanging or crashing.
func TestModelEmbedderFallbackOnTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	m, err := NewModelEmbedder(ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k", Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	got, err := m.Embed([]string{"hello"})
	if err != nil {
		t.Fatalf("fallback must be fail-soft: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("fallback took %v, timeout not enforced", elapsed)
	}
	if len(got) != 1 || len(got[0]) == 0 {
		t.Fatalf("fallback output malformed: %v", got)
	}
}

// TestModelEmbedderFallbackOnBadJSON verifies a malformed body degrades.
func TestModelEmbedderFallbackOnBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	m, err := NewModelEmbedder(ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Embed([]string{"hello"})
	if err != nil {
		t.Fatalf("fallback must be fail-soft: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d vectors, want 1", len(got))
	}
}

// TestModelEmbedderFallbackOnCountMismatch verifies a data length that does
// not match the input count degrades (no silent misalignment).
func TestModelEmbedderFallbackOnCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"embedding":[1.0,0.0]}]}`))
	}))
	defer srv.Close()

	m, err := NewModelEmbedder(ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Embed([]string{"a", "b"})
	if err != nil {
		t.Fatalf("fallback must be fail-soft: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d vectors, want 2", len(got))
	}
}

// TestModelEmbedderDimCheck verifies Dim>0 rejects mismatched dimensions.
func TestModelEmbedderDimCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"embedding":[1.0,0.0,0.0]}]}`))
	}))
	defer srv.Close()

	m, err := NewModelEmbedder(ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k", Dim: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Embed([]string{"hello"})
	if err != nil {
		t.Fatalf("fallback must be fail-soft: %v", err)
	}
	if len(got) != 1 || len(got[0]) != 256 {
		t.Fatalf("want hash fallback of dim 256, got %d vectors (dim %d)", len(got), len(got[0]))
	}
}

// TestModelEmbedderFallbackErrorPropagation verifies an error surfaces only
// when the fallback itself fails.
func TestModelEmbedderFallbackErrorPropagation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	boom := errors.New("fallback broken")
	m, err := NewModelEmbedder(ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
		Fallback: failingEmbedder{err: boom},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Embed([]string{"hello"}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want fallback error %v", err, boom)
	}
}

type failingEmbedder struct{ err error }

func (f failingEmbedder) Embed([]string) ([][]float32, error) { return nil, f.err }

// TestModelEmbedderEmptyInput verifies empty input performs no request and
// returns empty vectors.
func TestModelEmbedderEmptyInput(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer srv.Close()

	m, err := NewModelEmbedder(ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	vecs, err := m.Embed(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 0 {
		t.Fatalf("got %d vectors, want 0", len(vecs))
	}
	if calls != 0 {
		t.Fatalf("server called %d times, want 0", calls)
	}
}

// TestNewModelEmbedderValidation verifies constructor checks.
func TestNewModelEmbedderValidation(t *testing.T) {
	cases := []struct {
		name string
		cfg  ModelEmbedderConfig
	}{
		{"missing base-url", ModelEmbedderConfig{Model: "m", APIKey: "k"}},
		{"missing model", ModelEmbedderConfig{BaseURL: "http://x", APIKey: "k"}},
		{"missing key", ModelEmbedderConfig{BaseURL: "http://x", Model: "m"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewModelEmbedder(tc.cfg); err == nil {
				t.Fatal("want validation error")
			}
		})
	}
}

// TestModelEmbedderConcurrentEmbed verifies concurrent embedding batches
// stay independent (the client and config are shared read-only state).
func TestModelEmbedderConcurrentEmbed(t *testing.T) {
	var mu sync.Mutex
	var inputs [][]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		inputs = append(inputs, body["input"].([]any))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"embedding":[1.0,0.0]}]}`))
	}))
	defer srv.Close()

	m, err := NewModelEmbedder(ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text := string(rune('a' + i))
			vecs, err := m.Embed([]string{text})
			if err != nil {
				t.Errorf("embed %q: %v", text, err)
				return
			}
			if len(vecs) != 1 || len(vecs[0]) != 2 {
				t.Errorf("embed %q: got %v", text, vecs)
			}
		}(i)
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(inputs) != 8 {
		t.Fatalf("server saw %d requests, want 8", len(inputs))
	}
	for i, in := range inputs {
		if len(in) != 1 {
			t.Fatalf("request %d input = %v, want 1 element", i, in)
		}
	}
}
