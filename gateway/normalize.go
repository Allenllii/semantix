package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// chatRequest is the OpenAI-compatible subset the gateway consumes. Unknown
// fields are ignored and forwarded verbatim upstream (the upstream is the
// protocol authority; the gateway only rewrites what it must).
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature *float64      `json:"temperature"`
}

// chatMessage is one OpenAI chat message. Content may be a plain string or
// a multimodal part array (text/image urls); the gateway only reads text
// parts for query extraction and fingerprints the raw structure.
type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// textParts renders a message content value as its text form: a string is
// used as-is, an array keeps the text parts of each part. Non-text parts
// (image_url etc.) are skipped — they carry no reusable query semantics.
func textParts(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, part := range v {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if m["type"] != "text" {
				continue
			}
			if s, ok := m["text"].(string); ok {
				b.WriteString(s)
			}
		}
		return b.String()
	default:
		return ""
	}
}

var whitespace = regexp.MustCompile(`\s+`)

// normalizeQuery trims and collapses whitespace so semantically identical
// user turns map to the same cache key (same discipline as kernel sanitize).
func normalizeQuery(s string) string {
	return strings.TrimSpace(whitespace.ReplaceAllString(s, " "))
}

// lastUserText returns the normalized text of the last user message, which
// is the L2/L3 retrieval query (design §3.3 step 2).
func lastUserText(messages []chatMessage) (string, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		if q := normalizeQuery(textParts(messages[i].Content)); q != "" {
			return q, true
		}
	}
	return "", false
}

// contextHash fingerprints the full messages array (role + content) so the
// same query under a different conversation history never reuses another
// session's cached outcome (design §3.5: messages-context fingerprint).
func contextHash(messages []chatMessage) (string, error) {
	raw, err := json.Marshal(messages)
	if err != nil {
		return "", fmt.Errorf("gateway: fingerprint messages: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// parseChatRequest decodes the request body and runs the required structural
// checks (model present, non-empty messages, last user text available).
func parseChatRequest(body []byte) (*chatRequest, error) {
	var req chatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid request body: %w", err)
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("messages must not be empty")
	}
	if _, ok := lastUserText(req.Messages); !ok {
		return nil, fmt.Errorf("no user message with text content found")
	}
	return &req, nil
}
