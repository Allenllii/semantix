package gateway

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"semantix/kernel/cache"
	"semantix/kernel/inject"
	"semantix/kernel/usage"
	"semantix/kernel/zone"
)

// handleChatCompletions is the core pipeline (design §3.3):
//
//  1. auth (middleware)          5. model mapping + forward upstream
//  2. normalize (query, ctxHash) 6. passthrough content + usage
//  3. L3 lookup (hit → return)   7. async write-memory (landed with the
//  4. L2 injection                   write-memory commit)
//
// The gateway never blocks the main chain on local operations; upstream
// failures surface as OpenAI-format errors so the client / New API can retry.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error",
			"method_not_allowed", "chat completions only supports POST")
		return
	}
	cr, err := decodeChatRequest(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error",
			"invalid_request", err.Error())
		return
	}
	if cr.Model == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error",
			"invalid_request", "model is required")
		return
	}
	up, ok := s.up.resolve(cr.Model)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error",
			"model_not_found", fmt.Sprintf("model %q is not configured on this gateway", cr.Model))
		return
	}

	query, err := lastUserMessage(cr.Messages)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error",
			"invalid_request", err.Error())
		return
	}
	query = normalizeQuery(query)
	ctxHash := contextHash(cr.Raw)
	sessionID := sessionID(r)

	// Step 3: L3 verified-result reuse (design §3.5). A hit returns the
	// cached response with zero upstream calls. Without a configured deps
	// root the L3 gate cannot verify anything (and must not verify against
	// the gateway process CWD) — fail-closed, skip the lookup entirely.
	if !s.cfg.Disable && query != "" && s.cfg.Cache.DepsRoot != "" {
		if hit := s.lookupL3(r.Context(), query, cr.Model, ctxHash); hit != nil {
			s.serveL3Hit(w, r, cr, sessionID, hit)
			return
		}
	}

	// Step 4: L2 semantic injection (design §3.6) — appends a deterministic
	// reuse block to the system prompt so the model skips repeated
	// exploration. Byte-stable block keeps the upstream prefix cache warm.
	block := ""
	if !s.cfg.Disable && query != "" {
		z := zone.Default()
		inj, err := (&inject.Injector{
			Index:  s.index,
			Scope:  s.cfg.Scope,
			K:      s.cfg.Retrieval.TopK,
			Budget: s.cfg.Retrieval.Budget,
			Zones:  &z,
		}).Build(query)
		if err != nil {
			s.logf("L2 inject: %v", err)
		} else if inj.Text != "" {
			block = inj.Text
		}
	}

	// Step 5: build the forwarded body — model mapping + (maybe) injected
	// messages, deterministically re-encoded so identical requests forward
	// byte-identically (L1 prefix caching).
	messages := cr.Messages
	if block != "" {
		patched, err := injectIntoMessages(cr.Messages, block)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error",
				"invalid_request", err.Error())
			return
		}
		messages = patched
	}
	body, err := rebuildBody(cr.Raw, messages, s.up.upstreamModel(cr.Model))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error",
			"internal_error", err.Error())
		return
	}

	meta := forwardMeta{
		sessionID: sessionID,
		model:     cr.Model,
		query:     query,
		ctxHash:   ctxHash,
		injected:  len(block),
		promptEst: estTokens(string(body)),
	}
	if cr.Stream {
		s.forwardStream(w, r, up, body, meta)
		return
	}
	s.forwardJSON(w, r, up, body, meta)
}

// forwardMeta carries pipeline observability to the forward + write-memory
// stages.
type forwardMeta struct {
	sessionID string
	model     string
	query     string
	ctxHash   string
	injected  int // injected block bytes (0 = none)
	promptEst int // estimated input tokens (incl. injection)
}

// --- L3 ---

