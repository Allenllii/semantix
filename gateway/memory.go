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
// pipeline (P/C/R/T/M). The same worker also runs the L3 write-back
// (design §3.5) off the request goroutine — capturing a deps-tree
// fingerprint can take seconds on large repositories, and the main chain
// must stay <10ms local (design §3.3).
//
// Everything here is best-effort: failures are logged, never fatal.

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

// memJob is one queued write-memory unit.
type memJob struct {
	sessionID string
	meta      forwardMeta // write-back
	content   string      // write-back
	toolCalls bool        // write-back
}

// memoryWorker processes session extraction and L3 write-back jobs
// asynchronously. It exists when either write-memory or L3 write-back is
// enabled.
type memoryWorker struct {
	dir     string // sessions dir; empty = session extraction disabled
	extract bool
	s       *Server

	jobs chan memJob
	wg   sync.WaitGroup
	done chan struct{}
}

// newMemoryWorker starts the async worker goroutine.
func newMemoryWorker(s *Server, dir string, extract bool) *memoryWorker {
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			s.logf("sessions dir: %v", err)
		}
	}
	w := &memoryWorker{
		dir:     dir,
		extract: extract,
		s:       s,
		jobs:    make(chan memJob, 256),
		done:    make(chan struct{}),
	}
	go w.loop()
	return w
}

// appendSession appends transcript lines to <dir>/<sessionID>.jsonl (0600,
// append-only). Failure is best-effort: logged, never fatal to the chain.
func (w *memoryWorker) appendSession(sessionID string, lines []sessionLine) {
	if w == nil || w.dir == "" {
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

// submitSession queues a session file for extraction (non-blocking; a full
// queue drops the job — extraction is best-effort, a later request
// re-queues it).
func (w *memoryWorker) submitSession(sessionID string) {
	if w == nil || w.dir == "" || !w.extract {
		return
	}
	w.enqueue(memJob{sessionID: sessionID})
}

// submitWriteback queues an L3 write-back.
func (w *memoryWorker) submitWriteback(meta forwardMeta, content string, toolCalls bool) {
	if w == nil {
		return
	}
	w.enqueue(memJob{meta: meta, content: content, toolCalls: toolCalls})
}

func (w *memoryWorker) enqueue(j memJob) {
	w.wg.Add(1)
	select {
	case w.jobs <- j:
	default:
		w.wg.Done() // queue full: skip this pass
	}
}

// flush blocks until all queued jobs are processed (test hook).
func (w *memoryWorker) flush() {
	if w == nil {
		return
	}
	w.wg.Wait()
}

// close drains the queue and stops the worker goroutine.
func (w *memoryWorker) close() {
	if w == nil {
		return
	}
	select {
	case <-w.done:
		return
	default:
		close(w.done)
	}
}

func (w *memoryWorker) loop() {
	for {
		select {
		case j := <-w.jobs:
			w.process(j)
		case <-w.done:
			// Drain whatever is already queued (never lose accepted
			// jobs; wg.Done is guaranteed for every enqueued job).
			for {
				select {
				case j := <-w.jobs:
					w.process(j)
				default:
					return
				}
			}
		}
	}
}

// process runs one job.
func (w *memoryWorker) process(j memJob) {
	defer w.wg.Done()
	if j.sessionID != "" {
		w.extractSession(j.sessionID)
		return
	}
	w.s.maybeWriteBack(j.meta, j.content, j.toolCalls)
}

// extractSession runs the ingest pipeline over one session file (idempotent:
// slices dedup by content-derived ID via store.Put).
func (w *memoryWorker) extractSession(sessionID string) {
	path := filepath.Join(w.dir, safeFilename(sessionID)+".jsonl")
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
