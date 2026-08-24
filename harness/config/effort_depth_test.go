package config

import (
	"reflect"
	"testing"
)

// TestDepthEffortLevelsDropsThinkingToggles pins the gap that makes validating
// against EffortCapabilityForEntry alone a trap: that vocabulary legitimately
// advertises thinking on/off tokens, but a per-request override adjusts depth
// and nothing else, so the transport drops them. A setter that validated only
// against Levels would accept a level the wire then silently discards.
func TestDepthEffortLevelsDropsThinkingToggles(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry *ProviderEntry
		want  []string
	}{
		{
			// {"auto","adaptive","disabled"} — a binary thinking knob, no depth.
			name:  "minimax has no carryable depth",
			entry: &ProviderEntry{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1"},
			want:  nil,
		},
		{
			name:  "zhipu has no carryable depth",
			entry: &ProviderEntry{Kind: "openai", BaseURL: "https://open.bigmodel.cn/api/paas/v4"},
			want:  nil,
		},
		{
			name:  "longcat has no carryable depth",
			entry: &ProviderEntry{Kind: "openai", BaseURL: "https://api.longcat.chat/openai/v1"},
			want:  nil,
		},
		{
			// {"auto","none","low","medium","high","max"} — auto and none are
			// "omit the field", not depths.
			name:  "ollama cloud keeps only the depths",
			entry: &ProviderEntry{Kind: "openai", BaseURL: "https://ollama.com/v1"},
			want:  []string{"low", "medium", "high", "max"},
		},
		{
			name:  "anthropic keeps only the depths",
			entry: &ProviderEntry{Kind: "anthropic"},
			want:  []string{"low", "medium", "high", "xhigh", "max"},
		},
		{
			name:  "entry with no reasoning support has none",
			entry: &ProviderEntry{Kind: "openai", BaseURL: "https://example.com/v1"},
			want:  nil,
		},
		{
			name:  "nil entry has none",
			entry: nil,
			want:  nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DepthEffortLevels(tc.entry); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DepthEffortLevels = %v, want %v (capability levels were %v)",
					got, tc.want, EffortCapabilityForEntry(tc.entry).Levels)
			}
		})
	}
}

// TestDepthEffortLevelsIsASubsetOfTheCapability guards the wrapper itself: the
// depth list may only ever narrow the advertised vocabulary, never invent a
// level the entry does not claim to support.
func TestDepthEffortLevelsIsASubsetOfTheCapability(t *testing.T) {
	for _, entry := range []*ProviderEntry{
		{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1"},
		{Kind: "openai", BaseURL: "https://ollama.com/v1"},
		{Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro"},
		{Kind: "anthropic"},
	} {
		advertised := map[string]bool{}
		for _, level := range EffortCapabilityForEntry(entry).Levels {
			advertised[level] = true
		}
		for _, level := range DepthEffortLevels(entry) {
			if !advertised[level] {
				t.Errorf("%s/%s: depth level %q is not advertised in the capability",
					entry.Kind, entry.BaseURL, level)
			}
		}
	}
}