// lookupL3 runs the kernel fail-closed L3 gate (kernel/cache.L3Decider:
// retrieval + grey zone + deps/mtime/fingerprint verification) and then the
// gateway-specific reuse gates (design §3.5 cache key): model match,
// messages-context fingerprint match and TTL.
//
// Every gate is fail-closed: an entry without the gateway metadata
// (Model/ContextHash — e.g. a CLI-extracted Result slice), an unknown-age
// entry (CreatedAt == 0) or a stale entry is never served.
func (s *Server) lookupL3(ctx context.Context, query, model, ctxHash string) *cache.L3Result {
	decider := &cache.L3Decider{
		Index: s.index,
		Store: s.store,
		Root:  s.cfg.Cache.DepsRoot,
		K:     s.cfg.Retrieval.TopK,
	}
	res, err := decider.DecideL3(ctx, cache.Query{
		UserInput: query,
		Scope:     s.cfg.Scope,
	})
	if err != nil || res == nil {
		return nil
	}
	sl, err := s.store.Get(res.SliceID)
	if err != nil || sl == nil {
		return nil
	}
	if sl.Meta.Model == "" || sl.Meta.ContextHash == "" {
		// Gateway entries always carry both; anything else is not
		// verified for gateway reuse (review: fail-open on missing meta).
		s.logf("L3 reject: entry %s lacks gateway meta (model=%q context=%q)",
			sl.ID, sl.Meta.Model, sl.Meta.ContextHash)
		return nil
	}
	if sl.Meta.Model != model {
		s.logf("L3 reject: model %q != %q", sl.Meta.Model, model)
		return nil
	}
	if sl.Meta.ContextHash != ctxHash {
		s.logf("L3 reject: context mismatch")
		return nil
	}
	if sl.CreatedAt <= 0 ||
		(s.cfg.Cache.TTLSeconds > 0 && time.Now().Unix()-sl.CreatedAt > s.cfg.Cache.TTLSeconds) {
		s.logf("L3 reject: TTL expired")
		return nil
	}
	if res.Response == "" {
		// An empty answer is not a reusable outcome (gateway write-back
		// never produces one; reject defensively).
		s.logf("L3 reject: empty cached response %s", sl.ID)
		return nil
	}
	return res
}

// serveL3Hit answers a verified cache hit: synthetic usage (design §4.3:
// completion_tokens=0, all prompt tokens marked cached) and — for stream
// requests — an SSE replay of the cached content (§3.4).
func (s *Server) serveL3Hit(w http.ResponseWriter, r *http.Request, cr *chatRequest, sessionID string, hit *cache.L3Result) {
	w.Header().Set("x-semantix-cache", "hit")
	w.Header().Set("x-semantix-slice", hit.SliceID)

	estIn := estTokens(string(cr.Raw))
	go s.recordUsage(usage.Event{
		SessionID:     sessionID,
		TokensIn:      int64(estIn),
		TokensOut:     0,
		CacheHitToken: int64(estIn),
		L3Reuse:       true,
	})

	if cr.Stream {
		s.replayL3Stream(w, cr, hit)
		return
	}
	resp := chatCompletion{
		ID:      "chatcmpl-semantix-" + hit.SliceID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   cr.Model,
		Choices: []chatChoice{{
			Index:        0,
			Message:      chatRespMsg{Role: "assistant", Content: hit.Response},
			FinishReason: "stop",
		}},
		Usage: &usageInfo{
			PromptTokens:     estIn,
			CompletionTokens: 0,
			TotalTokens:      estIn,
			PromptDetails:    &promptTokenDetails{CachedTokens: estIn},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(resp)
}

// replayL3Stream rebuilds an OpenAI SSE stream from the cached response
// (design §3.4): role chunk, content chunks (≤4KB), finish chunk, [DONE].
func (s *Server) replayL3Stream(w http.ResponseWriter, cr *chatRequest, hit *cache.L3Result) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flush := func() { _ = http.NewResponseController(w).Flush() }

	id := "chatcmpl-semantix-" + hit.SliceID
	created := time.Now().Unix()
	chunk := func(delta map[string]any, finish any) {
		payload, _ := json.Marshal(map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   cr.Model,
			"choices": []map[string]any{{
				"index":         0,
				"delta":         delta,
				"finish_reason": finish,
			}},
		})
		_, _ = io.WriteString(w, "data: "+string(payload)+"\n\n")
		flush()
	}

	chunk(map[string]any{"role": "assistant", "content": ""}, nil)
	content := hit.Response
	for len(content) > 0 {
		n := min(len(content), 4096)
		chunk(map[string]any{"content": content[:n]}, nil)
		content = content[n:]
	}
	chunk(map[string]any{}, "stop")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flush()
}

// --- forward ---

// forwardJSON forwards a non-stream request and passes the upstream body
// through byte-exact (design §3.4 byte-stability note). The response is
// parsed (in a copy) only for write-memory/usage decisions. Upstream
// error responses (status >= 400) are passed through untouched and never
// enter the write-memory path.
func (s *Server) forwardJSON(w http.ResponseWriter, r *http.Request, up *Upstream, body []byte, meta forwardMeta) {
	ctx, cancel := context.WithTimeout(r.Context(), s.upstreamTimeout(up))
	defer cancel()

	respBody, status, retried, err := s.doUpstream(ctx, up, body)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error", "upstream_error",
			fmt.Sprintf("upstream request failed: %v", err))
		return
	}
	defer respBody.Close()

	// Read the full body before writing any status/headers: a read failure
	// must surface as a clean 502, not a 200 with a truncated body.
	data, readErr := io.ReadAll(io.LimitReader(respBody, maxResponseBodyBytes))
	if readErr != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error", "upstream_error",
			fmt.Sprintf("read upstream response: %v", readErr))
		return
	}

	w.Header().Set("x-semantix-cache", "miss")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)

	if retried {
		s.logf("upstream retry succeeded (%s)", up.Name)
	}
	if status >= 400 {
		// Error responses are not conversations: no usage record, no
		// session write, no L3 write-back.
		return
	}

	content, toolCalls := parseCompletionContent(data)
	s.afterResponse(meta, content, toolCalls)
}

