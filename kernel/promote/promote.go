package promote

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// Package promote implements judge-approved promotion entries (Issue #8
// acceptance ⑤): when the LLM judge approves a grey-zone candidate, an Entry
// records the source slice, its content version and the query it was approved
// for. When the source slice later changes, CascadeInvalidate invalidates
// every promoted entry pointing at the old version — rollback-safe by design.

// Entry is one judge-approved promotion.
type Entry struct {
	// SourceSliceID identifies the slice whose answer was promoted.
	SourceSliceID string
	// ContentVersion is the sha256 of the source slice content at promotion
	// time; entries never cross versions.
	ContentVersion string
	// Query is the request the judge approved (exact bytes).
	Query string
	// PromotedAt is the epoch seconds of promotion (observability only).
	PromotedAt int64
}

// Store persists promoted entries.
type Store interface {
	Put(e Entry) error
	// List returns all entries for a source slice (any version), newest last.
	List(sourceSliceID string) ([]Entry, error)
	// Delete removes one entry (rollback path).
	Delete(sourceSliceID, contentVersion, query string) error
}

// ContentVersion computes the version digest for a slice's content bytes.
func ContentVersion(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// CascadeInvalidate removes every promoted entry whose ContentVersion no
// longer matches the source slice's current version. It returns the number
// of invalidated entries; it is idempotent and never errors on a store miss.
func CascadeInvalidate(st Store, sourceSliceID string, currentContent []byte) (int, error) {
	entries, err := st.List(sourceSliceID)
	if err != nil {
		return 0, err
	}
	want := ContentVersion(currentContent)
	var invalidated int
	for _, e := range entries {
		if e.ContentVersion != want {
			if err := st.Delete(sourceSliceID, e.ContentVersion, e.Query); err != nil {
				return invalidated, fmt.Errorf("promote: cascade delete %s@%s: %w", e.SourceSliceID, e.ContentVersion, err)
			}
			invalidated++
		}
	}
	return invalidated, nil
}

// MemStore is an in-memory Store for tests and non-persistent use.
type MemStore struct {
	mu   chan struct{}
	m    map[string][]Entry
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{mu: make(chan struct{}, 1), m: map[string][]Entry{}}
}

func (s *MemStore) lock()   { s.mu <- struct{}{} }
func (s *MemStore) unlock() { <-s.mu }

// Put appends an entry (dedup by exact triple).
func (s *MemStore) Put(e Entry) error {
	s.lock()
	defer s.unlock()
	for _, old := range s.m[e.SourceSliceID] {
		if old.ContentVersion == e.ContentVersion && old.Query == e.Query {
			return nil // duplicate
		}
	}
	s.m[e.SourceSliceID] = append(s.m[e.SourceSliceID], e)
	return nil
}

// List returns entries for a source slice, newest last (by PromotedAt then
// insertion order — stable for tests).
func (s *MemStore) List(sourceSliceID string) ([]Entry, error) {
	s.lock()
	defer s.unlock()
	out := make([]Entry, len(s.m[sourceSliceID]))
	copy(out, s.m[sourceSliceID])
	sort.SliceStable(out, func(i, j int) bool { return out[i].PromotedAt < out[j].PromotedAt })
	return out, nil
}

// Delete removes one exact entry (no-op when absent).
func (s *MemStore) Delete(sourceSliceID, contentVersion, query string) error {
	s.lock()
	defer s.unlock()
	cur := s.m[sourceSliceID]
	for i, e := range cur {
		if e.ContentVersion == contentVersion && e.Query == query {
			s.m[sourceSliceID] = append(cur[:i], cur[i+1:]...)
			return nil
		}
	}
	return nil
}
