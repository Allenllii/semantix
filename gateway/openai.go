package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// This file defines the OpenAI-compatible wire protocol types and error
// format the gateway speaks (design §3.2). Error responses use the OpenAI
// shape {"error": {"message", "type", "code"}} so New API and clients
// recognize them.

// apiErrorBody is the OpenAI error envelope.
type apiErrorBody struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

// writeAPIError writes an OpenAI-format JSON error response.
func writeAPIError(w http.ResponseWriter, status int, typ, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(apiErrorBody{Error: apiError{
		Message: message,
		Type:    typ,
		Code:    code,
	}})
}

// --- chat completions request ---

// chatRequest is the minimal decoded form of POST /v1/chat/completions.
// Messages stay raw so the forwarded body is byte-faithful except for the
// two gateway-visible mutations (model mapping, L2 injection).
type chatRequest struct {
	Model    string            `json:"model"`
	Messages []json.RawMessage `json:"messages"`
	Stream   bool              `json:"stream,omitempty"`
	// Raw is the full original request body (for forwarding).
	Raw json.RawMessage `json:"-"`
}

// messageFields decodes just the fields the gateway needs from one message.
type messageFields struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// decodeChatRequest parses the request body (bounded) and keeps the raw
// bytes for forwarding.
func decodeChatRequest(r *http.Request) (*chatRequest, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err != nil {
		return nil, err
	}
	var cr chatRequest
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("invalid chat completion request: %w", err)
	}
	cr.Raw = body
	return &cr, nil
}

const maxRequestBodyBytes = 16 << 20 // 16 MiB

// lastUserMessage extracts the (normalized) text of the last user message.
// Content may be a plain string or an array of typed parts (multimodal);
// only text parts contribute.
func lastUserMessage(messages []json.RawMessage) (string, error) {
	for i := len(messages) - 1; i >= 0; i-- {
		var mf messageFields
		if err := json.Unmarshal(messages[i], &mf); err != nil {
			return "", fmt.Errorf("invalid message: %w", err)
		}
		if mf.Role != "user" {
			continue
		}
		return messageText(mf.Content)
	}
	return "", nil
}

// messageText renders a message content field (string or parts array).
func messageText(content json.RawMessage) (string, error) {
	if len(content) == 0 || string(content) == "null" {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &parts); err != nil {
		return "", fmt.Errorf("unsupported message content shape")
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String(), nil
}

// systemMessageText returns the content of the last system message, if any.
func systemMessageText(messages []json.RawMessage) (string, bool, error) {
	for i := len(messages) - 1; i >= 0; i-- {
		var mf messageFields
		if err := json.Unmarshal(messages[i], &mf); err != nil {
			return "", false, fmt.Errorf("invalid message: %w", err)
		}
		if mf.Role == "system" {
			t, err := messageText(mf.Content)
			return t, true, err
		}
	}
	return "", false, nil
}

// injectIntoMessages appends the L2 injection block to the end of the last
// system message's content (design §3.6: system-prompt tail, so history
// after the block stays byte-stable for L1). When no system message exists,
// a new one is prepended.
func injectIntoMessages(messages []json.RawMessage, block string) ([]json.RawMessage, error) {
	lastSystem := -1
	for i := range messages {
		var mf messageFields
		if err := json.Unmarshal(messages[i], &mf); err != nil {
			return nil, fmt.Errorf("invalid message: %w", err)
		}
		if mf.Role == "system" {
			lastSystem = i
		}
	}
	if lastSystem < 0 {
		// No system message: prepend a system message holding the block.
		prepended, err := json.Marshal(struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "system", Content: block})
		if err != nil {
			return nil, err
		}
		out := make([]json.RawMessage, 0, len(messages)+1)
		out = append(out, prepended)
		return append(out, messages...), nil
	}

	out := make([]json.RawMessage, 0, len(messages))
	for i, raw := range messages {
		if i != lastSystem {
			out = append(out, raw)
			continue
		}
		var mf messageFields
		if err := json.Unmarshal(raw, &mf); err != nil {
			return nil, fmt.Errorf("invalid message: %w", err)
		}
		var content string
		if err := json.Unmarshal(mf.Content, &content); err == nil {
			content += "\n\n" + block
			patchedRaw, err := json.Marshal(struct {
				Role       string          `json:"role"`
				Content    string          `json:"content"`
				ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
				Name       string          `json:"name,omitempty"`
				ToolCallID string          `json:"tool_call_id,omitempty"`
			}{Role: mf.Role, Content: content, ToolCalls: mf.ToolCalls})
			if err != nil {
				return nil, err
			}
			out = append(out, patchedRaw)
			continue
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(mf.Content, &parts); err == nil {
			parts = append(parts, struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{Type: "text", Text: "\n\n" + block})
			patchedRaw, err := json.Marshal(struct {
				Role       string          `json:"role"`
				Content    any             `json:"content"`
				ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
				Name       string          `json:"name,omitempty"`
				ToolCallID string          `json:"tool_call_id,omitempty"`
			}{Role: mf.Role, Content: parts, ToolCalls: mf.ToolCalls, Name: mf.Name})
			if err != nil {
				return nil, err
			}
			out = append(out, patchedRaw)
			continue
		}
		// Unknown content shape: leave the message untouched.
		out = append(out, raw)
	}
	return out, nil
}

// --- chat completions response ---

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatRespMsg `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type chatRespMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type usageInfo struct {
	PromptTokens     int                 `json:"prompt_tokens"`
	CompletionTokens int                 `json:"completion_tokens"`
	TotalTokens      int                 `json:"total_tokens"`
	PromptDetails    *promptTokenDetails `json:"prompt_tokens_details,omitempty"`
}

type promptTokenDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

type chatCompletion struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *usageInfo   `json:"usage,omitempty"`
}