// forwardStream pipes the upstream SSE stream through verbatim, line by
// line (design §3.4: 逐块透传，不重排不重写), injecting a usage chunk before
// [DONE] only when the upstream omitted usage and L2 injected tokens exist.
//
// Upstream error responses (status >= 400) are NOT SSE: they are passed
// through as their original JSON body so the client can surface the error
// (e.g. 429 rate limit) instead of hanging on a stream that never ends.
func (s *Server) forwardStream(w http.ResponseWriter, r *http.Request, up *Upstream, body []byte, meta forwardMeta) {
	ctx, cancel := context.WithTimeout(r.Context(), s.upstreamTimeout(up))
	defer cancel()

	respBody, status, retried, err := s.doUpstream(ctx, up, body)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error", "upstream_error",
			fmt.Sprintf("upstream request failed: %v", err))
		return
	}
	defer respBody.Close()

	w.Header().Set("x-semantix-cache", "miss")
	if status >= 400 {
		// Upstream refused the request: relay its error verbatim (it is
		// already in OpenAI error format) — never as an SSE stream.
		data, readErr := io.ReadAll(io.LimitReader(respBody, maxResponseBodyBytes))
		if readErr != nil {
			writeAPIError(w, http.StatusBadGateway, "upstream_error", "upstream_error",
				fmt.Sprintf("read upstream error: %v", readErr))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(data)
		if retried {
			s.logf("upstream retry succeeded (%s)", up.Name)
		}
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)

	flush := func() { _ = http.NewResponseController(w).Flush() }
	br := bufio.NewReader(respBody)

	var contentBuf strings.Builder
	toolCallsSeen := false
	usageSeen := false
	sawDone := false
	writeFailed := false

	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := strings.TrimSpace(string(line))
			if strings.HasPrefix(trimmed, "data: ") {
				payload := strings.TrimPrefix(trimmed, "data: ")
				if payload == "[DONE]" {
					// Defer [DONE]: a usage chunk may need to precede it
					// (design §3.4). Any lines after [DONE] are abnormal —
					// dropped, the stream is over.
					sawDone = true
					continue
				}
				if sawDone {
					continue // already terminated upstream: ignore tail
				}
				var evt struct {
					Choices []struct {
						Delta struct {
							Content   string `json:"content"`
							ToolCalls any    `json:"tool_calls"`
						} `json:"delta"`
					} `json:"choices"`
					Usage *usageInfo `json:"usage"`
				}
				if json.Unmarshal([]byte(payload), &evt) == nil {
					for _, c := range evt.Choices {
						contentBuf.WriteString(c.Delta.Content)
						if c.Delta.ToolCalls != nil {
							toolCallsSeen = true
						}
					}
					if evt.Usage != nil {
						usageSeen = true
					}
				}
			}
			if _, werr := w.Write(line); werr != nil {
				writeFailed = true
				break
			}
			flush()
		}
		if err != nil {
			break
		}
	}

	if !writeFailed {
		// Design §3.4: fill in a usage chunk before [DONE] when the
		// upstream omitted usage and the gateway injected tokens
		// (observability for L1 savings). The chunk carries the standard
		// SSE envelope so strict clients parse it like any other chunk.
		if !usageSeen && meta.injected > 0 {
			est := estTokens(string(body))
			payload, _ := json.Marshal(map[string]any{
				"id":      "chatcmpl-semantix-usage",
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   meta.model,
				"choices": []map[string]any{},
				"usage": map[string]any{
					"prompt_tokens":     est,
					"completion_tokens": estTokens(contentBuf.String()),
					"total_tokens":      est + estTokens(contentBuf.String()),
					"prompt_tokens_details": map[string]any{
						"cached_tokens": estTokensN(meta.injected),
					},
				},
			})
			_, _ = io.WriteString(w, "data: "+string(payload)+"\n\n")
			flush()
		}
		// [DONE] always terminates (rewritten if the upstream sent it).
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flush()
	}

	if !writeFailed {
		if retried {
			s.logf("upstream retry succeeded (%s)", up.Name)
		}
		s.afterResponse(meta, contentBuf.String(), toolCallsSeen)
	}
}

