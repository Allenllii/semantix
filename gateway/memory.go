package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"semantix/kernel/ingest"
	"semantix/kernel/slice"
)

// This file implements the write-memory path (design §3.7): every
// request/response pair is bypassed to a per-session JSONL file, and an
// async worker extracts the sessions into slices via the kernel ingest
// pipeline (P/C/R/T/M). Extraction is best-effort and never blocks the
// main chain.

// sessionLine is one transcript line, compatible with ingest.JSONLSource.
type sessionLine struct {
	Role      string            `json:"role"`
	Content   string            `json:"content,omitempty"`
	ToolCalls []sessionToolCall `json:"tool_calls,omitempty"`
}

// sessionToolCall mirrors the transcript tool-call shape the extractor
// understands (name is the only consumed field).
type sessionToolCall struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// memoryWorker appends session transcripts and extracts them asynchronously.
type memoryWorker struct {
	dir     string
	extract bool
	s       *Server

	jobs chan string
	wg   sync.WaitGroup
	done chan struct{}
}

// newMemoryWorker starts the async extraction worker (when extract is true).
func newMemoryWorker(s *Server, dir string, extract bool) *memoryWorker {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.logf("sessions dir: %v", err)
	}
	w := &memoryWorker{
		dir:     dir,
		extract: extract,
		s:       s,
		jobs:    make(chan string, 256),
		done:    make(chan struct{}),
	}
	if extract {
		go w.loop()
	}
	return w
}

// appendSession appends transcript lines to <dir>/<sessionID>.jsonl (0600,
// append-only). Failure is best-effort: logged, never fatal to the chain.
func (w *memoryWorker) appendSession(sessionID string, lines []sessionLine) {
	if w == nil {
		return
	}
	path := filepath.Join(w.dir, safeFilename(sessionID)+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		w.s.logf("session append %s: %v", sessionID, err)
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, l := range lines {
		if err := enc.Encode(l); err != nil {
			w.s.logf("session encode %s: %v", sessionID, err)
			return
		}
	}
}

// submit queues a session file for extraction (non-blocking; a full queue
// drops the job — extraction is best-effort, a later request re-queues it).
func (w *memoryWorker) submit(sessionID string) {
	if w == nil || !w.extract {
		return
	}
	path := filepath.Join(w.dir, safeFilename(sessionID)+".jsonl")
	w.wg.Add(1)
	select {
	case w.jobs <- path:
	default:
		w.wg.Done() // queue full: skip this pass
	}
}

// flush blocks until all queued extractions are processed (test hook).
func (w *memoryWorker) flush() {
	if w == nil {
		return
	}
	w.wg.Wait()
}

// close stops the worker goroutine.
func (w *memoryWorker) close() {
	if w == nil {
		return
	}
	if w.extract {
		select {
		case <-w.done:
		default:
			close(w.done)
		}
	}
}

func (w *memoryWorker) loop() {
	for {
		select {
		case path := <-w.jobs:
			w.process(path)
		case <-w.done:
			return
		}
	}
}

// process extracts one session file into slices (idempotent: slices dedup
// by content-derived ID via store.Put).
func (w *memoryWorker) process(path string) {
	defer w.wg.Done()
	pipe := ingest.Pipeline{
		Extractor: slice.NewExtractor(),
		Store:     w.s.store,
		Index:     w.s.index,
		Scope:     w.s.cfg.Scope,
	}
	src, err := ingest.NewJSONLSource(path)
	if err != nil {
		w.s.logf("extract %s: %v", path, err)
		return
	}
	if _, err := pipe.Run(src); err != nil {
		w.s.logf("extract %s: %v", path, err)
	}
}

// safeFilename neutralizes session ids that could escape the sessions dir
// (design §3.10 path hygiene).
func safeFilename(id string) string {
	out := make([]rune, 0, len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
