package gateway

import (
	"encoding/json"
	"log"
)

// Thinking-effort translation (GLM-P0-5, Issue #293).
//
// The two wire paths take DIFFERENT thinking parameters and do not accept
// each other's (measured on Tencent MaaS, glm-spike-week.md §2): the
// OpenAI-style path takes `reasoning_effort` ("low"|"medium"|"high"|"max"),
// while the Anthropic-style path takes `thinking:{type:"enabled",
// budget_tokens:N}` and returns HTTP 400 for `reasoning_effort`. The
// gateway's client surface is OpenAI-compatible, so requests bound for an
// anthropic-vendor upstream must translate — and after translation the
// outgoing body must be INCAPABLE of carrying reasoning_effort.
//
// effortBudgetTokens maps the effort ladder onto thinking budgets. The
// floor (1024) is the Anthropic-shape minimum budget; the ceiling mirrors
// the effort=max default of GLM-5.3 (thinking always-on, spec §3.2).
var effortBudgetTokens = map[string]int{
	"low":    1024,
	"medium": 4096,
	"high":   16384,
	"max":    32768,
}

// anthropicThinking is the thinking parameter of the Anthropic-style path.
type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

// translateThinking resolves the thinking parameter for an anthropic-vendor
// upstream from the client's OpenAI-shape fields.
//
//   - The client's explicit `thinking` object wins verbatim (it is already
//     in the target dialect; a client that sends both fields gets its
//     explicit object, not a translation).
//   - Otherwise a known `reasoning_effort` maps through effortBudgetTokens.
//   - An unknown effort fails OPEN: no thinking parameter at all, plus a
//     log warning — the endpoint's own default (GLM-5.3: thinking on,
//     effort max) applies, which degrades gracefully; failing closed would
//     turn a vocabulary drift into a hard 400 for every request.
func translateThinking(effort string, clientThinking json.RawMessage) (json.RawMessage, bool) {
	if len(clientThinking) > 0 && string(clientThinking) != "null" {
		return clientThinking, true
	}
	if effort == "" {
		return nil, true
	}
	budget, ok := effortBudgetTokens[effort]
	if !ok {
		log.Printf("gateway: unknown reasoning_effort %q — forwarding without a thinking parameter (endpoint default applies)", effort)
		return nil, false
	}
	raw, err := json.Marshal(anthropicThinking{Type: "enabled", BudgetTokens: budget})
	if err != nil {
		// Marshal of a static struct cannot fail in practice; keep the
		// fail-open contract anyway.
		log.Printf("gateway: thinking translation: %v — forwarding without a thinking parameter", err)
		return nil, false
	}
	return raw, true
}

// validateEffortForOpenAIPath warns (once per request) when the effort
// vocabulary is unknown — the OpenAI-style path forwards it verbatim, so a
// typo surfaces as an upstream 400 that is otherwise hard to attribute.
func validateEffortForOpenAIPath(effort string) {
	if effort == "" {
		return
	}
	if _, ok := effortBudgetTokens[effort]; !ok {
		log.Printf("gateway: reasoning_effort %q is outside the known ladder %v — forwarding verbatim (upstream may reject it)",
			effort, []string{"low", "medium", "high", "max"})
	}
}

// NOTE: the Anthropic path cannot leak reasoning_effort by construction —
// toAnthropicRequest re-marshals the body from anthropicRequest, which has
// no such field. TestAnthropicNeverForwardsReasoningEffort locks this in.
