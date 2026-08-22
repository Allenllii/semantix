package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"semantix/harness/billing"
)

// This file implements the customer free-tier quota: every distributed
// gateway serves the first [billing] free_tokens tokens (default 10M) for
// free, then answers 402 with the platform recharge URL until the customer's
// platform wallet reports an active balance. Enforcement lives here — on the
// server side of the customer's install — so patching or replacing the CLI
// client cannot extend the free tier.
//
// Token accounting rides the existing usage events (recordUsage): only
// requests that actually reached an upstream count against the tier; L3
// verified-reuse hits cost the platform nothing and are deliberately free,
// which also keeps the "cache saves you money" story honest.

// quotaMode labels the admission decision for headers and /v1/quota.
const (
	quotaModeFree    = "free"    // free tier still has tokens
	quotaModePaid    = "paid"    // tier exhausted, platform wallet active
	quotaModeBlocked = "blocked" // tier exhausted, no active wallet
)

// quotaStateVersion is the persisted counter schema.
const quotaStateVersion = 1

// balanceErrorBackoff bounds how long a *failed* wallet probe suppresses the
// next probe. A successful verdict is cached for balance_cache_seconds, but an
// error must be retried quickly (not held for the full success TTL): otherwise
// a flaky wallet endpoint — especially right after a restart, when no prior
// verdict exists to fall back on — would 402 a paying customer for the whole
// TTL. Short backoff keeps stale-while-error self-healing in seconds.
const balanceErrorBackoff = 5 * time.Second

// quotaState is the durable token counter. It survives gateway restarts via
// an atomic tmp+rename write next to the slice store.
type quotaState struct {
	Version    int   `json:"version"`
	UsedTokens int64 `json:"used_tokens"`
	UpdatedAt  int64 `json:"updated_at"`
}

// quotaStatus is one consistent snapshot of the tier, served on /v1/quota
// and echoed as x-semantix-quota-* headers.
type quotaStatus struct {
	FreeTokens      int64  `json:"free_tokens"`
	UsedTokens      int64  `json:"used_tokens"`
	RemainingTokens int64  `json:"remaining_tokens"`
	Exhausted       bool   `json:"exhausted"`
	Mode            string `json:"mode"`
	RechargeURL     string `json:"recharge_url,omitempty"`
}

// quotaEngine meters tokens against the free tier and, once the tier is
// exhausted, probes the platform balance endpoint (DeepSeek GET /user/balance
// schema, shared with the harness status line via harness/billing) to unlock
// paid mode automatically after the customer tops up.
type quotaEngine struct {
	cfg    BillingConfig
	path   string       // persisted counter location
	client *http.Client // balance probe transport (injectable in tests)
	now    func() time.Time

	mu   sync.Mutex
	used int64
	// Balance probe cache: FetchWithClient is a remote call, so its verdict
	// is reused for balance_cache_seconds. On probe error the last verdict
	// is kept (stale-while-error) — a flaky wallet endpoint must not lock
	// out a paying customer, and the free-tier gate itself never depends on
	// the probe.
	paidOK      bool
	paidChecked time.Time // last *successful* probe; cached for balance_cache_seconds
	probeErrAt  time.Time // last *failed* probe; suppresses re-probe for balanceErrorBackoff only
}

// newQuotaEngine loads (or initializes) the persisted counter. A corrupt or
// unreadable state file fails startup: silently resetting the counter would
// re-grant the free tier.
func newQuotaEngine(cfg BillingConfig, path string, client *http.Client, now func() time.Time) (*quotaEngine, error) {
	if now == nil {
		now = time.Now
	}
	q := &quotaEngine{cfg: cfg, path: path, client: client, now: now}
	raw, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		// first run: zero usage
	case err != nil:
		return nil, fmt.Errorf("quota state %s: %w", path, err)
	default:
		var st quotaState
		if jerr := json.Unmarshal(raw, &st); jerr != nil {
			return nil, fmt.Errorf("quota state %s: %w", path, jerr)
		}
		if st.UsedTokens < 0 {
			return nil, fmt.Errorf("quota state %s: negative used_tokens", path)
		}
		q.used = st.UsedTokens
	}
	return q, nil
}

// Consume adds n tokens to the meter and persists the counter. Persist
// errors are returned for logging but never roll back the in-memory count —
// under-counting is the failure mode we must avoid.
func (q *quotaEngine) Consume(n int64) error {
	if n <= 0 {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.used += n
	return q.persistLocked()
}

// persistLocked writes the counter atomically (tmp+rename). Callers hold mu.
func (q *quotaEngine) persistLocked() error {
	st := quotaState{Version: quotaStateVersion, UsedTokens: q.used, UpdatedAt: q.now().Unix()}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, q.path)
}

