package responses

import (
	"encoding/json"
	"testing"

	"semantix/harness/provider"
)

func newTestClient(t *testing.T, baseURL, model, effort string) *client {
	t.Helper()
	return New(Config{Name: "p", BaseURL: baseURL, Model: model, Effort: effort}).(*client)
}

func reasoningEffort(t *testing.T, c *client, req provider.Request) string {
	t.Helper()
	body, _, _ := c.buildRequestBody(req)
	reasoning, _ := body["reasoning"].(map[string]any)
	got, _ := reasoning["effort"].(string)
	return got
}

// TestEffortOverrideDeepSeekFlash pins the official DeepSeek vocabulary:
// flash admits low, an in-vocabulary override wins, out-of-vocabulary and
// thinking-toggle values fall back to the constructed effort.
func TestEffortOverrideDeepSeekFlash(t *testing.T) {
	c := newTestClient(t, "https://api.deepseek.com", "deepseek-v4-flash", "high")
	if got := reasoningEffort(t, c, provider.Request{EffortOverride: "low"}); got != "low" {
		t.Fatalf("override low: reasoning.effort = %q, want low", got)
	}
	if got := reasoningEffort(t, c, provider.Request{EffortOverride: "max"}); got != "max" {
		t.Fatalf("override max: reasoning.effort = %q, want max", got)
	}
	if got := reasoningEffort(t, c, provider.Request{EffortOverride: "medium"}); got != "high" {
		t.Fatalf("override outside DeepSeek vocabulary must keep the default, got %q", got)
	}
	if got := reasoningEffort(t, c, provider.Request{EffortOverride: "disabled"}); got != "high" {
		t.Fatalf("override must never toggle thinking off, got %q", got)
	}
	if got := reasoningEffort(t, c, provider.Request{}); got != "high" {
		t.Fatalf("empty override must keep the constructed effort, got %q", got)
	}
}

// TestEffortOverrideDeepSeekNonFlashRejectsLow: deepseek-v4 has no low level;
// an out-of-vocabulary override is ignored, never forwarded raw.
func TestEffortOverrideDeepSeekNonFlashRejectsLow(t *testing.T) {
	c := newTestClient(t, "https://api.deepseek.com", "deepseek-v4", "max")
	if got := reasoningEffort(t, c, provider.Request{EffortOverride: "low"}); got != "max" {
		t.Fatalf("low is flash-only; reasoning.effort = %q, want max", got)
	}
	if got := reasoningEffort(t, c, provider.Request{EffortOverride: "high"}); got != "high" {
		t.Fatalf("override high: reasoning.effort = %q, want high", got)
	}
}

// TestEffortOverrideOtherVendors: the OpenAI Responses scale is
// low|medium|high for non-DeepSeek endpoints.
func TestEffortOverrideOtherVendors(t *testing.T) {
	for _, vendor := range []struct{ baseURL, model string }{
		{"https://api.openai.com", "gpt-5"},
		{"https://dashscope.aliyuncs.com", "qwen3"},
		{"https://api.xiaomimimo.com/v1", "mimo-v2.5-pro"},
	} {
		c := newTestClient(t, vendor.baseURL, vendor.model, "low")
		if got := reasoningEffort(t, c, provider.Request{EffortOverride: "medium"}); got != "medium" {
			t.Errorf("%s: override medium: reasoning.effort = %q, want medium", vendor.baseURL, got)
		}
		if got := reasoningEffort(t, c, provider.Request{EffortOverride: "max"}); got != "low" {
			t.Errorf("%s: max is outside the OpenAI scale; reasoning.effort = %q, want low", vendor.baseURL, got)
		}
	}
}

// TestEffortOverrideIgnoredWhenThinkingDisabled: an endpoint configured with
// effort none/disabled has no depth scale — the override must not re-enable
// or deepen reasoning.
func TestEffortOverrideIgnoredWhenThinkingDisabled(t *testing.T) {
	for _, effort := range []string{"none", "disabled", "off"} {
		c := newTestClient(t, "https://api.deepseek.com", "deepseek-v4-flash", effort)
		if got := reasoningEffort(t, c, provider.Request{EffortOverride: "max"}); got != "none" {
			t.Fatalf("thinking-disabled %q with override max: reasoning.effort = %q, want none", effort, got)
		}
	}
}

// TestEffortOverrideEmptyIsByteIdentical pins the wire-compat contract: with an
// empty EffortOverride every request body stays byte-identical to the
// pre-change goldens (captured from the unmodified buildRequestBody).
func TestEffortOverrideEmptyIsByteIdentical(t *testing.T) {
	req := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}}
	cases := []struct {
		name   string
		c      *client
		golden string
	}{
		{
			name:   "deepseek flash high",
			c:      &client{vendor: "deepseek", model: "deepseek-v4-flash", effort: "high", caps: capabilitiesFor("deepseek")},
			golden: `{"input":[{"content":"hi","role":"user"}],"max_output_tokens":65536,"model":"deepseek-v4-flash","reasoning":{"effort":"high"},"stream":true}`,
		},
		{
			name:   "deepseek pro low",
			c:      &client{vendor: "deepseek", model: "deepseek-v4-pro", effort: "low", caps: capabilitiesFor("deepseek")},
			golden: `{"input":[{"content":"hi","role":"user"}],"max_output_tokens":32768,"model":"deepseek-v4-pro","reasoning":{"effort":"low"},"stream":true}`,
		},
		{
			name:   "generic medium",
			c:      &client{vendor: "generic", model: "gpt-5", effort: "medium", caps: capabilitiesFor("generic")},
			golden: `{"input":[{"content":"hi","role":"user"}],"model":"gpt-5","reasoning":{"effort":"medium"},"stream":true}`,
		},
		{
			name:   "thinking disabled",
			c:      &client{vendor: "deepseek", model: "deepseek-v4-flash", effort: "none", caps: capabilitiesFor("deepseek")},
			golden: `{"input":[{"content":"hi","role":"user"}],"max_output_tokens":16384,"model":"deepseek-v4-flash","reasoning":{"effort":"none"},"stream":true}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _, _ := tc.c.buildRequestBody(req)
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tc.golden {
				t.Fatalf("empty override changed the wire body:\ngot  %s\nwant %s", b, tc.golden)
			}
		})
	}
}

// TestEffortOverrideAutoBudget: the auto-derived max_output_tokens re-resolves
// from the effective effort, while a configured budget is untouched.
func TestEffortOverrideAutoBudget(t *testing.T) {
	t.Run("deepseek auto", func(t *testing.T) {
		c := newTestClient(t, "https://api.deepseek.com", "deepseek-v4-flash", "low")
		body, _, _ := c.buildRequestBody(provider.Request{})
		if got := body["max_output_tokens"]; got != provider.DefaultReasoningOutputTokens {
			t.Fatalf("default budget = %#v, want %d", got, provider.DefaultReasoningOutputTokens)
		}
		body, _, _ = c.buildRequestBody(provider.Request{EffortOverride: "max"})
		if got := body["max_output_tokens"]; got != provider.DefaultHighReasoningOutputTokens {
			t.Fatalf("override max budget = %#v, want %d", got, provider.DefaultHighReasoningOutputTokens)
		}
	})
	t.Run("configured budget unaffected", func(t *testing.T) {
		c := New(Config{Name: "p", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Effort: "low", MaxOutputTokens: 8192}).(*client)
		body, _, _ := c.buildRequestBody(provider.Request{EffortOverride: "max"})
		if got := body["max_output_tokens"]; got != 8192 {
			t.Fatalf("configured budget with override = %#v, want 8192", got)
		}
	})
}
