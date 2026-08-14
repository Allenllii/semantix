package gateway

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// TestStreamUpstreamErrorNotSSE: when the upstream rejects a stream request
// (e.g. 429 rate limit), the gateway must relay the JSON error — not wrap
// it in an SSE stream that never terminates (clients would hang waiting
// for [DONE]).
func TestStreamUpstreamErrorNotSSE(t *testing.T) {
	cfg := gatewayConfig(t, "ds-chat")
	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"rate limited","type":"rate_limit_error","code":"rate_limit_exceeded"}}`)
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	body := requestBody("ds-chat", true, "user:hi")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json (not SSE)", ct)
	}
	if strings.Contains(rec.Body.String(), "[DONE]") || strings.Contains(rec.Body.String(), "chat.completion.chunk") {
		t.Fatalf("upstream error must not be SSE-ified: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "rate limited") {
		t.Fatalf("error body not relayed: %s", rec.Body.String())
	}
}

// TestJSONUpstreamErrorSkipsWriteMemory: an upstream error response is not
// a conversation — no session transcript, no usage event.
func TestJSONUpstreamErrorSkipsWriteMemory(t *testing.T) {
	sessions := filepath.Join(t.TempDir(), "sessions")
	usageDB := filepath.Join(t.TempDir(), "usage.jsonl")
	cfg := gatewayConfig(t, "ds-chat")
	cfg.Ingest.SessionsDir = sessions
	cfg.Ingest.Extract = true
	cfg.UsageDB = usageDB

	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"bad model","type":"invalid_request_error"}}`)
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	body := requestBody("ds-chat", false, "user:trigger an error")
	rec, _ := doJSON(t, s.Handler(), http.MethodPost, "/v1/chat/completions", string(body), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	entries, err := os.ReadDir(sessions)
	if err != nil || len(entries) != 0 {
		t.Fatalf("error response must not write a session transcript: %v %v", entries, err)
	}
	raw, err := os.ReadFile(usageDB)
	if err != nil {
		t.Fatalf("usage db: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "" {
		t.Fatalf("error response must not write usage events:\n%s", raw)
	}
}

// TestNewCreatesStoreParentDir: the very first `semantix serve` run must
// work even when ~/.semantix does not exist yet (parent dir auto-created).
func TestNewCreatesStoreParentDir(t *testing.T) {
	db := filepath.Join(t.TempDir(), "does", "not", "exist", "yet", "gw.jsonl")
	s, err := New(Config{
		StoreDB:   db,
		Scope:     slice.Project,
		Upstreams: []Upstream{{Name: "x", BaseURL: "http://x", APIKey: "k", ModelAlias: []string{"m"}}},
		Retrieval: struct {
			TopK   int
			Budget int
		}{TopK: 5, Budget: 4096},
	})
	if err != nil {
		t.Fatalf("New with missing parent dir: %v", err)
	}
	defer s.Close()
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("store file not created: %v", err)
	}
}

// TestStreamTailAfterDoneDropped: lines the upstream sends after [DONE] are
// abnormal and must not leak into the client stream.
func TestStreamTailAfterDoneDropped(t *testing.T) {
	cfg := gatewayConfig(t, "ds-chat")
	up := newTestUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}`)
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		// Abnormal tail: must be dropped by the gateway.
		fmt.Fprint(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"TAIL\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
	})
	cfg.Upstreams[0].BaseURL = up.url()
	s := testServer(t, &cfg)

	body := requestBody("ds-chat", true, "user:stream")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "TAIL") {
		t.Fatalf("tail after [DONE] leaked into the stream: %s", rec.Body.String())
	}
	if !strings.HasSuffix(strings.TrimSpace(rec.Body.String()), "data: [DONE]") {
		t.Fatalf("stream must end with [DONE]: %q", rec.Body.String())
	}
}

// TestEnvInvalidScopeFails: a typo in SEMANTIX_GATEWAY_SCOPE must fail
// startup instead of silently falling back to the default.
func TestEnvInvalidScopeFails(t *testing.T) {
	t.Setenv("SEMANTIX_GATEWAY_SCOPE", "projct") // typo
	if _, err := Load(Options{}); err == nil || !strings.Contains(err.Error(), "SEMANTIX_GATEWAY_SCOPE") {
		t.Fatalf("invalid env scope must fail startup, got %v", err)
	}
}