// parseCompletionContent extracts (content, hasToolCalls) from a non-stream
// upstream completion response.
func parseCompletionContent(data []byte) (string, bool) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls any    `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", false
	}
	for _, c := range resp.Choices {
		if c.Message.ToolCalls != nil {
			return c.Message.Content, true
		}
		if c.Message.Content != "" {
			return c.Message.Content, false
		}
	}
	return "", false
}

// doUpstream performs the HTTP call with one retry on network errors only
// (design §3.8: retry logic lives mostly at the New API channel; the
// gateway only does a single best-effort retry for transport errors — the
// body is fully buffered, so a resend is always safe).
func (s *Server) doUpstream(ctx context.Context, up *Upstream, body []byte) (io.ReadCloser, int, bool, error) {
	url := strings.TrimSuffix(up.BaseURL, "/") + "/chat/completions"
	do := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+up.APIKey)
		req.Header.Set("Content-Type", "application/json")
		return s.client.Do(req)
	}
	resp, err := do()
	if err != nil {
		resp, err = do()
		if err != nil {
			return nil, 0, true, err
		}
		return resp.Body, resp.StatusCode, true, nil
	}
	return resp.Body, resp.StatusCode, false, nil
}

func (s *Server) upstreamTimeout(up *Upstream) time.Duration {
	sec := up.TimeoutSec
	if sec <= 0 {
		sec = 60
	}
	return time.Duration(sec) * time.Second
}

// --- helpers ---

// normalizeQuery collapses whitespace and strips control characters
// (design §3.5: 归一化 query，复用 sanitize 纪律).
func normalizeQuery(q string) string {
	var b strings.Builder
	b.Grow(len(q))
	lastSpace := false
	for _, r := range q {
		if r <= 0x20 || r == 0x7f {
			if !lastSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			lastSpace = true
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimSpace(b.String())
}

// contextHash is the messages-context fingerprint (design §3.5): the sha256
// of the exact request bytes, gating L3 reuse to the same context.
func contextHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// rebuildBody deterministically re-encodes the request with the mapped
// model and (possibly injected) messages. encoding/json marshals maps with
// sorted keys, so identical inputs yield byte-identical output — the L1
// prefix-cache requirement (design §2.1).
func rebuildBody(raw []byte, messages []json.RawMessage, model string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	msgJSON, err := json.Marshal(messages)
	if err != nil {
		return nil, err
	}
	modelJSON, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	fields["messages"] = msgJSON
	fields["model"] = modelJSON
	return json.Marshal(fields)
}

// sessionID derives the write-memory session id from the client header
// (design §3.7: gateway-session-id), falling back to a shared default.
func sessionID(r *http.Request) string {
	if id := r.Header.Get("x-semantix-session"); id != "" {
		return id
	}
	return "default"
}

// recordUsage appends a usage event (best-effort, never blocks the chain).
func (s *Server) recordUsage(e usage.Event) {
	if s.usage == nil {
		return
	}
	if e.At == 0 {
		e.At = time.Now().Unix()
	}
	if err := s.usage.Append(e); err != nil {
		s.logf("usage record: %v", err)
	}
}

// estTokens is a crude token estimate (≈ bytes/4, design §4.3 synthetic
// usage; the gateway never bills — New API does).
func estTokens(s string) int { return estTokensN(len(s)) }

// estTokensN estimates tokens for a byte count.
func estTokensN(n int) int {
	if n <= 0 {
		return 0
	}
	return (n + 3) / 4
}

const maxResponseBodyBytes = 32 << 20 // 32 MiB

// afterResponse runs the post-forward write-memory path (design §3.7):
// usage record + session bypass write + async extraction + L3 write-back.
// Everything is best-effort and never blocks the main chain; the deps-tree
// fingerprint capture for the write-back runs on the worker goroutine.
func (s *Server) afterResponse(meta forwardMeta, content string, toolCalls bool) {
	injectedEst := estTokensN(meta.injected)
	s.recordUsage(usage.Event{
		SessionID:      meta.sessionID,
		TokensIn:       int64(meta.promptEst),
		TokensOut:      int64(estTokens(content)),
		CacheHitToken:  int64(injectedEst),
		InjectedTokens: int64(injectedEst),
	})

	if s.mem != nil {
		var calls []sessionToolCall
		if toolCalls {
			calls = []sessionToolCall{{ID: "gateway-tool", Name: "tool_call"}}
		}
		s.mem.appendSession(meta.sessionID, []sessionLine{
			{Role: "user", Content: meta.query},
			{Role: "assistant", Content: content, ToolCalls: calls},
		})
		s.mem.submitSession(meta.sessionID)
		s.mem.submitWriteback(meta, content, toolCalls)
	}
}
