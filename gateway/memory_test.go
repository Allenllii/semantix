package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// TestSessionBypassAndExtract: request/response pairs land in per-session
// JSONL files and the async worker extracts them into the slice store
// (design §3.7) — the acceptance that a gateway conversation becomes
// searchable, i.e. L2 material for future turns.
func TestSessionBypassAndExtract(t *testing.T) {
	cfg := gatewayConfig(t, "ds-chat")
	sessions := filepath.Join(t.TempDir(), "sessions")
	cfg.Ingest.SessionsDir = sessions
	cfg.Ingest.Extract = true

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("fixed the go test failure by running -race"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	body := requestBody("ds-chat", false, "user:fix the failing go test in this repo")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-semantix-session", "test-session-1")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	// Session file written with user + assistant lines.
	path := filepath.Join(sessions, "test-session-1.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("session file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("session lines = %d, want 2:\n%s", len(lines), raw)
	}
	var userLine, asstLine map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &userLine); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &asstLine); err != nil {
		t.Fatal(err)
	}
	if userLine["role"] != "user" || !strings.Contains(userLine["content"].(string), "fix the failing go test") {
		t.Fatalf("user line = %v", userLine)
	}
	if asstLine["role"] != "assistant" || !strings.Contains(asstLine["content"].(string), "-race") {
		t.Fatalf("assistant line = %v", asstLine)
	}

	// Async extraction lands slices in the store.
	s.mem.flush()
	st, err := slice.NewFileStore(cfg.StoreDB)
	if err != nil {
		t.Fatal(err)
	}
	items, err := st.List(slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	for _, it := range items {
		types[it.Type.String()] = true
	}
	if !types["prompt"] || !types["result"] {
		t.Fatalf("extracted types = %v, want prompt+result", types)
	}

	// The extracted prompt is retrievable for a future turn (L2 material).
	s.rebuild(t)
	hits, err := s.index.Search("fix the failing go test", 5, slice.Project)
	if err != nil || len(hits) == 0 {
		t.Fatalf("future-turn lookup missed: %v %v", hits, err)
	}
}

// TestSessionIDSanitized: hostile session ids cannot escape the sessions
// directory (design §3.10).
func TestSessionIDSanitized(t *testing.T) {
	cfg := gatewayConfig(t, "ds-chat")
	cfg.Ingest.SessionsDir = filepath.Join(t.TempDir(), "sessions")
	cfg.Ingest.Extract = false
	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("answer"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	body := requestBody("ds-chat", false, "user:hi")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	req.Header.Set("x-semantix-session", "../evil")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(cfg.Ingest.SessionsDir, "..", "evil.jsonl")); err == nil {
		t.Fatal("session id escaped the sessions dir")
	}
	// ../evil sanitizes to .._evil (separators replaced, no traversal).
	if _, err := os.Stat(filepath.Join(cfg.Ingest.SessionsDir, ".._evil.jsonl")); err != nil {
		t.Fatalf("sanitized session file missing: %v", err)
	}
}

// TestL3WriteBackThenHit is the acceptance loop: a first miss caches the
// response (opt-in l3_safe + deps_root), the identical second request is
// served from L3 with zero upstream calls.
func TestL3WriteBackThenHit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("version 1"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := gatewayConfig(t, "deepseek-chat")
	cfg.Cache.L3Safe = true
	cfg.Cache.DepsRoot = root

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("stable answer for the go test question"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	body := requestBody("deepseek-chat", false, "user:what does the go test question ask")
	rec, _ := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if rec.Header().Get("x-semantix-cache") != "miss" {
		t.Fatalf("first request must miss, got %s", rec.Header().Get("x-semantix-cache"))
	}

	// The write-back lands in the store + index.
	st, err := slice.NewFileStore(cfg.StoreDB)
	if err != nil {
		t.Fatal(err)
	}
	items, err := st.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("write-back entries = %d, want 1", len(items))
	}
	if items[0].Meta.Model != "deepseek-chat" || items[0].Meta.ContextHash == "" {
		t.Fatalf("write-back meta = %+v", items[0].Meta)
	}
	if len(items[0].Meta.Deps) == 0 {
		t.Fatal("write-back must capture deps")
	}

	// Second identical request: L3 hit, zero upstream calls.
	before := up.calls.Load()
	rec2, m := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if rec2.Header().Get("x-semantix-cache") != "hit" {
		t.Fatalf("second request must hit, got %s (%s)", rec2.Header().Get("x-semantix-cache"), rec2.Body.String())
	}
	if got := up.calls.Load(); got != before {
		t.Fatalf("L3 hit called upstream: %d → %d", before, got)
	}
	choices, _ := m["choices"].([]any)
	msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
	if !strings.Contains(msg["content"].(string), "stable answer") {
		t.Fatalf("cached content = %v", msg["content"])
	}
}

