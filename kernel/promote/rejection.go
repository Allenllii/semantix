package promote

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
)

// ErrBlacklisted is returned by Decision.Promote when the (query, slice)
// pair has been rejected often enough to enter the failure blacklist
// (Issue #280, A-MemGuard double-memory: repeated rejections block
// promotion even when a later judge approves).
var ErrBlacklisted = errors.New("promote: candidate blacklisted by repeated rejections")

// Rejection is one failure-lesson record: a judge decline or a consensus
// failure for a (query, slice) pair. Rejections live in their OWN
// namespace — never injected, never indexed, never retrieved — and are
// consumed only by the promotion decision (blacklist evidence).
type Rejection struct {
	SourceSliceID string
	Query         string
	Reason        string // "judge_declined" | "consensus_failed"
	RejectedAt    int64
}

// RejectionStore persists failure lessons in an independent namespace.
type RejectionStore interface {
	Add(r Rejection) error
	// Count returns the number of rejections for (sourceSliceID, query)
	// that are still inside the retention window (lazily expiring older
	// ones so the file cannot grow without bound).
	Count(sourceSliceID, query string, now, ttl int64) (int, error)
}

// MemRejectionStore is an in-memory RejectionStore for tests.
type MemRejectionStore struct {
	mu sync.Mutex
	m  []Rejection
}

// NewMemRejectionStore returns an empty in-memory store.
func NewMemRejectionStore() *MemRejectionStore { return &MemRejectionStore{} }

// Add appends one rejection record.
func (s *MemRejectionStore) Add(r Rejection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = append(s.m, r)
	return nil
}

// Count counts in-window rejections, expiring older records.
func (s *MemRejectionStore) Count(sourceSliceID, query string, now, ttl int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.m[:0]
	n := 0
	for _, r := range s.m {
		if r.SourceSliceID == sourceSliceID && r.Query == query &&
			(ttl <= 0 || now-r.RejectedAt <= ttl) {
			n++
		}
		if ttl > 0 && now-r.RejectedAt > ttl {
			continue // expired: drop
		}
		kept = append(kept, r)
	}
	s.m = kept
	return n, nil
}

// rejectionFileStore is a zero-dependency JSONL RejectionStore mirroring
// fileStore: 0600, atomic rewrite under a mutex, tolerant reads.
type rejectionFileStore struct {
	mu   sync.Mutex
	path string
}

// NewRejectionFileStore opens (or creates) the rejection JSONL at path.
func NewRejectionFileStore(path string) (RejectionStore, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	return &rejectionFileStore{path: path}, nil
}

// Add appends a rejection (full rewrite under mutex; volumes are small).
func (s *rejectionFileStore) Add(r Rejection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return err
	}
	all = append(all, r)
	return s.writeAll(all)
}

// Count counts in-window rejections and rewrites the file without the
// expired records (lazy bounded growth).
func (s *rejectionFileStore) Count(sourceSliceID, query string, now, ttl int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return 0, err
	}
	kept := all[:0]
	n := 0
	for _, r := range all {
		expired := ttl > 0 && now-r.RejectedAt > ttl
		if r.SourceSliceID == sourceSliceID && r.Query == query && !expired {
			n++
		}
		if !expired {
			kept = append(kept, r)
		}
	}
	if len(kept) != len(all) {
		if err := s.writeAll(kept); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (s *rejectionFileStore) readAll() ([]Rejection, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Rejection
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r Rejection
		if err := json.Unmarshal(line, &r); err != nil {
			continue // tolerant: skip corrupt lines
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

func (s *rejectionFileStore) writeAll(rs []Rejection) error {
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].RejectedAt < rs[j].RejectedAt })
	var b bytes.Buffer
	for _, r := range rs {
		line, err := json.Marshal(r)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
