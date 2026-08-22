package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"semantix/kernel/inject"
)

func sanitizeGateway(t *testing.T, cfg SanitizeConfig) *Gateway {
	t.Helper()
	return &Gateway{cfg: &Config{Sanitize: cfg}}
}

func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return raw
}

const attributionBody = `{"model":"glm","messages":[{"role":"system","content":"x-anthropic-billing-header: cch=abc123-per-request\n\nYou are a coding agent."},{"role":"user","content":"hi"}]}`

// TestSanitizeStripsAttributionByDefault: the per-request billing marker at
// the head of the system prompt is removed with the default config, and the
// remaining prompt is byte-identical to the original tail.
func TestSanitizeStripsAttributionByDefault(t *testing.T) {
	g := sanitizeGateway(t, SanitizeConfig{})
	up := UpstreamConfig{UpstreamModel: "glm-5.3"}

	out := g.rewriteOutgoing([]byte(attributionBody), nil, up, nil)
	raw := decodeBody(t, out)
	msgs := raw["messages"].([]any)
	sys := msgs[0].(map[string]any)["content"].(string)
	if strings.Contains(sys, "x-anthropic-billing-header") {
		t.Fatalf("attribution marker survived: %q", sys)
	}
	if sys != "You are a coding agent." {
		t.Fatalf("prompt tail mutated: %q", sys)
	}
}

// TestSanitizeAttributionOffPreservesBytes: strip_attribution=false forwards
// the system prompt untouched.
func TestSanitizeAttributionOffPreservesBytes(t *testing.T) {
	off := false
	g := sanitizeGateway(t, SanitizeConfig{StripAttribution: &off})
	up := UpstreamConfig{UpstreamModel: "glm-5.3"}

	out := g.rewriteOutgoing([]byte(attributionBody), nil, up, nil)
	raw := decodeBody(t, out)
	sys := raw["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.HasPrefix(sys, "x-anthropic-billing-header") {
		t.Fatalf("marker was stripped with sanitizer off: %q", sys)
	}
}

// TestSanitizeStripsMarkerInContentParts covers the OpenAI content-parts
// system form.
func TestSanitizeStripsMarkerInContentParts(t *testing.T) {
	body := `{"model":"glm","messages":[{"role":"system","content":[{"type":"text","text":"x-anthropic-billing-header: cch=zzz\nReal prompt."},{"type":"text","text":"second part"}]},{"role":"user","content":"hi"}]}`
	g := sanitizeGateway(t, SanitizeConfig{})
	out := g.rewriteOutgoing([]byte(body), nil, UpstreamConfig{UpstreamModel: "m"}, nil)
	raw := decodeBody(t, out)
	parts := raw["messages"].([]any)[0].(map[string]any)["content"].([]any)
	first := parts[0].(map[string]any)["text"].(string)
	if first != "Real prompt." {
		t.Fatalf("first part = %q", first)
	}
	if second := parts[1].(map[string]any)["text"].(string); second != "second part" {
		t.Fatalf("second part touched: %q", second)
	}
}

// TestSanitizeCustomMarkerAndMultiline: configured markers merge with
// built-ins; consecutive marker lines and their blank separator all strip.
func TestSanitizeCustomMarkerAndMultiline(t *testing.T) {
	body := `{"model":"m","messages":[{"role":"system","content":"x-custom-billing: a=1\nx-anthropic-billing-header: cch=2\n\nPrompt."}]}`
	g := sanitizeGateway(t, SanitizeConfig{AttributionMarkers: []string{"x-custom-billing"}})
	out := g.rewriteOutgoing([]byte(body), nil, UpstreamConfig{UpstreamModel: "m"}, nil)
	sys := decodeBody(t, out)["messages"].([]any)[0].(map[string]any)["content"].(string)
	if sys != "Prompt." {
		t.Fatalf("multi-marker head not fully stripped: %q", sys)
	}
}

