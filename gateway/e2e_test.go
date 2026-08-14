package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"semantix/kernel/slice"
	"semantix/kernel/usage"
)

// ---------------------------------------------------------------------------
// helpers

// testUpstream simulates an OpenAI-compatible upstream LLM: it counts calls,
// captures the last forwarded body, and answers with a canned response.
type testUpstream struct {
	mu       sync.Mutex
	calls    int
	lastBody map[string]any
	stream   string // canned SSE body when the request asked for stream
	plain    string // canned JSON body otherwise
}

func (u *testUpstream) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		u.mu.Lock()
		u.calls++
		u.lastBody = parsed
		u.mu.Unlock()
		if stream, _ := parsed["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, u.stream)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, u.plain)
	}
}

func (u *testUpstream) callCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

func (u *testUpstream) forwardedMessages() []map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	m, _ := u.lastBody["messages"].([]any)
	out := make([]map[string]any, 0, len(m))
	for _, item := range m {
		if mm, ok := item.(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out
}

// newTestGateway builds a gateway wired to the fake upstream in a temp dir.
func newTestGateway(t *testing.T, upURL string) *Gateway {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "test-key"
	cfg.Store.DB = filepath.Join(dir, "db.jsonl")
	cfg.Store.DepsRoot = dir
	cfg.Ingest.SessionsDir = filepath.Join(dir, "sessions")
	cfg.Ingest.UsageLog = filepath.Join(dir, "usage.jsonl")
	cfg.Upstreams = []UpstreamConfig{{
		Name: "deepseek", BaseURL: upURL, APIKey: "up-key",
		ModelAlias: []string{"deepseek-chat"}, UpstreamModel: "deepseek-chat",
		Vendor: "deepseek",
	}}
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

// seed puts a slice into the store and index directly.
func seed(t *testing.T, g *Gateway, s *slice.Slice) {
	t.Helper()
	if s.CreatedAt == 0 {
		s.CreatedAt = time.Now().Unix()
	}
	if err := g.store.Put(s); err != nil {
		t.Fatal(err)
	}
	if err := g.index.Insert(s); err != nil {
		t.Fatal(err)
	}
}

func chatBody(model, user string, stream bool) string {
	raw, _ := json.Marshal(map[string]any{
		"model":    model,
		"stream":   stream,
		"messages": []map[string]string{{"role": "user", "content": user}},
	})
	return string(raw)
}

func postChat(t *testing.T, srv *httptest.Server, key, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

func respContent(t *testing.T, body []byte) string {
	t.Helper()
	var r struct {
		Choices []struct {
			Message struct{ Content string `json:"content"` } `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("unmarshal response %s: %v", body, err)
	}
	if len(r.Choices) == 0 {
		t.Fatalf("no choices in %s", body)
	}
	return r.Choices[0].Message.Content
}

// ---------------------------------------------------------------------------
// L3 hit: zero upstream calls (issue acceptance #1)

func TestE2EL3HitZeroUpstreamCalls(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"unused"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	chash, _ := contextHash([]chatMessage{msg("user", "hello world")})
	seed(t, g, &slice.Slice{
		ID: "l3-a", Type: slice.Result, Scope: slice.Project,
		Content: []byte("hello world hello world cached answer"),
		Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat"},
	})

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if resp.Header.Get("x-semantix-cache") != "hit" {
		t.Errorf("x-semantix-cache = %q, want hit", resp.Header.Get("x-semantix-cache"))
	}
	if got := respContent(t, out); got != "hello world hello world cached answer" {
		t.Errorf("content = %q, want cached answer", got)
	}
	if n := up.callCount(); n != 0 {
		t.Errorf("upstream calls = %d, want 0 (L3 must not reach upstream)", n)
	}
}

// ---------------------------------------------------------------------------
// L2 injection: forwarded request carries the reuse block (acceptance #2)

func TestE2EL2InjectionForwarded(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"upstream reply"}}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	seed(t, g, &slice.Slice{
		ID: "l2-a", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte("prior knowledge about widgets"),
	})
	// Keep the target's BM25 score above the default absolute hit threshold.
	// A single-document corpus scores too low and only produces an empty
	// marker block, which is not an L2 slice hit.
	for i, content := range []string{"alpha", "bravo", "charlie", "delta"} {
		seed(t, g, &slice.Slice{
			ID: fmt.Sprintf("distractor-%d", i), Type: slice.Prompt, Scope: slice.Project,
			Content: []byte(content),
		})
	}

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "widgets", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if resp.Header.Get("x-semantix-cache") != "miss" {
		t.Errorf("x-semantix-cache = %q, want miss", resp.Header.Get("x-semantix-cache"))
	}
	if got := respContent(t, out); got != "upstream reply" {
		t.Errorf("content = %q, want upstream reply (passthrough)", got)
	}
	if n := up.callCount(); n != 1 {
		t.Fatalf("upstream calls = %d, want 1", n)
	}
	msgs := up.forwardedMessages()
	if len(msgs) == 0 {
		t.Fatal("no messages forwarded")
	}
	sys, _ := msgs[0]["content"].(string)
	if msgs[0]["role"] != "system" || !strings.Contains(sys, "[semantix-reuse]") {
		t.Errorf("injection block not prepended as system message: %#v", msgs[0])
	}
	summary, err := usage.Summarize(g.cfg.Ingest.UsageLog, usage.DefaultCostMissPerMTok, usage.DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if summary.SliceHits != 1 {
		t.Errorf("usage slice hits = %d, want 1", summary.SliceHits)
	}
}

// ---------------------------------------------------------------------------
// SSE passthrough (design §3.4)

func TestE2EStreamPassthrough(t *testing.T) {
	up := &testUpstream{stream: "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\ndata: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "stream me", true))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if !strings.Contains(string(out), "[DONE]") || !strings.Contains(string(out), `"content":"a"`) {
		t.Errorf("stream not relayed verbatim: %s", out)
	}
}

// ---------------------------------------------------------------------------
// auth + errors

func TestE2EAuthAndErrors(t *testing.T) {
	upSrv := httptest.NewServer((&testUpstream{plain: `{}`}).handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	// no key
	resp, out := postChat(t, srv, "", chatBody("deepseek-chat", "hi", false))
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(out), "authentication_error") {
		t.Errorf("no key: status=%d body=%s", resp.StatusCode, out)
	}
	// wrong key
	resp, out = postChat(t, srv, "wrong", chatBody("deepseek-chat", "hi", false))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong key: status=%d", resp.StatusCode)
	}
	// unknown model
	resp, out = postChat(t, srv, "test-key", chatBody("nope", "hi", false))
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(out), "model_not_found") {
		t.Errorf("unknown model: status=%d body=%s", resp.StatusCode, out)
	}
	// bad request
	resp, out = postChat(t, srv, "test-key", `{"model":"deepseek-chat","messages":[]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty messages: status=%d body=%s", resp.StatusCode, out)
	}
	// healthz is unauthenticated
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(out), `"status":"ok"`) {
		t.Errorf("healthz: status=%d body=%s", resp.StatusCode, out)
	}
	// models endpoint lists the routed model
	resp, err = http.Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	out, _ = io.ReadAll(resp.Body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/v1/models without key: status=%d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// L3 streaming replay (design §3.4)

func TestE2EL3StreamingReplay(t *testing.T) {
	up := &testUpstream{stream: "data: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	chash, _ := contextHash([]chatMessage{msg("user", "widgets cached")})
	seed(t, g, &slice.Slice{
		ID: "l3-s", Type: slice.Result, Scope: slice.Project,
		Content: []byte("widgets cached widgets cached stream answer"),
		Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat"},
	})

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "widgets cached", true))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	body := string(out)
	if resp.Header.Get("x-semantix-cache") != "hit" {
		t.Errorf("x-semantix-cache = %q, want hit", resp.Header.Get("x-semantix-cache"))
	}
	if !strings.Contains(body, "cached stream answer") || !strings.Contains(body, "[DONE]") {
		t.Errorf("replay stream missing content or terminator: %s", body)
	}
	if n := up.callCount(); n != 0 {
		t.Errorf("upstream calls = %d, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// sidecar write memory (design §3.7)

func TestE2ESidecarWrittenAndIngested(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"fresh reply"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	_, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "brand new topic", false))
	if respContent(t, out) != "fresh reply" {
		t.Fatalf("unexpected passthrough: %s", out)
	}

	// sidecar JSONL exists with the request + response turns
	dir := g.cfg.Ingest.SessionsDir
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no sidecar files in %s: %v", dir, err)
	}
	path := filepath.Join(dir, entries[0].Name())
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "brand new topic") || !strings.Contains(string(raw), "fresh reply") {
		t.Errorf("sidecar missing turns: %s", raw)
	}

	// async ingest eventually extracts slices into the store
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		items, err := g.store.ListAll()
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range items {
			if s.ID == "l3-a" || s.ID == "l2-a" { // seeds of other tests
				continue
			}
			if bytes.Contains(s.Content, []byte("brand new topic")) {
				return // ingested
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("async ingest did not extract the session into the store within 3s")
}

// ---------------------------------------------------------------------------
// scope header override

func TestE2EScopeHeader(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions",
		strings.NewReader(chatBody("deepseek-chat", "scope test", false)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("x-semantix-scope", "user")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("scope header: status=%d body=%s", resp.StatusCode, out)
	}
	if sc, err := g.resolveScope(req); err != nil || sc != slice.User {
		t.Errorf("scope not resolved to user: %v (%v)", sc, err)
	}

	// invalid scope header is rejected
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions",
		strings.NewReader(chatBody("deepseek-chat", "scope test", false)))
	req2.Header.Set("Authorization", "Bearer test-key")
	req2.Header.Set("x-semantix-scope", "bogus")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	out2, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("bad scope header: status=%d body=%s", resp2.StatusCode, out2)
	}
}

var _ = fmt.Sprintf

// ---------------------------------------------------------------------------
// model alias mapping (review fix: upstream_model must reach the upstream)

func TestE2EAliasMapping(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"mapped"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	g.cfg.Upstreams[0].ModelAlias = []string{"client-model"}
	g.cfg.Upstreams[0].UpstreamModel = "real-model"
	srv := httptest.NewServer(g)
	defer srv.Close()

	_, out := postChat(t, srv, "test-key", chatBody("client-model", "hi", false))
	if respContent(t, out) != "mapped" {
		t.Fatalf("unexpected: %s", out)
	}
	up.mu.Lock()
	model, _ := up.lastBody["model"].(string)
	up.mu.Unlock()
	if model != "real-model" {
		t.Errorf("forwarded model = %q, want real-model (alias must be mapped)", model)
	}
}

// ---------------------------------------------------------------------------
// L3 context isolation (review fix: different history must not reuse)

func TestE2ECrossContextIsolation(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"fresh"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	messagesA := []chatMessage{msg("system", "project alpha"), msg("user", "hello world cached")}
	chashA, _ := contextHash(messagesA)
	seed(t, g, &slice.Slice{
		ID: "l3-ctx", Type: slice.Result, Scope: slice.Project,
		Content: []byte("hello world cached answer from context A"),
		Meta: slice.SliceMeta{
			L3Safe: true, ContextHash: chashA, Model: "deepseek-chat",
		},
	})

	body := func(messages []chatMessage) string {
		raw, _ := json.Marshal(map[string]any{
			"model": "deepseek-chat", "messages": messages,
		})
		return string(raw)
	}

	// same context → hit, zero upstream calls
	resp, out := postChat(t, srv, "test-key", body(messagesA))
	if resp.Header.Get("x-semantix-cache") != "hit" {
		t.Errorf("same context: cache = %q, want hit (%s)", resp.Header.Get("x-semantix-cache"), out)
	}

	// different system prompt (different context) → miss, upstream called
	other := []chatMessage{msg("system", "project beta"), msg("user", "hello world cached")}
	resp2, out2 := postChat(t, srv, "test-key", body(other))
	if resp2.Header.Get("x-semantix-cache") != "miss" {
		t.Errorf("different context: cache = %q, want miss (%s)", resp2.Header.Get("x-semantix-cache"), out2)
	}
	if n := up.callCount(); n != 1 {
		t.Errorf("upstream calls = %d, want exactly 1 (only the different-context request)", n)
	}
}

// ---------------------------------------------------------------------------
// L3Safe=false rejection (design §3.5: gateway results are not L3 by default)

func TestE2EL3SafeFalseRejected(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"not cached"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	seed(t, g, &slice.Slice{
		ID: "l3-unsafe", Type: slice.Result, Scope: slice.Project,
		Content: []byte("hello world hello world unsafe answer"),
		Meta:    slice.SliceMeta{L3Safe: false}, // no deps + not opted in
	})

	resp, _ := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp.Header.Get("x-semantix-cache") != "miss" {
		t.Errorf("cache = %q, want miss (L3Safe=false must not be served)", resp.Header.Get("x-semantix-cache"))
	}
	if n := up.callCount(); n != 1 {
		t.Errorf("upstream calls = %d, want 1", n)
	}
}
