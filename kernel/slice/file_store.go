package slice

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// fileStore is a zero-dependency Store backed by a JSONL file (one Slice JSON
// per line). Chosen over bbolt for the MVP because it keeps the kernel
// dependency-free and the store human-readable; swap to bbolt if volume
// demands it later. Writes rewrite the file (slice volumes are small).
type fileStore struct {
	mu   sync.Mutex
	path string
}

// NewFileStore opens (or creates) the JSONL slice store at path.
func NewFileStore(path string) (Store, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	return &fileStore{path: path}, nil
}

// Put inserts s, replacing any existing slice with the same ID.
func (s *fileStore) Put(sl *Slice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return err
	}
	kept := make([]*Slice, 0, len(all)+1)
	for _, e := range all {
		if e.ID != sl.ID {
			kept = append(kept, e)
		}
	}
	kept = append(kept, sl)
	return s.writeAll(kept)
}

// Get returns the slice with id, or (nil, nil) when not found.
func (s *fileStore) Get(id string) (*Slice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	for _, e := range all {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, nil
}

// List returns all slices of the given scope.
func (s *fileStore) List(scope Scope) ([]*Slice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	var out []*Slice
	for _, e := range all {
		if e.Scope == scope {
			out = append(out, e)
		}
	}
	return out, nil
}

// ListAll returns every slice in the store, regardless of scope.
func (s *fileStore) ListAll() ([]*Slice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readAll()
}

// Delete removes the slice with id; a missing id is a no-op.
func (s *fileStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return err
	}
	kept := make([]*Slice, 0, len(all))
	for _, e := range all {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	if len(kept) == len(all) {
		return nil // nothing to delete
	}
	return s.writeAll(kept)
}

// UpdateStats applies delta to the slice's stats.
func (s *fileStore) UpdateStats(id string, delta SliceStats) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return err
	}
	for i := range all {
		if all[i].ID == id {
			all[i].Stats.Hits += delta.Hits
			all[i].Stats.Misses += delta.Misses
			all[i].Stats.Injected += delta.Injected
			all[i].Stats.Rejected += delta.Rejected
			all[i].Stats.UserFeedback += delta.UserFeedback
			return s.writeAll(all)
		}
	}
	return errors.New("slice: not found")
}

func (s *fileStore) readAll() ([]*Slice, error) {
	all, _, err := s.readAllSkipped()
	return all, err
}

// readAllSkipped reads every parseable slice and also reports how many store
// lines were corrupt and dropped (maintenance export surfaces this so a
// "lossless backup" cannot silently be incomplete). Oversized (> 8 MiB) lines
// are skipped and counted, exactly like Import — a store must never become
// unreadable because of one oversized/corrupt line.
func (s *fileStore) readAllSkipped() ([]*Slice, int, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	var out []*Slice
	skipped := 0
	br := bufio.NewReader(f)
	for {
		line, tooLong, err := readJSONLLine(br)
		if err == io.EOF {
			break // clean end of stream
		}
		if err != nil {
			return out, skipped, err
		}
		if tooLong {
			skipped++
			continue
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var sl Slice
		if err := json.Unmarshal(line, &sl); err != nil {
			skipped++ // tolerant: count and skip corrupt lines
			continue
		}
		out = append(out, &sl)
	}
	return out, skipped, nil
}

func (s *fileStore) writeAll(slices []*Slice) error {
	var buf bytes.Buffer
	for _, sl := range slices {
		b, err := json.Marshal(sl)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	// Write via a uniquely-named temp file (0600) then atomic rename:
	// avoids symlink pre-placement attacks and never exposes the store
	// world-readable (os.Create would use 0666&umask).
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil { // durability: flush before rename
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	return nil
}