// TestSanitizeSortsToolsBothShapes: OpenAI and Anthropic tool shapes sort by
// name; serialization becomes registration-order independent.
func TestSanitizeSortsToolsBothShapes(t *testing.T) {
	openaiBody := `{"model":"m","messages":[],"tools":[{"type":"function","function":{"name":"zeta"}},{"type":"function","function":{"name":"alpha"}}]}`
	anthropicBody := `{"model":"m","messages":[],"tools":[{"name":"zeta"},{"name":"alpha"}]}`
	g := sanitizeGateway(t, SanitizeConfig{})
	for name, body := range map[string]string{"openai": openaiBody, "anthropic": anthropicBody} {
		out := g.rewriteOutgoing([]byte(body), nil, UpstreamConfig{UpstreamModel: "m"}, nil)
		tools := decodeBody(t, out)["tools"].([]any)
		n0, _ := toolNameOf(tools[0])
		n1, _ := toolNameOf(tools[1])
		if n0 != "alpha" || n1 != "zeta" {
			t.Fatalf("%s tools not sorted: %v, %v", name, n0, n1)
		}
	}
}

// TestSanitizeToolsOrderByteStable: two requests with the same tool set in
// different client order serialize to byte-identical tools arrays.
func TestSanitizeToolsOrderByteStable(t *testing.T) {
	a := `{"model":"m","messages":[],"tools":[{"type":"function","function":{"name":"b"}},{"type":"function","function":{"name":"a"}}]}`
	b := `{"model":"m","messages":[],"tools":[{"type":"function","function":{"name":"a"}},{"type":"function","function":{"name":"b"}}]}`
	g := sanitizeGateway(t, SanitizeConfig{})
	up := UpstreamConfig{UpstreamModel: "m"}
	outA := g.rewriteOutgoing([]byte(a), nil, up, nil)
	outB := g.rewriteOutgoing([]byte(b), nil, up, nil)
	toolsA, _ := json.Marshal(decodeBody(t, outA)["tools"])
	toolsB, _ := json.Marshal(decodeBody(t, outB)["tools"])
	if string(toolsA) != string(toolsB) {
		t.Fatalf("tools serialization order-dependent:\n a=%s\n b=%s", toolsA, toolsB)
	}
}

// TestSanitizeCacheControlPerUpstream: default passes cache_control through;
// strip_cache_control=true removes it at every depth.
func TestSanitizeCacheControlPerUpstream(t *testing.T) {
	body := `{"model":"m","cache_control":{"type":"ephemeral"},"messages":[{"role":"system","content":[{"type":"text","text":"p","cache_control":{"type":"ephemeral"}}]}]}`
	g := sanitizeGateway(t, SanitizeConfig{})

	kept := decodeBody(t, g.rewriteOutgoing([]byte(body), nil, UpstreamConfig{UpstreamModel: "m"}, nil))
	if _, ok := kept["cache_control"]; !ok {
		t.Fatal("default should pass cache_control through")
	}

	stripped := decodeBody(t, g.rewriteOutgoing([]byte(body), nil, UpstreamConfig{UpstreamModel: "m", StripCacheControl: true}, nil))
	if _, ok := stripped["cache_control"]; ok {
		t.Fatal("top-level cache_control survived strip")
	}
	part := stripped["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if _, ok := part["cache_control"]; ok {
		t.Fatal("nested cache_control survived strip")
	}
}

// TestSanitizeSurvivesInjectionPath: the L2 injection path must build on the
// sanitized messages (regression for the raw-vs-req.Messages fork).
func TestSanitizeSurvivesInjectionPath(t *testing.T) {
	g := sanitizeGateway(t, SanitizeConfig{})
	out := g.rewriteOutgoing([]byte(attributionBody), nil, UpstreamConfig{UpstreamModel: "m"},
		&inject.Injection{Text: "[semantix-reuse]\nX\n[/semantix-reuse]"})
	raw := decodeBody(t, out)
	sys := raw["messages"].([]any)[0].(map[string]any)["content"].(string)
	if strings.Contains(sys, "x-anthropic-billing-header") {
		t.Fatalf("injection path lost sanitation: %q", sys)
	}
	if !strings.Contains(sys, "[semantix-reuse]") {
		t.Fatalf("injection block missing: %q", sys)
	}
}
