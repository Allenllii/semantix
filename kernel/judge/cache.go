package judge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// The LLM judge sits on the gateway's synchronous L3 path: every grey-zone
// candidate otherwise pays a full model round-trip before TTFT, and a
// timed-out judge (30s kernel default) stalls the request with nothing to
// show for it. Two mechanisms fix the economics without weakening the
// fail-closed contract:
//
//   - VerdictCache: identical (query, slice) pairs get identical verdicts,
//     so approved/declined outcomes are memoized and repeat occurrences are
//     instant.
//   - Warm: a judge call that errored (timeout, transport) is retried once
//     in the background; the in-flight request still fails closed, but the
//     NEXT occurrence of the same candidate hits a warmed verdict instead
//     of timing out again.
//
// Errors are never cached as verdicts — a transient judge outage must not
// permanently reject a reusable candidate.

// verdictTTL bounds a cached verdict. Judge verdicts encode model behavior
// and sanitizer state; a day keeps them fresher than the L3 TTL windows
// (DeepSeek 24h / Anthropic 5m vendor windows).
const verdictTTL = 24 * time.Hour

// cacheCap bounds memory: 4096 verdicts at ~100B key + small payload is
// well under a megabyte; eviction is oldest-expiry.
const cacheCap = 4096

type verdictEntry struct {
	confirm bool
	expires time.Time
}

// VerdictCache memoizes judge outcomes keyed by (query, slice).
type VerdictCache struct {
	mu      sync.Mutex
	entries map[string]verdictEntry
}

// NewVerdictCache builds an empty cache.
func NewVerdictCache() *VerdictCache {
	return &VerdictCache{entries: map[string]verdictEntry{}}
}

// Key derives the cache key from the query and slice identity. Content is
// deliberately excluded: the slice ID already pins content via the store's
// content-hash identity, and the query is the remaining variable.
func VerdictKey(query, sliceID string) string {
	h := sha256.Sum256([]byte(query + "\x00" + sliceID))
	return hex.EncodeToString(h[:16])
}

// Get returns the cached verdict, or false on miss/expiry.
func (c *VerdictCache) Get(key string) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return false, false
	}
	if time.Now().After(e.expires) {
		delete(c.entries, key)
		return false, false
	}
	return e.confirm, true
}

// Set stores a verdict, evicting expired entries when over capacity.
func (c *VerdictCache) Set(key string, confirm bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= cacheCap {
		now := time.Now()
		for k, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, k)
			}
		}
	}
	if len(c.entries) >= cacheCap {
		return // all-fresh overflow: drop the new verdict, keep the warm set
	}
	c.entries[key] = verdictEntry{confirm: confirm, expires: time.Now().Add(verdictTTL)}
}

// Len reports the number of cached verdicts.
func (c *VerdictCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// CachedJudge decorates any Judge with the verdict cache and optional
// background warming. It implements Judge and VariantJudge (Issue #280):
// the secondary consensus perspective lives in its own cache namespace and
// delegates to the inner judge's ConfirmSecondary when available, so a
// cached primary verdict can never stand in for an independent
// second-rubric judgement. It composes with RuleGate.Chain and the
// L3Decider unchanged.
type CachedJudge struct {
	Inner Judge
	Cache *VerdictCache
	// Warm retries errored judge calls in the background so repeat
	// candidates stop paying the timeout (see package comment).
	Warm bool

	inflightMu sync.Mutex
	inflight   map[string]bool
}

// Confirm implements Judge (primary rubric, cache namespace "p").
func (c *CachedJudge) Confirm(ctx context.Context, cand Candidate) (bool, error) {
	return c.confirm(ctx, cand, "p", false)
}

// ConfirmSecondary implements VariantJudge (rephrased rubric, cache
// namespace "s"). Without an inner VariantJudge it falls back to the plain
// Confirm — mirroring the consensus baseline degradation.
func (c *CachedJudge) ConfirmSecondary(ctx context.Context, cand Candidate) (bool, error) {
	return c.confirm(ctx, cand, "s", true)
}

func (c *CachedJudge) confirm(ctx context.Context, cand Candidate, ns string, secondary bool) (bool, error) {
	if c.Cache == nil {
		c.Cache = NewVerdictCache()
	}
	key := ns + VerdictKey(cand.Query, cand.SliceID)
	if confirm, ok := c.Cache.Get(key); ok {
		return confirm, nil
	}
	var confirm bool
	var err error
	if secondary {
		if vj, ok := c.Inner.(VariantJudge); ok {
			confirm, err = vj.ConfirmSecondary(ctx, cand)
		} else {
			confirm, err = c.Inner.Confirm(ctx, cand)
		}
	} else {
		confirm, err = c.Inner.Confirm(ctx, cand)
	}
	if err != nil {
		if c.Warm {
			c.warmInBackground(key, cand, ns, secondary)
		}
		return false, err // fail closed, nothing cached
	}
	c.Cache.Set(key, confirm)
	return confirm, nil
}

// warmInBackground re-runs the judge once per candidate, off the request
// path. A repeat candidate already being warmed is not re-spawned.
func (c *CachedJudge) warmInBackground(key string, cand Candidate, ns string, secondary bool) {
	c.inflightMu.Lock()
	if c.inflight == nil {
		c.inflight = map[string]bool{}
	}
	if c.inflight[key] {
		c.inflightMu.Unlock()
		return
	}
	c.inflight[key] = true
	c.inflightMu.Unlock()
	go func(cand Candidate) {
		defer func() {
			c.inflightMu.Lock()
			delete(c.inflight, key)
			c.inflightMu.Unlock()
		}()
		bgCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var confirm bool
		var err error
		if secondary {
			if vj, ok := c.Inner.(VariantJudge); ok {
				confirm, err = vj.ConfirmSecondary(bgCtx, cand)
			} else {
				confirm, err = c.Inner.Confirm(bgCtx, cand)
			}
		} else {
			confirm, err = c.Inner.Confirm(bgCtx, cand)
		}
		if err != nil {
			return // stay uncached: next request may warm again
		}
		c.Cache.Set(key, confirm)
	}(cand)
}
