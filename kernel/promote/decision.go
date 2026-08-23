package promote

// Decision aggregates the promotion store and the failure-lesson
// (rejection) store into the single decision surface the L3 cache
// consumes (Issue #280): TTL-aware promotion lookup, blacklist-gated
// writes, and failure recording. It is a plain struct — the L3 side
// satisfies its own Promote interface structurally, no glue code.
type Decision struct {
	entries     Store
	rejections  RejectionStore
	ttl         int64 // seconds; 0 = no expiry (Security §4.2.4 default is a TTL)
	rejectLimit int   // rejections before a candidate is blacklisted; 0 = disabled
}

// NewDecision builds the decision aggregate.
func NewDecision(entries Store, rejections RejectionStore, ttl, rejectLimit int64) *Decision {
	return &Decision{entries: entries, rejections: rejections, ttl: ttl, rejectLimit: int(rejectLimit)}
}

// Lookup reports whether (sourceSliceID, query) has a promotion entry
// whose content version matches the current slice content and whose
// PromotedAt is inside the TTL window. Expired entries are lazily deleted
// (bounded growth, Security §4.2.4: "verified" never exempts from TTL).
// Deletion works on the snapshot this call already holds — a concurrent
// Promote that lands between the snapshot and the cleanup cannot be
// mistaken for an expired entry (TOCTOU-safe).
func (d *Decision) Lookup(sourceSliceID, query string, currentVersion string, now int64) bool {
	entries, err := d.entries.List(sourceSliceID)
	if err != nil {
		return false // store failure → conservative miss
	}
	hit := false
	expired := false
	for _, e := range entries {
		if e.expiredBy(now, d.ttl) {
			expired = true
			continue
		}
		if e.Query == query && e.ContentVersion == currentVersion {
			hit = true
		}
	}
	if expired {
		// Lazy expiry: delete only the entries present in THIS snapshot
		// that are outside the window. Entries written concurrently after
		// the snapshot are out of scope by construction (TOCTOU-safe).
		for _, e := range entries {
			if !e.expiredBy(now, d.ttl) {
				continue
			}
			if err := d.entries.Delete(sourceSliceID, e.ContentVersion, e.Query); err != nil {
				// Best-effort: a failed delete must not turn a real hit
				// into a miss — hit was already decided above.
				break
			}
		}
	}
	return hit
}

// expiredBy reports whether an entry is outside the TTL window.
func (e Entry) expiredBy(now, ttl int64) bool {
	return ttl > 0 && now-e.PromotedAt > ttl
}

// Blacklisted reports whether (sourceSliceID, query) has accumulated
// rejectLimit or more in-window rejections (A-MemGuard double-memory:
// repeated failures hard-block promotion).
func (d *Decision) Blacklisted(sourceSliceID, query string, now int64) bool {
	if d.rejectLimit <= 0 {
		return false
	}
	n, err := d.rejections.Count(sourceSliceID, query, now, d.ttl)
	if err != nil {
		return false // store failure → conservative allow (decision path unaffected)
	}
	return n >= d.rejectLimit
}

// Promote writes a promotion entry unless the candidate is blacklisted.
// A blacklisted candidate returns ErrBlacklisted so the caller can count
// and log the block (the L3 reuse path itself is NOT affected — the
// blacklist gates only promotion, per spec §2.1).
func (d *Decision) Promote(e Entry) error {
	if d.Blacklisted(e.SourceSliceID, e.Query, e.PromotedAt) {
		return ErrBlacklisted
	}
	return d.entries.Put(e)
}

// Rejected records one failure lesson (judge decline / consensus failure)
// in the independent rejection namespace.
func (d *Decision) Rejected(sourceSliceID, query, reason string, now int64) {
	_ = d.rejections.Add(Rejection{
		SourceSliceID: sourceSliceID,
		Query:         query,
		Reason:        reason,
		RejectedAt:    now,
	})
}
