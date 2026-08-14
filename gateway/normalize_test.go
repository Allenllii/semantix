package gateway

import (
	"strings"
	"testing"
)

func msg(role, content string) chatMessage {
	return chatMessage{Role: role, Content: content}
}

func TestLastUserText(t *testing.T) {
	cases := []struct {
		name     string
		messages []chatMessage
		want     string
		ok       bool
	}{
		{"plain", []chatMessage{msg("user", "  hello   world  ")}, "hello world", true},
		{"last user wins", []chatMessage{
			msg("user", "first"),
			msg("assistant", "reply"),
			msg("user", "second"),
		}, "second", true},
		{"empty user skipped", []chatMessage{
			msg("user", "   "),
			msg("assistant", "r"),
			msg("user", "final"),
		}, "final", true},
		{"no user", []chatMessage{msg("system", "s"), msg("assistant", "a")}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := lastUserText(tc.messages)
			if got != tc.want || ok != tc.ok {
				t.Errorf("lastUserText = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestLastUserTextMultimodal(t *testing.T) {
	m := chatMessage{Role: "user", Content: []any{
		map[string]any{"type": "text", "text": "describe"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "http://x"}},
		map[string]any{"type": "text", "text": "  the chart "},
	}}
	got, ok := lastUserText([]chatMessage{m})
	if !ok || got != "describe the chart" {
		t.Errorf("multimodal = (%q, %v), want (\"describe the chart\", true)", got, ok)
	}
}

func TestContextHash(t *testing.T) {
	a := []chatMessage{msg("system", "sys"), msg("user", "hi")}
	b := []chatMessage{msg("system", "sys"), msg("user", "hi")}
	c := []chatMessage{msg("system", "different"), msg("user", "hi")}
	ha, _ := contextHash(a)
	hb, _ := contextHash(b)
	hc, _ := contextHash(c)
	if ha != hb {
		t.Error("identical contexts must hash identically")
	}
	if ha == hc {
		t.Error("different contexts must hash differently")
	}
}

func TestParseChatRequestErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"bad json", `{`},
		{"no model", `{"messages":[{"role":"user","content":"x"}]}`},
		{"no messages", `{"model":"m"}`},
		{"no user text", `{"model":"m","messages":[{"role":"assistant","content":"x"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseChatRequest([]byte(tc.body)); err == nil {
				t.Errorf("parseChatRequest(%s) must fail", tc.body)
			}
		})
	}
}

func TestParseChatRequestOK(t *testing.T) {
	req, err := parseChatRequest([]byte(`{"model":"ds","messages":[{"role":"system","content":"s"},{"role":"user","content":"hello"}],"temperature":0.5}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "ds" || len(req.Messages) != 2 || req.Temperature == nil || *req.Temperature != 0.5 {
		t.Errorf("req = %#v", req)
	}
}

func TestAttachBlock(t *testing.T) {
	// appended to the first system message
	in := []chatMessage{msg("system", "sys1"), msg("user", "u")}
	out := attachBlock(in, "[semantix-reuse]\nblock\n[/semantix-reuse]")
	if s, ok := out[0].Content.(string); !ok || !strings.Contains(s, "[semantix-reuse]") {
		t.Errorf("block not appended to system: %#v", out[0])
	}
	// prepended when no system message exists
	in = []chatMessage{msg("user", "u")}
	out = attachBlock(in, "[semantix-reuse]\nblock\n[/semantix-reuse]")
	if len(out) != 2 || out[0].Role != "system" || out[0].Content != "[semantix-reuse]\nblock\n[/semantix-reuse]" {
		t.Errorf("system not prepended: %#v", out)
	}
	// multimodal system message untouched, block prepended instead
	in = []chatMessage{{Role: "system", Content: []any{}}, msg("user", "u")}
	out = attachBlock(in, "block")
	if out[0].Role != "system" || out[0].Content != "block" {
		t.Errorf("multimodal system handling wrong: %#v", out)
	}
}

func TestExtractAssistantContent(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"answer text"}}]}`)
	if got := extractAssistantContent(body); got != "answer text" {
		t.Errorf("extract = %q", got)
	}
	if got := extractAssistantContent([]byte(`{broken`)); got != "" {
		t.Errorf("broken body must yield empty, got %q", got)
	}
}

func TestTurns(t *testing.T) {
	req := &chatRequest{Messages: []chatMessage{msg("user", "q"), msg("assistant", "a")}}
	turns := turns(req, "answer")
	if len(turns) != 3 || turns[2]["role"] != "assistant" || turns[2]["content"] != "answer" {
		t.Errorf("turns = %#v", turns)
	}
}