// TestWriteBackInvalidatedOnDepsChange: modifying a dependency file kills
// the cached entry (design §3.5: 文件一变缓存即失效).
func TestWriteBackInvalidatedOnDepsChange(t *testing.T) {
	root := t.TempDir()
	dep := filepath.Join(root, "a.txt")
	if err := os.WriteFile(dep, []byte("version 1"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := gatewayConfig(t, "deepseek-chat")
	cfg.Cache.L3Safe = true
	cfg.Cache.DepsRoot = root

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("answer one"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	body := requestBody("deepseek-chat", false, "user:question about the go module")
	doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if up.calls.Load() != 1 {
		t.Fatalf("calls after first = %d", up.calls.Load())
	}

	// Change a dependency: the entry must fail verification.
	if err := os.WriteFile(dep, []byte("version 2"), 0o600); err != nil {
		t.Fatal(err)
	}
	doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if got := up.calls.Load(); got != 2 {
		t.Fatalf("stale entry must not be reused after deps change: calls = %d, want 2", got)
	}
}

// TestToolCallsNeverCached: responses with tool calls never enter L3
// (design §3.5: side effects).
func TestToolCallsNeverCached(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := gatewayConfig(t, "deepseek-chat")
	cfg.Cache.L3Safe = true
	cfg.Cache.DepsRoot = root

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, `{"id":"c","object":"chat.completion","created":1,"model":"m",
"choices":[{"index":0,"message":{"role":"assistant","content":"checking...",
"tool_calls":[{"id":"t1","type":"function","function":{"name":"run","arguments":"{}"}}]},"finish_reason":"tool_calls"}],
"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	body := requestBody("deepseek-chat", false, "user:run the checks please")
	doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if got := up.calls.Load(); got != 2 {
		t.Fatalf("tool-call responses must never be cached: calls = %d, want 2", got)
	}
}

// TestWriteBackDisabledByDefault: without explicit opt-in nothing is cached
// (fail-closed default).
func TestWriteBackDisabledByDefault(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := gatewayConfig(t, "deepseek-chat")
	cfg.Cache.L3Safe = false // default
	cfg.Cache.DepsRoot = root

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("plain answer"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	body := requestBody("deepseek-chat", false, "user:some question here")
	doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if got := up.calls.Load(); got != 2 {
		t.Fatalf("l3_safe=false must not cache: calls = %d, want 2", got)
	}
}

func TestCaptureTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("aaa"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.go"), []byte("bbb"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, mtimes, err := captureTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 || deps["a.txt"] == "" || deps[filepath.Join("sub", "b.go")] == "" {
		t.Fatalf("deps = %v", deps)
	}
	if mtimes["a.txt"] == 0 || mtimes[filepath.Join("sub", "b.go")] == 0 {
		t.Fatalf("mtimes = %v", mtimes)
	}
	// Changed content → new digest (invalidation basis).
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps2, _, err := captureTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if deps2["a.txt"] == deps["a.txt"] {
		t.Fatal("digest must change after content change")
	}
}

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		"plain-session_1": "plain-session_1",
		"../evil":         ".._evil",
		"a/b\\c":          "a_b_c",
		"中文会话":            "____",
	}
	for in, want := range cases {
		if got := safeFilename(in); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
