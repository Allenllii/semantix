package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"semantix/kernel/sanitize"
)

// LLMConfig wires the model judge backend (Issue #8 stage ②). The protocol
// is user-selected — "openai" (OpenAI-compatible chat completions) or
// "anthropic" (Messages API) — pointing at the user's own endpoint and model;
// the API key is read from the environment (SEMANTIX_JUDGE_API_KEY), never
// stored in config or code.
type LLMConfig struct {
	Protocol string // "openai" | "anthropic"
	BaseURL  string // e.g. https://api.openai.com/v1 or https://api.anthropic.com/v1
	Model    string
	APIKey   string
	Timeout  time.Duration // 0 -> 30s
}

// LLMJudge implements Judge by asking an LLM the binary question
// J(q, s, a): is reusing slice s's answer for query q acceptable?
type LLMJudge struct {
	cfg LLMConfig
	hc  *http.Client
}

// NewLLMJudge validates the config and builds the judge.
func NewLLMJudge(cfg LLMConfig) (*LLMJudge, error) {
	switch cfg.Protocol {
	case "openai", "anthropic":
	default:
		return nil, fmt.Errorf("judge: unsupported protocol %q (want openai|anthropic)", cfg.Protocol)
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		return nil, fmt.Errorf("judge: base-url and model are required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("judge: API key is required (set SEMANTIX_JUDGE_API_KEY)")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &LLMJudge{cfg: cfg, hc: &http.Client{Timeout: cfg.Timeout}}, nil
}

// rubricPrompt builds the code-scenario verification prompt from the
// six-dimension checklist (docs/reports/verify-rubric.md).
func rubricPrompt(c Candidate) string {
	return fmt.Sprintf(`You are a strict reuse judge for a coding agent's semantic cache.
Decide whether the cached answer below is acceptable for the new request.

CHECKLIST (reject if any applies):
- API signature: cached answer references functions/methods whose signatures changed
- File paths: referenced paths no longer exist in the current worktree
- Dependency versions: go.mod/package.json versions drifted for referenced deps
- Language/framework mismatch: answer's stack differs from the request's context
- Freshness: build artifacts or manifests changed (if mentioned)
- Personalization: project config / user preference conflicts

NEW REQUEST:
%s

CACHED ANSWER (slice %s):
%s

Reply with exactly one word: yes (reuse acceptable) or no (do not reuse).`,
		c.Query, c.SliceID, c.Content)
}

// secondaryRubricPrompt is the rephrased second perspective of the same
// six-dimension checklist (Issue #280 consensus gate): same output
// contract (one token: yes/no), deliberately different wording and
// example order so a wording-sensitive single judgement is caught by the
// second pass.
func secondaryRubricPrompt(c Candidate) string {
	return fmt.Sprintf(`You verify cached answers before reuse in a coding assistant.
Is this cached response still correct for the current request?

Reject it when ANY of these holds:
- the answer mentions code whose interface (function/method signature) is outdated
- files or paths it relies on are gone from the working tree
- dependency manifests (go.mod, package.json, ...) show drifted versions
- the answer assumes a language or framework stack the request does not use
- build outputs or configs it depends on were regenerated
- it conflicts with the project's settings or the user's stated preferences

REQUEST:
%s

CACHED RESPONSE (slice %s):
%s

Answer with a single word only: yes or no.`,
		c.Query, c.SliceID, c.Content)
}

// Confirm implements Judge.
func (j *LLMJudge) Confirm(ctx context.Context, c Candidate) (bool, error) {
	// Issue #8 acceptance ④ + Issue #278: the judge reads the FULL
	// sanitization pipeline (escape stripping + injection-feature removal +
	// sensitive redaction, kernel/sanitize) — never raw history. A cached
	// slice may carry prompt-injection payloads (OSC52 clipboard writes,
	// forged instructions, keys); deterministic sanitization before the
	// prompt keeps the judge trustworthy (Security §3.2.1 reuses §3.1).
	c.Content = sanitize.Sanitize(c.Content)
	text, err := j.call(ctx, rubricPrompt(c))
	if err != nil {
		return false, err
	}
	t := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(t, "yes"), nil
}

// ConfirmSecondary is the rephrased-rubric second perspective (Issue #280
// consensus gate): the same candidate through secondaryRubricPrompt with
// the same single-token yes/no contract. Confirm (primary) and
// ConfirmSecondary (secondary) must both approve for the consensus gate.
func (j *LLMJudge) ConfirmSecondary(ctx context.Context, c Candidate) (bool, error) {
	c.Content = sanitize.Sanitize(c.Content) // same read-side pipeline
	text, err := j.call(ctx, secondaryRubricPrompt(c))
	if err != nil {
		return false, err
	}
	t := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(t, "yes"), nil
}

// call performs the protocol-specific request and returns the assistant text.
func (j *LLMJudge) call(ctx context.Context, prompt string) (string, error) {
	switch j.cfg.Protocol {
	case "openai":
		return j.callOpenAI(ctx, prompt)
	case "anthropic":
		return j.callAnthropic(ctx, prompt)
	default:
		return "", fmt.Errorf("judge: unsupported protocol %q", j.cfg.Protocol)
	}
}

func (j *LLMJudge) do(ctx context.Context, path string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("judge: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.cfg.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("judge: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if j.cfg.Protocol == "anthropic" {
		req.Header.Set("x-api-key", j.cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+j.cfg.APIKey)
	}
	resp, err := j.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("judge: call: %w", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("judge: read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Status only — never echo the response body: a user-configured
		// endpoint/proxy could reflect the Authorization header into it.
		return nil, fmt.Errorf("judge: %s: HTTP %d (use https endpoints)", j.cfg.BaseURL+path, resp.StatusCode)
	}
	return out, nil
}

func (j *LLMJudge) callOpenAI(ctx context.Context, prompt string) (string, error) {
	body := map[string]any{
		"model":       j.cfg.Model,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a strict semantic-cache reuse judge."},
			{"role": "user", "content": prompt},
		},
	}
	out, err := j.do(ctx, "/chat/completions", body)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return "", fmt.Errorf("judge: openai response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("judge: openai response: no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

func (j *LLMJudge) callAnthropic(ctx context.Context, prompt string) (string, error) {
	body := map[string]any{
		"model":      j.cfg.Model,
		"max_tokens": 16,
		"system":     "You are a strict semantic-cache reuse judge. Reply with exactly one word: yes or no.",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	out, err := j.do(ctx, "/messages", body)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return "", fmt.Errorf("judge: anthropic response: %w", err)
	}
	for _, c := range parsed.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}
	return "", fmt.Errorf("judge: anthropic response: no text block")
}
