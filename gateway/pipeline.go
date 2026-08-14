package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"semantix/kernel/cache"
	"semantix/kernel/inject"
	"semantix/kernel/usage"
)

// handleChat runs the full pipeline for POST /v1/chat/completions
// (design §3.3): auth is done in the router; here: normalize → L3 → L2 →
// forward upstream → passthrough/SSE → sidecar + usage.
func (g *Gateway) handleChat(w http.ResponseWriter, r *http.Request, body []byte) {
	req, err := parseChatRequest(body)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	up, ok := g.cfg.UpstreamFor(req.Model)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "model_not_found",
			fmt.Sprintf("model %q is not routed by this gateway", req.Model))
		return
	}
	query, _ := lastUserText(req.Messages)
	chash, err := contextHash(req.Messages)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	scope, err := g.resolveScope(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	sessionID := r.Header.Get("x-semantix-session")
	ctx := r.Context()
	q := cache.Query{UserInput: query, ContextHash: chash, Scope: scope}

	// L3: verified reuse — zero upstream calls (design §3.3 step 3).
	if !g.disabled {
		if res, lerr := g.decider.DecideL3(ctx, q); lerr == nil && res != nil && g.l3Eligible(res.SliceID) {
			g.recordUsage(usage.Event{
				SessionID: sessionID, TokensIn: int64(len(query)/4) + int64(len(res.Response)/4),
				TokensOut: 0, CacheHitToken: int64(len(res.Response) / 4),
				L3Reuse: true, At: g.now().Unix(),
			})
			g.replyFromCache(w, r, req, res)
			return
		}
	}

	// L2: inject reuse context, then forward (design §3.3 steps 4-5).
	var injectedTokens int64
	inj, ierr := g.injector.Build(query)
	if ierr != nil {
		log.Printf("gateway: inject: %v", ierr) // never blocks the main path
	}
	if inj != nil {
		injectedTokens = int64(inj.Bytes / 4)
		body = g.attachInjection(body, req, inj)
	}

	resp, ferr := g.forward(ctx, up, body)
	if ferr != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error",
			fmt.Sprintf("upstream request failed: %v", ferr))
		return
	}
	defer resp.Body.Close()

	if req.Stream {
		g.streamThrough(w, resp, sessionID, req, query, injectedTokens)
		return
	}
	g.passthrough(w, resp, sessionID, req, query, injectedTokens)
}

// l3Eligible applies the gateway-side TTL window on top of the kernel
// verification chain. A slice that disappeared from the store also fails
// closed.
func (g *Gateway) l3Eligible(id string) bool {
	s, err := g.store.Get(id)
	if err != nil {
		return false
	}
	return g.cacheFresh(s)
}

// attachInjection rewrites the outgoing body: the injection block is
// appended to the first system message (byte-stable prefix tail, L1) or
// prepended as a new system message when none exists. All other request
// fields pass through untouched.
func (g *Gateway) attachInjection(body []byte, req *chatRequest, inj *inject.Injection) []byte {
	if inj == nil || inj.Text == "" {
		return body
	}
	messages := append([]chatMessage(nil), req.Messages...)
	messages = attachBlock(messages, inj.Text)

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return body
	}
	raw["messages"] = messages
	out, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return out
}

// attachBlock appends block to the first string-content system message, or
// prepends a system message when none exists (design §3.6 injection slot).
func attachBlock(messages []chatMessage, block string) []chatMessage {
	for i := range messages {
		if messages[i].Role != "system" {
			continue
		}
		if s, ok := messages[i].Content.(string); ok {
			messages[i].Content = s + "\n\n" + block
			return messages
		}
	}
	return append([]chatMessage{{Role: "system", Content: block}}, messages...)
}

// forward posts the (rewritten) body to the upstream OpenAI-compatible
// endpoint with the upstream API key.
func (g *Gateway) forward(ctx context.Context, up UpstreamConfig, body []byte) (*http.Response, error) {
	url := strings.TrimRight(up.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+up.APIKey)
	return g.client.Do(req)
}

// passthrough relays a non-streaming upstream response, records usage and
// writes the session sidecar (request turns + assistant reply).
func (g *Gateway) passthrough(w http.ResponseWriter, resp *http.Response, sessionID string, req *chatRequest, query string, injectedTokens int64) {
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error",
			fmt.Sprintf("read upstream response: %v", err))
		return
	}
	content := extractAssistantContent(out)
	g.recordSession(sessionID, turns(req, content))

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("x-semantix-cache", "miss")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(out)

	if resp.StatusCode >= 400 {
		return // upstream error: nothing reusable happened
	}
	g.recordUsage(usage.Event{
		SessionID: sessionID,
		TokensIn:  int64(len(query)/4) + injectedTokens,
		TokensOut: int64(len(out) / 4),
		CacheHitToken: 0,
		InjectedTokens: injectedTokens,
		At: g.now().Unix(),
	})
}

