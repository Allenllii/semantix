package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"semantix/kernel/bm25"
	"semantix/kernel/fingerprint"
	"semantix/kernel/slice"
)

// --- helpers ---

// testUpstream spins up a fake OpenAI-compatible upstream recording calls.
type testUpstream struct {
	srv      *httptest.Server
	calls    atomic.Int32
	lastBody []byte
	lastPath string
	handler  func(w http.ResponseWriter, r *http.Request, body []byte)
}

func newTestUpstream(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, body []byte)) *testUpstream {
	t.Helper()
	u := &testUpstream{handler: handler}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		u.lastBody = body
		u.lastPath = r.URL.Path
		if u.handler != nil {
			u.handler(w, r, body)
		}
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *testUpstream) url() string { return u.srv.URL }

func jsonCompletion(content string) string {
	return fmt.Sprintf(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"m",
"choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],
"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, mustJSON(content))
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// seedResult seeds a verified reusable Result slice (as if captured by
// extract --l3-safe with deps, or by the gateway write-back).
func seedResult(t *testing.T, dbPath string, content, model, ctxHash string, deps fingerprint.Deps, mtimes map[string]int64) {
	t.Helper()
	seedResultMeta(t, dbPath, content, model, ctxHash, deps, mtimes)
}

// seedResultMeta is seedResult with full meta control.
func seedResultMeta(t *testing.T, dbPath string, content, model, ctxHash string, deps fingerprint.Deps, mtimes map[string]int64) {
	t.Helper()
	st, err := slice.NewFileStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sl := &slice.Slice{
		ID: seedID(content), Type: slice.Result, Scope: slice.Project,
		Content:   []byte(content),
		Weight:    1.0,
		CreatedAt: time.Now().Unix(),
		Meta: slice.SliceMeta{
			Model:       model,
			ContextHash: ctxHash,
			Deps:        deps,
			Mtimes:      mtimes,
		},
	}
	if err := st.Put(sl); err != nil {
		t.Fatal(err)
	}
}

func seedID(content string) string {
	sum := sha256.Sum256([]byte{byte(slice.Result), byte(slice.Project)})
	sum2 := sha256.Sum256([]byte(content))
	return hex.EncodeToString(append(sum[:], sum2[:]...))[:16]
}

// seedPrompt seeds a reusable Prompt slice for L2 injection.
func seedPrompt(t *testing.T, dbPath, content string) {
	t.Helper()
	st, err := slice.NewFileStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sl := &slice.Slice{
		ID: seedID(content), Type: slice.Prompt, Scope: slice.Project,
		Content:   []byte(content),
		Weight:    1.0,
		CreatedAt: time.Now().Unix(),
	}
	if err := st.Put(sl); err != nil {
		t.Fatal(err)
	}
}

func captureDeps(t *testing.T, root string, paths ...string) (fingerprint.Deps, map[string]int64) {
	t.Helper()
	deps, err := fingerprint.Capture(root, paths)
	if err != nil {
		t.Fatal(err)
	}
	mtimes := map[string]int64{}
	for _, p := range paths {
		fi, err := os.Stat(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		mtimes[p] = fi.ModTime().Unix()
	}
	return deps, mtimes
}

func requestBody(model string, stream bool, messages ...string) []byte {
	var msgs []map[string]string
	for _, m := range messages {
		parts := strings.SplitN(m, ":", 2)
		if len(parts) != 2 {
			parts = []string{"user", m}
		}
		msgs = append(msgs, map[string]string{"role": parts[0], "content": parts[1]})
	}
	b, _ := json.Marshal(map[string]any{"model": model, "messages": msgs, "stream": stream})
	return b
}

func ctxHashOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func gatewayConfig(t *testing.T, alias string) Config {
	t.Helper()
	cfg := Config{
		StoreDB: filepath.Join(t.TempDir(), "gw.jsonl"),
		Scope:   slice.Project,
		Upstreams: []Upstream{{
			Name:          "test-up",
			BaseURL:       "http://127.0.0.1:1", // replaced by tests with a live upstream
			APIKey:        "up-key",
			ModelAlias:    []string{alias},
			UpstreamModel: "real-model",
			Vendor:        "openai",
		}},
		Retrieval: struct {
			TopK   int
			Budget int
		}{TopK: 5, Budget: 4096},
		Cache: struct {
			TTLSeconds int64
			L3Safe     bool
			DepsRoot   string
		}{TTLSeconds: 86400},
	}
	return cfg
}

func (s *Server) rebuild(t *testing.T) {
	t.Helper()
	idx := bm25.New()
	if err := indexFromStore(s.store, idx); err != nil {
		t.Fatal(err)
	}
	s.index = idx
}

// --- tests ---

// TestL3HitZeroUpstream is the issue acceptance core: an L3 hit serves the
// cached response with zero upstream calls.
func TestL3HitZeroUpstream(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("data v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, mtimes := captureDeps(t, root, "a.txt")

	body := requestBody("deepseek-chat", false, "user:how do I run go tests with race detection")
	cfg := gatewayConfig(t, "deepseek-chat")
	cfg.Cache.DepsRoot = root
	seedResult(t, cfg.StoreDB, "cached answer: run `go test -race ./...` and read the failure trace",
		"deepseek-chat", ctxHashOf(body), deps, mtimes)

	up := newTestUpstream(t, nil)
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	rec, m := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("x-semantix-cache") != "hit" {
		t.Fatalf("x-semantix-cache = %q, want hit", rec.Header().Get("x-semantix-cache"))
	}
	choices, _ := m["choices"].([]any)
	msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
	if !strings.Contains(msg["content"].(string), "go test -race") {
		t.Fatalf("content = %v", msg["content"])
	}
	usage, _ := m["usage"].(map[string]any)
	if usage["completion_tokens"] != float64(0) {
		t.Fatalf("completion_tokens must be 0 on L3 hit, got %v", usage)
	}
	details, _ := usage["prompt_tokens_details"].(map[string]any)
	if details["cached_tokens"] != usage["prompt_tokens"] {
		t.Fatalf("cached_tokens must cover prompt_tokens on L3 hit: %v", usage)
	}
	if got := up.calls.Load(); got != 0 {
		t.Fatalf("L3 hit must not call upstream, got %d calls", got)
	}
}

// TestL3ModelMismatchNoReuse: a cached entry for another model never serves
// this request (design §3.5 cache key includes the model).
func TestL3ModelMismatchNoReuse(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("data v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, mtimes := captureDeps(t, root, "a.txt")

	body := requestBody("deepseek-chat", false, "user:how do I run go tests with race detection")
	cfg := gatewayConfig(t, "deepseek-chat")
	cfg.Cache.DepsRoot = root
	seedResult(t, cfg.StoreDB, "claude-style answer about go test", "claude-sonnet", ctxHashOf(body), deps, mtimes)

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("fresh upstream answer"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	rec, m := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("x-semantix-cache") == "hit" {
		t.Fatal("cross-model reuse must be rejected")
	}
	choices, _ := m["choices"].([]any)
	msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
	if !strings.Contains(msg["content"].(string), "fresh upstream answer") {
		t.Fatalf("expected upstream answer, got %v", msg["content"])
	}
	if up.calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1", up.calls.Load())
	}
}

// TestL3ContextMismatchNoReuse: the same query under a different message
// context never reuses (design §3.5 messages-context fingerprint).
func TestL3ContextMismatchNoReuse(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("data v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, mtimes := captureDeps(t, root, "a.txt")

	bodyA := requestBody("deepseek-chat", false, "system:project alpha", "user:how do I run go tests with race detection")
	bodyB := requestBody("deepseek-chat", false, "system:project beta", "user:how do I run go tests with race detection")
	cfg := gatewayConfig(t, "deepseek-chat")
	cfg.Cache.DepsRoot = root
	seedResult(t, cfg.StoreDB, "answer valid only under project alpha", "deepseek-chat", ctxHashOf(bodyA), deps, mtimes)

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("fresh answer"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	rec, _ := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(bodyB), "")
	if rec.Header().Get("x-semantix-cache") == "hit" {
		t.Fatal("cross-context reuse must be rejected")
	}
	if !strings.Contains(rec.Body.String(), "fresh answer") {
		t.Fatalf("expected upstream answer: %s", rec.Body.String())
	}
}

// TestL3TTLExpired: an expired entry is never served.
func TestL3TTLExpired(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("data v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, mtimes := captureDeps(t, root, "a.txt")

	body := requestBody("deepseek-chat", false, "user:how do I run go tests with race detection")
	cfg := gatewayConfig(t, "deepseek-chat")
	cfg.Cache.DepsRoot = root
	cfg.Cache.TTLSeconds = 60
	seedResult(t, cfg.StoreDB, "expired cached answer", "deepseek-chat", ctxHashOf(body), deps, mtimes)
	// age the entry beyond the TTL
	st, _ := slice.NewFileStore(cfg.StoreDB)
	sl, _ := st.Get(seedID("expired cached answer"))
	sl.CreatedAt = time.Now().Unix() - 120
	_ = st.Put(sl)

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("fresh answer"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	rec, _ := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if rec.Header().Get("x-semantix-cache") == "hit" {
		t.Fatal("expired L3 entry must not be served")
	}
	if !strings.Contains(rec.Body.String(), "fresh answer") {
		t.Fatalf("expected upstream answer: %s", rec.Body.String())
	}
}

// TestL2InjectionForwarded is the issue acceptance core: on a miss, the
// forwarded request carries the [semantix-reuse] block (the model skips
// repeated exploration), the model is mapped, and the upstream body is
// passed through untouched.
func TestL2InjectionForwarded(t *testing.T) {
	cfg := gatewayConfig(t, "ds-chat")
	seedPrompt(t, cfg.StoreDB, "how to fix a failing go test: run go test -race and read the failure trace")

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("fixed the test failure"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	body := requestBody("ds-chat", false, "user:how do I fix this failing go test")
	rec, _ := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("x-semantix-cache") != "miss" {
		t.Fatalf("x-semantix-cache = %q, want miss", rec.Header().Get("x-semantix-cache"))
	}
	if up.calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1", up.calls.Load())
	}
	fwd := string(up.lastBody)
	if !strings.Contains(fwd, "[semantix-reuse]") {
		t.Fatalf("forwarded request missing injection block: %s", fwd)
	}
	if !strings.Contains(fwd, "how to fix a failing go test") {
		t.Fatalf("forwarded request missing slice content: %s", fwd)
	}
	// Model mapping: alias ds-chat → upstream real-model.
	var sent struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(up.lastBody, &sent); err != nil || sent.Model != "real-model" {
		t.Fatalf("forwarded model = %q (err %v), want real-model", sent.Model, err)
	}
	// Byte-exact passthrough of the upstream body.
	if rec.Body.String() != jsonCompletion("fixed the test failure") {
		t.Fatalf("body passthrough mismatch:\n got %s", rec.Body.String())
	}
	// Deterministic rebuild: the same request forwards byte-identically
	// (L1 prefix-cache requirement).
	rec2, _ := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if up.calls.Load() != 2 {
		t.Fatalf("second request must also hit upstream (no L3 entry), calls = %d", up.calls.Load())
	}
	if string(up.lastBody) != fwd {
		t.Fatal("forwarded body must be byte-identical for identical requests (L1)")
	}
	_ = rec2
}

// TestForwardStreamPassthrough: SSE events flow through verbatim and the
// stream terminates with [DONE].
func TestForwardStreamPassthrough(t *testing.T) {
	cfg := gatewayConfig(t, "ds-chat")
	seedPrompt(t, cfg.StoreDB, "the answer to streamed questions is often in the first chunk")

	chunks := []string{
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hello "},"finish_reason":null}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"world"},"finish_reason":null}]}`,
	}
	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	body := requestBody("ds-chat", true, "user:stream me an answer")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	for _, c := range chunks {
		if !strings.Contains(out, c) {
			t.Fatalf("stream chunk not passed through: %s", c)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "data: [DONE]") {
		t.Fatalf("stream must end with [DONE]: %q", out)
	}
}

// TestStreamL3HitReplay: an L3 hit under stream=true replays the cached
// content as SSE without calling upstream.
func TestStreamL3HitReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("data v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, mtimes := captureDeps(t, root, "a.txt")

	body := requestBody("deepseek-chat", true, "user:how do I run go tests with race detection")
	cfg := gatewayConfig(t, "deepseek-chat")
	cfg.Cache.DepsRoot = root
	seedResult(t, cfg.StoreDB, "cached answer: run `go test -race ./...`",
		"deepseek-chat", ctxHashOf(body), deps, mtimes)

	up := newTestUpstream(t, nil)
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("x-semantix-cache") != "hit" {
		t.Fatal("expected x-semantix-cache: hit")
	}
	out := rec.Body.String()
	if !strings.Contains(out, "cached answer: run") || !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("replay stream missing content or [DONE]: %q", out)
	}
	if !strings.Contains(out, `"finish_reason":"stop"`) {
		t.Fatalf("replay stream missing finish_reason: %q", out)
	}
	if got := up.calls.Load(); got != 0 {
		t.Fatalf("L3 stream hit must not call upstream, got %d", got)
	}
}

// TestUnknownModel: unconfigured models are rejected with an OpenAI error.
func TestUnknownModel(t *testing.T) {
	cfg := gatewayConfig(t, "ds-chat")
	s := testServer(t, &cfg)
	body := requestBody("nope-model", false, "user:hi")
	rec, m := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if errMsg, _ := m["error"].(map[string]any); errMsg["code"] != "model_not_found" {
		t.Fatalf("error = %v", m)
	}
}

// TestUpstreamFailureSurfacesOpenAIError: upstream network failure yields a
// 502 in OpenAI error format (the client / New API can retry).
func TestUpstreamFailureSurfacesOpenAIError(t *testing.T) {
	cfg := gatewayConfig(t, "ds-chat")
	cfg.Upstreams[0].BaseURL = "http://127.0.0.1:1" // nothing listens here
	s := testServer(t, &cfg)
	body := requestBody("ds-chat", false, "user:hi")
	rec, m := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body %s", rec.Code, rec.Body.String())
	}
	if errMsg, _ := m["error"].(map[string]any); errMsg["code"] != "upstream_error" {
		t.Fatalf("error = %v", m)
	}
}

// TestL3EmptyMetaNoReuse: an entry without the gateway metadata (e.g. a
// CLI-extracted Result slice) must never serve a gateway request
// (review fix: fail-closed on missing Model/ContextHash). The seed content
// deliberately overlaps the query so the retrieval/zone gates pass and the
// meta gate is actually exercised.
func TestL3EmptyMetaNoReuse(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("data v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, mtimes := captureDeps(t, root, "a.txt")

	body := requestBody("deepseek-chat", false, "user:how do I run go tests with race detection")
	cfg := gatewayConfig(t, "deepseek-chat")
	cfg.Cache.DepsRoot = root
	// No Model / no ContextHash in meta (legacy/CLI entry) — content
	// overlaps the query so it would be retrieved if it were eligible.
	seedResultMeta(t, cfg.StoreDB, "go tests with race detection: answer without gateway meta",
		"", "", deps, mtimes)

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("fresh answer"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	rec, _ := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if rec.Header().Get("x-semantix-cache") == "hit" {
		t.Fatal("entry without gateway meta must not be reused")
	}
	if !strings.Contains(rec.Body.String(), "fresh answer") {
		t.Fatalf("expected upstream answer: %s", rec.Body.String())
	}
}

// TestL3UnknownAgeNoReuse: an entry with unknown creation time (CreatedAt
// == 0) is never served (review fix: TTL gate treats unknown age as
// expired).
func TestL3UnknownAgeNoReuse(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("data v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, mtimes := captureDeps(t, root, "a.txt")

	body := requestBody("deepseek-chat", false, "user:how do I run go tests with race detection")
	cfg := gatewayConfig(t, "deepseek-chat")
	cfg.Cache.DepsRoot = root
	seedResult(t, cfg.StoreDB, "go tests with race detection: cached answer of unknown age",
		"deepseek-chat", ctxHashOf(body), deps, mtimes)
	st, _ := slice.NewFileStore(cfg.StoreDB)
	sl, _ := st.Get(seedID("go tests with race detection: cached answer of unknown age"))
	sl.CreatedAt = 0
	if err := st.Put(sl); err != nil {
		t.Fatal(err)
	}

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("fresh answer"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	rec, _ := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if rec.Header().Get("x-semantix-cache") == "hit" {
		t.Fatal("unknown-age entry must not be reused")
	}
	if !strings.Contains(rec.Body.String(), "fresh answer") {
		t.Fatalf("expected upstream answer: %s", rec.Body.String())
	}
}

// TestL3SkippedWithoutDepsRoot: with no deps_root configured the L3 lookup
// is skipped entirely (fail-closed — verification against the process CWD
// must never happen), even for a fully qualified, retrievable entry.
func TestL3SkippedWithoutDepsRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("data v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, mtimes := captureDeps(t, root, "a.txt")

	body := requestBody("deepseek-chat", false, "user:how do I run go tests with race detection")
	cfg := gatewayConfig(t, "deepseek-chat")
	// cfg.Cache.DepsRoot intentionally left empty.
	seedResult(t, cfg.StoreDB, "go tests with race detection: fully qualified cached answer",
		"deepseek-chat", ctxHashOf(body), deps, mtimes)

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("fresh answer"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	rec, _ := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if rec.Header().Get("x-semantix-cache") == "hit" {
		t.Fatal("L3 must be skipped without deps_root")
	}
	if !strings.Contains(rec.Body.String(), "fresh answer") {
		t.Fatalf("expected upstream answer: %s", rec.Body.String())
	}
}

// TestRebuildBodyDeterministic: identical inputs produce byte-identical
// output (L1 requirement).
func TestRebuildBodyDeterministic(t *testing.T) {
	raw := []byte(`{"model":"a","stream":true,"messages":[{"role":"user","content":"x"}]}`)
	var msgs []json.RawMessage
	if err := json.Unmarshal([]byte(`[{"role":"user","content":"x"}]`), &msgs); err != nil {
		t.Fatal(err)
	}
	a, err := rebuildBody(raw, msgs, "b")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := rebuildBody(raw, msgs, "b")
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b2) {
		t.Fatalf("rebuild not deterministic:\n%s\n%s", a, b2)
	}
	var m struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(a, &m); err != nil || m.Model != "b" {
		t.Fatalf("model not mapped: %s", a)
	}
}

func TestNormalizeQuery(t *testing.T) {
	cases := map[string]string{
		"  fix\n my\t tests  ": "fix my tests",
		"already clean":        "already clean",
		"   ":                  "",
		"a\u0000b\u001fc":      "a b c",
	}
	for in, want := range cases {
		if got := normalizeQuery(in); got != want {
			t.Errorf("normalizeQuery(%q) = %q, want %q", in, got, want)
		}
	}
}