// Status computes the current admission verdict. Inside the free tier it
// never touches the network; once exhausted it consults the (cached)
// platform balance probe to distinguish paid from blocked.
func (q *quotaEngine) Status(ctx context.Context) quotaStatus {
	q.mu.Lock()
	used := q.used
	q.mu.Unlock()

	st := quotaStatus{
		FreeTokens:  q.cfg.FreeTokens,
		UsedTokens:  used,
		RechargeURL: q.cfg.RechargeURL,
	}
	st.RemainingTokens = st.FreeTokens - used
	if st.RemainingTokens > 0 {
		st.Mode = quotaModeFree
		return st
	}
	st.RemainingTokens = 0
	st.Exhausted = true
	if q.balanceActive(ctx) {
		st.Mode = quotaModePaid
	} else {
		st.Mode = quotaModeBlocked
	}
	return st
}

// balanceActive reports whether the customer's platform wallet can serve
// API calls (is_available in the DeepSeek balance schema). Without a
// configured balance_url the tier can only be extended by config, so the
// answer is always false.
func (q *quotaEngine) balanceActive(ctx context.Context) bool {
	if q.cfg.BalanceURL == "" {
		return false
	}
	q.mu.Lock()
	ttl := time.Duration(q.cfg.BalanceCacheSeconds) * time.Second
	// Reuse a cached *successful* verdict for the full TTL.
	if !q.paidChecked.IsZero() && q.now().Sub(q.paidChecked) < ttl {
		ok := q.paidOK
		q.mu.Unlock()
		return ok
	}
	// After a *failed* probe, suppress re-probing only for the short backoff —
	// never the full success TTL — so a transient wallet error self-heals fast
	// and cannot lock out a paid customer (Issue B).
	if !q.probeErrAt.IsZero() && q.now().Sub(q.probeErrAt) < balanceErrorBackoff {
		ok := q.paidOK
		q.mu.Unlock()
		return ok
	}
	q.mu.Unlock()

	pctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	b, err := billing.FetchWithClient(pctx, q.client, q.cfg.BalanceURL, q.cfg.BalanceKey)

	q.mu.Lock()
	defer q.mu.Unlock()
	if err != nil {
		// stale-while-error: keep the previous verdict and retry after a short
		// backoff (not the full TTL). Logged so a misconfigured/expired
		// balance_key that would silently 402 paid customers is diagnosable.
		q.probeErrAt = q.now()
		log.Printf("gateway: wallet balance probe failed (%v); keeping previous paid=%v, retrying in %s", err, q.paidOK, balanceErrorBackoff)
		return q.paidOK
	}
	q.paidOK = b != nil && b.Available
	q.paidChecked = q.now()
	q.probeErrAt = time.Time{} // clear the error backoff on any successful probe
	return q.paidOK
}

// setQuotaHeaders exposes the tier state on every metered response so any
// client can render progress ("8.2M / 10M free tokens used") without an
// extra round trip.
func setQuotaHeaders(h http.Header, st quotaStatus) {
	h.Set("x-semantix-quota-limit", fmt.Sprintf("%d", st.FreeTokens))
	h.Set("x-semantix-quota-used", fmt.Sprintf("%d", st.UsedTokens))
	h.Set("x-semantix-quota-remaining", fmt.Sprintf("%d", st.RemainingTokens))
	h.Set("x-semantix-quota-mode", st.Mode)
	if st.Exhausted && st.RechargeURL != "" {
		h.Set("x-semantix-quota-recharge-url", st.RechargeURL)
	}
}

// admitQuota gates one chat request. Inside the free tier (or with an active
// paid wallet) it stamps advisory headers and admits. Blocked requests get
// the OpenAI insufficient_quota error with the recharge URL — the harness
// surfaces that message verbatim, which is the whole client-side flow.
func (g *Gateway) admitQuota(w http.ResponseWriter, r *http.Request) bool {
	if g.quota == nil {
		return true
	}
	st := g.quota.Status(r.Context())
	setQuotaHeaders(w.Header(), st)
	if st.Mode != quotaModeBlocked {
		return true
	}
	msg := fmt.Sprintf(
		"free tier exhausted: %d of %d free tokens used. Top up at %s to continue — access unlocks automatically once your platform balance is active. "+
			"免费额度已用完（已使用 %d / %d tokens）。请前往 %s 充值，到账后自动恢复使用。",
		st.UsedTokens, st.FreeTokens, st.RechargeURL,
		st.UsedTokens, st.FreeTokens, st.RechargeURL)
	writeAPIErrorCode(w, http.StatusPaymentRequired, "insufficient_quota", "free_tier_exhausted", msg)
	return false
}

// handleQuota serves GET /v1/quota: the JSON snapshot the CLI (or any
// dashboard) polls to show remaining free tokens and the recharge link.
func (g *Gateway) handleQuota(w http.ResponseWriter, r *http.Request) {
	if g.quota == nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "billing is not enabled on this gateway")
		return
	}
	st := g.quota.Status(r.Context())
	setQuotaHeaders(w.Header(), st)
	raw, err := json.Marshal(struct {
		Object string `json:"object"`
		quotaStatus
	}{Object: "quota", quotaStatus: st})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}