// streamThrough relays a streaming upstream response chunk by chunk (SSE),
// preserving events verbatim (design §3.4: never reorder/rewrite the
// upstream stream). Sidecar records the request turns only — parsing the
// assistant content out of the SSE chunks is deferred (documented debt).
func (g *Gateway) streamThrough(w http.ResponseWriter, resp *http.Response, sessionID string, req *chatRequest, query string, injectedTokens int64) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("x-semantix-cache", "miss")
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)

	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}
	if resp.StatusCode < 400 {
		g.recordSession(sessionID, turns(req, ""))
		g.recordUsage(usage.Event{
			SessionID: sessionID,
			TokensIn:  int64(len(query)/4) + injectedTokens,
			InjectedTokens: injectedTokens,
			At: g.now().Unix(),
		})
	}
}

// turns renders the request messages plus the assistant reply as the
// ingest.JSONLSource-compatible sidecar lines.
func turns(req *chatRequest, assistant string) []map[string]any {
	out := make([]map[string]any, 0, len(req.Messages)+1)
	for _, m := range req.Messages {
		out = append(out, map[string]any{"role": m.Role, "content": textParts(m.Content)})
	}
	if assistant != "" {
		out = append(out, map[string]any{"role": "assistant", "content": assistant})
	}
	return out
}

// extractAssistantContent pulls choices[0].message.content out of a
// non-streaming OpenAI response (best-effort; empty on parse failure).
func extractAssistantContent(body []byte) string {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Message.Content
}

// recordUsage appends one kernel/usage event (best-effort).
func (g *Gateway) recordUsage(e usage.Event) {
	if g.usageLog == nil {
		return
	}
	if err := g.usageLog.Append(e); err != nil {
		log.Printf("gateway: usage append: %v", err)
	}
}

// ---------------------------------------------------------------------------
// L3 hit responses (design §3.4)

// chatCompletion is the synthetic non-streaming response for an L3 hit.
type chatCompletion struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Choices []choicePayload `json:"choices"`
	Usage   usagePayload    `json:"usage"`
}

type choicePayload struct {
	Index        int           `json:"index"`
	Message      messagePayload `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type messagePayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type usagePayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	PromptDetails    struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// replyFromCache serves a verified L3 hit: a plain JSON response, or a
// reconstructed SSE stream for stream=true (design §3.4 streaming replay).
func (g *Gateway) replyFromCache(w http.ResponseWriter, r *http.Request, req *chatRequest, res *cache.L3Result) {
	w.Header().Set("x-semantix-cache", "hit")
	if req.Stream {
		g.replayStream(w, r, req, res)
		return
	}
	cached := len(res.Response) / 4
	query, _ := lastUserText(req.Messages)
	prompt := len(query) / 4
	body := chatCompletion{
		ID:      "chatcmpl-" + res.SliceID,
		Object:  "chat.completion",
		Created: g.now().Unix(),
		Model:   req.Model,
		Choices: []choicePayload{{
			Index:        0,
			Message:      messagePayload{Role: "assistant", Content: res.Response},
			FinishReason: "stop",
		}},
		Usage: usagePayload{
			PromptTokens:     prompt + cached,
			CompletionTokens: 0,
			TotalTokens:      prompt + cached,
		},
	}
	body.Usage.PromptDetails.CachedTokens = cached
	raw, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// replayStream rebuilds an SSE stream from the cached response, chunking the
// content into deltas (design §3.4 streaming replay; MVP chunk size 256B).
func (g *Gateway) replayStream(w http.ResponseWriter, r *http.Request, req *chatRequest, res *cache.L3Result) {
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	base := struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
	}{"chatcmpl-" + res.SliceID, "chat.completion.chunk", g.now().Unix(), req.Model}

	writeChunk := func(delta map[string]any, finish any) {
		evt := map[string]any{
			"id": base.ID, "object": base.Object, "created": base.Created, "model": base.Model,
			"choices": []map[string]any{{
				"index": 0, "delta": delta, "finish_reason": finish,
			}},
		}
		raw, _ := json.Marshal(evt)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		if flusher != nil {
			flusher.Flush()
		}
	}

	writeChunk(map[string]any{"role": "assistant"}, nil)
	content := res.Response
	for len(content) > 0 {
		n := 256
		if len(content) < n {
			n = len(content)
		}
		writeChunk(map[string]any{"content": content[:n]}, nil)
		content = content[n:]
	}
	writeChunk(map[string]any{}, "stop")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
