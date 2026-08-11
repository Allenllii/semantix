package judge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"semantix/kernel/zone"
)

// TestLLMJudgeOpenAIProtocol verifies the OpenAI-format request and
// yes/no parsing against a mock server.
func TestLLMJudgeOpenAIProtocol(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"yes"}}]}`))
	}))
	defer srv.Close()

	j, err := NewLLMJudge(LLMConfig{
		Protocol: "openai", BaseURL: srv.URL, Model: "gpt-4o-mini", APIKey: "sk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := j.Confirm(context.Background(), cand(zone.Hit))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want yes -> reuse acceptable")
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Errorf("auth header = %q, want Bearer", gotAuth)
	}
	if gotBody["model"] != "gpt-4o-mini" {
		t.Errorf("model = %v", gotBody["model"])
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2 (system+user)", len(msgs))
	}
}

// TestLLMJudgeAnthropicProtocol verifies the Messages-API request format.
func TestLLMJudgeAnthropicProtocol(t *testing.T) {
	var gotBody map[string]any
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		if r.URL.Path != "/messages" {
			t.Errorf("path = %q, want /messages", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"content":[{"type":"text","text":"no"}]}`))
	}))
	defer srv.Close()

	j, err := NewLLMJudge(LLMConfig{
		Protocol: "anthropic", BaseURL: srv.URL, Model: "claude-sonnet-4-5", APIKey: "sk-ant-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := j.Confirm(context.Background(), cand(zone.Hit))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want no -> not acceptable")
	}
	if gotKey != "sk-ant-test" {
		t.Errorf("x-api-key = %q", gotKey)
	}
	if gotBody["model"] != "claude-sonnet-4-5" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if _, ok := gotBody["system"]; !ok {
		t.Error("anthropic request missing system field")
	}
}

func TestLLMJudgeConfigValidation(t *testing.T) {
	if _, err := NewLLMJudge(LLMConfig{Protocol: "gemini", BaseURL: "x", Model: "m", APIKey: "k"}); err == nil {
		t.Error("unsupported protocol must error")
	}
	if _, err := NewLLMJudge(LLMConfig{Protocol: "openai", Model: "m", APIKey: "k"}); err == nil {
		t.Error("missing base-url must error")
	}
	if _, err := NewLLMJudge(LLMConfig{Protocol: "openai", BaseURL: "x", Model: "m"}); err == nil {
		t.Error("missing api key must error")
	}
}

func TestLLMJudgeHTTPErrorConservative(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	j, _ := NewLLMJudge(LLMConfig{Protocol: "openai", BaseURL: srv.URL, Model: "m", APIKey: "k"})
	if _, err := j.Confirm(context.Background(), cand(zone.Hit)); err == nil {
		t.Fatal("HTTP error must surface (caller treats as conservative reject)")
	}
}
