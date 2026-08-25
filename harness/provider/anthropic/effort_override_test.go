package anthropic

import (
	"context"
	"encoding/json"
	"testing"

	"semantix/harness/provider"
)

func newTestClient(t *testing.T, baseURL, model string, extra map[string]any) *client {
	t.Helper()
	cfg := provider.Config{Name: "p", BaseURL: baseURL, Model: model, APIKey: "k", Extra: extra}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p.(*client)
}

// TestEffortOverrideNativeAdaptive pins the acceptance case: an in-vocabulary
// override wins on the native adaptive path, and an empty override keeps the
// constructed effort untouched.
func TestEffortOverrideNativeAdaptive(t *testing.T) {
	c := newTestClient(t, defaultBaseURL, "claude-opus-4-8", map[string]any{"thinking": "adaptive", "effort": "low"})
	if got := c.buildRequest(context.Background(), provider.Request{EffortOverride: "max"}).OutputConfig; got == nil || got.Effort != "max" {
		t.Fatalf("override max: output_config = %+v, want effort=max", got)
	}
	if got := c.buildRequest(context.Background(), provider.Request{EffortOverride: "low"}).OutputConfig; got == nil || got.Effort != "low" {
		t.Fatalf("override low: output_config = %+v, want effort=low", got)
	}
	if got := c.buildRequest(context.Background(), provider.Request{}).OutputConfig; got == nil || got.Effort != "low" {
		t.Fatalf("empty override must keep constructed effort low, got %+v", got)
	}
}

// TestEffortOverrideDeepSeekVocabulary pins the DeepSeek wire vocabulary
// (low|high|max): an out-of-vocabulary override is ignored and the constructed
// effort is used — the override is never forwarded raw.
func TestEffortOverrideDeepSeekVocabulary(t *testing.T) {
	c := newTestClient(t, "https://api.deepseek.com", "deepseek-v4", map[string]any{"effort": "high"})
	if got := c.buildRequest(context.Background(), provider.Request{EffortOverride: "medium"}).OutputConfig; got == nil || got.Effort != "high" {
		t.Fatalf("override medium (outside low|high|max) must keep constructed high, got %+v", got)
	}
	if got := c.buildRequest(context.Background(), provider.Request{EffortOverride: "max"}).OutputConfig; got == nil || got.Effort != "max" {
		t.Fatalf("override max: output_config = %+v, want effort=max", got)
	}
	if got := c.buildRequest(context.Background(), provider.Request{EffortOverride: "disabled"}).OutputConfig; got == nil || got.Effort != "high" {
		t.Fatalf("override disabled (a thinking toggle, not a depth) must keep constructed high, got %+v", got)
	}
}

// TestEffortOverrideEmptyIsByteIdentical pins the wire-compat contract: with an
// empty EffortOverride every request body must stay byte-identical to the
// pre-change goldens (captured from the unmodified buildRequest).
func TestEffortOverrideEmptyIsByteIdentical(t *testing.T) {
	req := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}}
	cases := []struct {
		name   string
		c      *client
		golden string
	}{
		{
			name:   "native adaptive low",
			c:      &client{model: "claude-opus-4-8", thinking: "adaptive", effort: "low"},
			golden: `{"model":"claude-opus-4-8","max_tokens":16384,"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}],"thinking":{"type":"adaptive","display":"summarized"},"output_config":{"effort":"low"},"stream":true}`,
		},
		{
			name:   "deepseek high",
			c:      &client{model: "deepseek-v4", deepseek: true, thinking: "enabled", effort: "high"},
			golden: `{"model":"deepseek-v4","max_tokens":16384,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"thinking":{"type":"enabled"},"output_config":{"effort":"high"},"stream":true}`,
		},
		{
			name:   "deepseek provider default",
			c:      &client{model: "deepseek-v4-flash", deepseek: true},
			golden: `{"model":"deepseek-v4-flash","max_tokens":16384,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"thinking":{"type":"enabled"},"stream":true}`,
		},
		{
			name:   "gateway enabled/disabled",
			c:      &client{model: "LongCat-2.0", thinking: "enabled", effort: "disabled"},
			golden: `{"model":"LongCat-2.0","max_tokens":16384,"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}],"thinking":{"type":"disabled"},"stream":true}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.c.buildRequest(context.Background(), req))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tc.golden {
				t.Fatalf("empty override changed the wire body:\ngot  %s\nwant %s", b, tc.golden)
			}
		})
	}
}

// TestEffortOverrideIgnoredWithoutVocabulary: binary thinking knobs have no
// depth scale, so an override must never invent wire fields.
func TestEffortOverrideIgnoredWithoutVocabulary(t *testing.T) {
	gateway := &client{model: "LongCat-2.0", thinking: "enabled", effort: "disabled"}
	r := gateway.buildRequest(context.Background(), provider.Request{EffortOverride: "max"})
	if r.OutputConfig != nil {
		t.Fatalf("enabled/disabled thinking must omit output_config with an override: %+v", r.OutputConfig)
	}
	deepseekDisabled := &client{model: "deepseek-v4", deepseek: true, thinking: "enabled", effort: "disabled"}
	r = deepseekDisabled.buildRequest(context.Background(), provider.Request{EffortOverride: "max"})
	if r.Thinking == nil || r.Thinking.Type != "disabled" || r.OutputConfig != nil {
		t.Fatalf("disabled DeepSeek thinking with override = %+v / %+v, want disabled/none", r.Thinking, r.OutputConfig)
	}
}

// TestEffortOverrideAutoBudget: the auto-derived max_tokens is re-resolved from
// the effective effort, while a configured (non-auto) budget is untouched.
func TestEffortOverrideAutoBudget(t *testing.T) {
	t.Run("deepseek auto", func(t *testing.T) {
		c := newTestClient(t, "https://api.deepseek.com", "deepseek-v4", map[string]any{"effort": "low"})
		if got := c.buildRequest(context.Background(), provider.Request{}).MaxTokens; got != provider.DefaultReasoningOutputTokens {
			t.Fatalf("default budget = %d, want %d", got, provider.DefaultReasoningOutputTokens)
		}
		if got := c.buildRequest(context.Background(), provider.Request{EffortOverride: "max"}).MaxTokens; got != provider.DefaultHighReasoningOutputTokens {
			t.Fatalf("override max budget = %d, want %d", got, provider.DefaultHighReasoningOutputTokens)
		}
	})
	t.Run("native adaptive auto", func(t *testing.T) {
		c := newTestClient(t, defaultBaseURL, "claude-opus-4-8", map[string]any{"thinking": "adaptive", "effort": "low"})
		if got := c.buildRequest(context.Background(), provider.Request{}).MaxTokens; got != provider.DefaultReasoningOutputTokens {
			t.Fatalf("default budget = %d, want %d", got, provider.DefaultReasoningOutputTokens)
		}
		if got := c.buildRequest(context.Background(), provider.Request{EffortOverride: "max"}).MaxTokens; got != provider.DefaultHighReasoningOutputTokens {
			t.Fatalf("override max budget = %d, want %d", got, provider.DefaultHighReasoningOutputTokens)
		}
	})
	t.Run("configured budget unaffected", func(t *testing.T) {
		c := newTestClient(t, "https://api.deepseek.com", "deepseek-v4", map[string]any{"effort": "low", "max_output_tokens": 8192})
		if got := c.buildRequest(context.Background(), provider.Request{EffortOverride: "max"}).MaxTokens; got != 8192 {
			t.Fatalf("configured budget with override = %d, want 8192", got)
		}
	})
}
