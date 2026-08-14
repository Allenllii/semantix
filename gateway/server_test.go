package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"semantix/kernel/slice"
)

// testServer builds a Server with an in-memory-backed config for endpoint
// tests. The slice store lives in a temp dir.
func testServer(t *testing.T, cfg *Config) *Server {
	t.Helper()
	if cfg == nil {
		cfg = &Config{}
	}
	base := *cfg
	if base.StoreDB == "" {
		base.StoreDB = filepath.Join(t.TempDir(), "gw.jsonl")
	}
	base.Scope = slice.Project
	if base.Retrieval.TopK == 0 {
		base.Retrieval.TopK = 5
	}
	if base.Retrieval.Budget == 0 {
		base.Retrieval.Budget = 4096
	}
	s, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func doJSON(t *testing.T, h http.Handler, method, path, body, key string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var m map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
	}
	return rec, m
}

func TestHealthz(t *testing.T) {
	s := testServer(t, &Config{Upstreams: []Upstream{
		{Name: "ds", BaseURL: "http://x", APIKey: "k", ModelAlias: []string{"m"}},
	}})
	h := s.Handler()
	rec, _ := doJSON(t, h, http.MethodGet, "/healthz", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestHealthzNoUpstreams(t *testing.T) {
	s := testServer(t, nil)
	rec, m := doJSON(t, s.Handler(), http.MethodGet, "/healthz", "", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("healthz with no upstreams = %d, want 503; %v", rec.Code, m)
	}
	if errMsg, _ := m["error"].(map[string]any); errMsg["code"] != "no_upstream" {
		t.Fatalf("error body = %v", m)
	}
}

func TestModelsListsAliases(t *testing.T) {
	s := testServer(t, &Config{Upstreams: []Upstream{
		{Name: "ds", BaseURL: "http://x", APIKey: "k", ModelAlias: []string{"ds-chat"}},
		{Name: "claude", BaseURL: "http://x", APIKey: "k", ModelAlias: []string{"claude-sonnet", "claude-opus"}},
	}})
	rec, m := doJSON(t, s.Handler(), http.MethodGet, "/v1/models", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("models = %d", rec.Code)
	}
	data, ok := m["data"].([]any)
	if !ok || len(data) != 3 {
		t.Fatalf("models data = %v", m)
	}
	first := data[0].(map[string]any)
	if first["id"] != "claude-opus" || first["object"] != "model" {
		t.Fatalf("first model = %v", first)
	}
}

func TestAuthRequired(t *testing.T) {
	s := testServer(t, &Config{
		GatewayKey: "secret",
		Upstreams:  []Upstream{{Name: "ds", BaseURL: "http://x", APIKey: "k", ModelAlias: []string{"m"}}},
	})
	h := s.Handler()
	rec, m := doJSON(t, h, http.MethodGet, "/v1/models", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key = %d, want 401; %v", rec.Code, m)
	}
	if errMsg, _ := m["error"].(map[string]any); errMsg["code"] != "invalid_api_key" {
		t.Fatalf("error body = %v", m)
	}
	rec, _ = doJSON(t, h, http.MethodGet, "/v1/models", "", "wrong")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key = %d, want 401", rec.Code)
	}
	rec, _ = doJSON(t, h, http.MethodGet, "/v1/models", "", "secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("correct key = %d, want 200", rec.Code)
	}
}

func TestAuthDisabledWhenKeyEmpty(t *testing.T) {
	s := testServer(t, &Config{Upstreams: []Upstream{
		{Name: "ds", BaseURL: "http://x", APIKey: "k", ModelAlias: []string{"m"}},
	}})
	rec, _ := doJSON(t, s.Handler(), http.MethodGet, "/v1/models", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty key must allow: %d", rec.Code)
	}
}

func TestCompletionsNotImplemented(t *testing.T) {
	s := testServer(t, &Config{Upstreams: []Upstream{
		{Name: "ds", BaseURL: "http://x", APIKey: "k", ModelAlias: []string{"m"}},
	}})
	rec, m := doJSON(t, s.Handler(), http.MethodPost, "/v1/completions", `{"model":"m"}`, "")
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("completions = %d, want 501", rec.Code)
	}
	if errMsg, _ := m["error"].(map[string]any); errMsg["code"] != "not_implemented" {
		t.Fatalf("error body = %v", m)
	}
}

func TestOpenAIProtocolHelpers(t *testing.T) {
	messages := []json.RawMessage{
		json.RawMessage(`{"role":"system","content":"base"}`),
		json.RawMessage(`{"role":"user","content":"hello"}`),
	}
	// lastUserMessage picks the last user message.
	got, err := lastUserMessage(messages)
	if err != nil || got != "hello" {
		t.Fatalf("lastUserMessage = %q, %v", got, err)
	}
	// Multimodal content (parts array) yields concatenated text parts.
	multi := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":[{"type":"text","text":"a"},{"type":"image_url","image_url":{"url":"x"}},{"type":"text","text":"b"}]}`),
	}
	got, err = lastUserMessage(multi)
	if err != nil || got != "ab" {
		t.Fatalf("multimodal = %q, %v", got, err)
	}
	// Injection appends to the last system message.
	patched, err := injectIntoMessages(messages, "[semantix-reuse]x[/semantix-reuse]")
	if err != nil {
		t.Fatal(err)
	}
	if len(patched) != 2 {
		t.Fatalf("patched length = %d", len(patched))
	}
	var sys struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(patched[0], &sys); err != nil {
		t.Fatal(err)
	}
	if sys.Role != "system" || sys.Content != "base\n\n[semantix-reuse]x[/semantix-reuse]" {
		t.Fatalf("system patched = %+v", sys)
	}
	// No system message → a system message is prepended.
	only := []json.RawMessage{json.RawMessage(`{"role":"user","content":"hi"}`)}
	patched, err = injectIntoMessages(only, "block")
	if err != nil {
		t.Fatal(err)
	}
	if len(patched) != 2 {
		t.Fatalf("prepend length = %d", len(patched))
	}
	var first struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(patched[0], &first); err != nil {
		t.Fatal(err)
	}
	if first.Role != "system" || first.Content != "block" {
		t.Fatalf("prepended = %+v", first)
	}

	// Multiple system messages: the block lands at the END of the LAST one
	// (design §3.6 "system 提示末尾"), earlier system messages untouched.
	multiSys := []json.RawMessage{
		json.RawMessage(`{"role":"system","content":"first"}`),
		json.RawMessage(`{"role":"user","content":"u1"}`),
		json.RawMessage(`{"role":"system","content":"second"}`),
	}
	patched, err = injectIntoMessages(multiSys, "tail-block")
	if err != nil {
		t.Fatal(err)
	}
	var sys0, sys1 struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(patched[0], &sys0); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(patched[2], &sys1); err != nil {
		t.Fatal(err)
	}
	if sys0.Content != "first" {
		t.Fatalf("first system mutated: %+v", sys0)
	}
	if sys1.Content != "second\n\ntail-block" {
		t.Fatalf("last system not tail-injected: %+v", sys1)
	}
}

// TestServeLifecycle: Serve serves requests until the context is cancelled,
// then shuts down cleanly (graceful shutdown path used by `semantix serve`).
func TestServeLifecycle(t *testing.T) {
	cfg := gatewayConfig(t, "ds-chat")
	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		io.WriteString(w, jsonCompletion("up"))
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	url := "http://" + ln.Addr().String() + "/healthz"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("healthz during serve: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "ok") {
		t.Fatalf("healthz = %d %s", resp.StatusCode, body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve after cancel = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not shut down after cancel")
	}
}
