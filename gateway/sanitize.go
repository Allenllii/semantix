package gateway

import (
	"sort"
	"strings"
)

// sanitizeOutgoing applies the prefix-hygiene middleware (GLM-P0-2, #290) to
// the decoded request body, in place. Order matters for byte stability:
// attribution stripping first (head of the system prefix), then tools
// canonicalization, then the per-upstream cache_control policy.
func (g *Gateway) sanitizeOutgoing(raw map[string]any, up UpstreamConfig) {
	if g.cfg.Sanitize.StripAttributionEnabled() {
		stripAttributionMarkers(raw, g.cfg.Sanitize.EffectiveAttributionMarkers())
	}
	if g.cfg.Sanitize.SortToolsEnabled() {
		sortToolsArray(raw)
	}
	if up.StripCacheControl {
		stripCacheControl(raw)
	}
}

// stripAttributionMarkers removes attribution/billing marker lines from the
// head of the FIRST system message only — that is where harnesses inject
// per-request billing headers, and it is the first byte of the provider's
// cache prefix. Marker lines are matched by prefix after trimming leading
// whitespace; stripping stops at the first non-marker, non-empty line so
// legitimate prompt text is never touched.
func stripAttributionMarkers(raw map[string]any, markers []string) {
	msgs, ok := raw["messages"].([]any)
	if !ok {
		return
	}
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok || msg["role"] != "system" {
			continue
		}
		switch content := msg["content"].(type) {
		case string:
			msg["content"] = stripMarkerHead(content, markers)
		case []any:
			// OpenAI content-parts form: strip inside the first text part.
			for _, p := range content {
				part, ok := p.(map[string]any)
				if !ok || part["type"] != "text" {
					continue
				}
				if text, ok := part["text"].(string); ok {
					part["text"] = stripMarkerHead(text, markers)
				}
				break
			}
		}
		return // first system message only
	}
}

// stripMarkerHead drops leading lines that begin with any marker prefix
// (plus blank lines directly following them). The remainder is returned
// unchanged, byte for byte.
func stripMarkerHead(text string, markers []string) string {
	rest := text
	for {
		line, tail, hasNL := strings.Cut(rest, "\n")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" && rest != text {
			// swallow blank separator lines after a stripped marker
			if hasNL {
				rest = tail
				continue
			}
			return ""
		}
		matched := false
		for _, m := range markers {
			if strings.HasPrefix(trimmed, m) {
				matched = true
				break
			}
		}
		if !matched {
			return rest
		}
		if !hasNL {
			return ""
		}
		rest = tail
	}
}

// sortToolsArray canonicalizes raw["tools"] by tool name using a stable sort,
// covering both the OpenAI shape ({type:"function",function:{name}}) and the
// Anthropic shape ({name}). Elements without a recognizable name keep their
// relative order at the end.
func sortToolsArray(raw map[string]any) {
	tools, ok := raw["tools"].([]any)
	if !ok || len(tools) < 2 {
		return
	}
	sort.SliceStable(tools, func(i, j int) bool {
		ni, oki := toolNameOf(tools[i])
		nj, okj := toolNameOf(tools[j])
		if oki != okj {
			return oki // named tools sort before unnamed
		}
		return ni < nj
	})
	raw["tools"] = tools
}

func toolNameOf(v any) (string, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", false
	}
	if fn, ok := m["function"].(map[string]any); ok {
		if name, ok := fn["name"].(string); ok && name != "" {
			return name, true
		}
	}
	if name, ok := m["name"].(string); ok && name != "" {
		return name, true
	}
	return "", false
}

// stripCacheControl removes every cache_control key nested anywhere in the
// body. Used only for upstreams explicitly configured with
// strip_cache_control = true; the default is pass-through (both measured
// GLM-hosting stacks tolerate the field, glm-spike-week.md §2/§3A.2).
func stripCacheControl(v any) {
	switch node := v.(type) {
	case map[string]any:
		delete(node, "cache_control")
		for _, child := range node {
			stripCacheControl(child)
		}
	case []any:
		for _, child := range node {
			stripCacheControl(child)
		}
	}
}
