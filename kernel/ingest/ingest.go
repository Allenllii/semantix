// Package ingest implements the harness-event bypass (U7): a kernel-side
// receiver that turns harness session transcripts into kernel events and
// drives the slice extraction pipeline.
//
// Architecture role (see docs/events.md §3): the kernel stays decoupled from
// any specific harness. A Source yields per-session event batches; the
// Pipeline converts each batch into slices (via kernel/slice.Extractor) and
// persists them (Store + Index). A Reasonix-style JSONL source is provided;
// an in-process harness adapter would implement Source over its own event
// stream.
package ingest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"semantix/kernel/event"
	"semantix/kernel/slice"
)

// SessionEvents is one source batch: the kernel events observed for one
// session plus its raw transcript (the extractor consumes the transcript).
type SessionEvents struct {
	SessionID string
	Events    []event.Event
	Transcript []byte // raw JSONL, Reasonix-style
}

// Source yields session event batches in chronological order. It returns
// (nil, io.EOF) when exhausted.
type Source interface {
	Next() (*SessionEvents, error)
}

// --- Reasonix JSONL source ---

// JSONLSource reads Reasonix-style session JSONL files (one JSON object per
// line: role/content/tool_calls) from explicit paths or a directory
// (non-recursive, *.jsonl), ordered by mtime.
type JSONLSource struct {
	files []string
	idx   int
}

// NewJSONLSource builds a source from files or directories.
func NewJSONLSource(paths ...string) (*JSONLSource, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
					files = append(files, filepath.Join(p, e.Name()))
				}
			}
			continue
		}
		files = append(files, p)
	}
	sort.Slice(files, func(i, j int) bool {
		a, aerr := os.Stat(files[i])
		b, berr := os.Stat(files[j])
		if aerr != nil || berr != nil {
			return false // stat failures keep relative order; Next() will surface them
		}
		return a.ModTime().Before(b.ModTime())
	})
	return &JSONLSource{files: files}, nil
}

// Next parses the next session file into kernel events. Line order maps to
// turn order: user message opens a turn (TurnStarted), assistant tool_calls
// become ToolDispatch, a following tool line becomes ToolResult, and a
// batch of tool results closes with ToolRoundEnd.
func (s *JSONLSource) Next() (*SessionEvents, error) {
	if s.idx >= len(s.files) {
		return nil, io.EOF
	}
	path := s.files[s.idx]
	s.idx++

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	se := &SessionEvents{
		SessionID:  strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Transcript: raw,
	}
	lineNo := 0
	turn := 0
	var round []event.Event // ToolResult batch for ToolRoundEnd
	flushRound := func() {
		if len(round) > 0 {
			ok := 0
			for _, e := range round {
				var p event.ToolResultPayload
				if json.Unmarshal(e.Data, &p) == nil && p.OK {
					ok++
				}
			}
			payload, _ := json.Marshal(event.ToolRoundEndPayload{Dispatched: len(round), Succeeded: ok})
			se.Events = append(se.Events, event.Event{
				Kind: event.ToolRoundEnd, SessionID: se.SessionID, Turn: turn,
				At: time.Now(), Data: payload,
			})
			round = nil
		}
	}

	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var l struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"tool_calls,omitempty"`
		}
		if err := json.Unmarshal(line, &l); err != nil {
			continue // tolerant: skip corrupt lines
		}
		at := time.Now()
		switch l.Role {
		case "user":
			flushRound()
			turn++
			se.Events = append(se.Events, event.Event{
				Kind: event.TurnStarted, SessionID: se.SessionID, Turn: turn, At: at,
			})
		case "assistant":
			for _, tc := range l.ToolCalls {
				payload, _ := json.Marshal(event.ToolDispatchPayload{CallID: tc.ID, Name: tc.Name})
				se.Events = append(se.Events, event.Event{
					Kind: event.ToolDispatch, SessionID: se.SessionID, Turn: turn, At: at, Data: payload,
				})
			}
		case "tool":
			payload, _ := json.Marshal(event.ToolResultPayload{CallID: fmt.Sprintf("c%d", len(round)+1), OK: true})
			se.Events = append(se.Events, event.Event{
				Kind: event.ToolResult, SessionID: se.SessionID, Turn: turn, At: at, Data: payload,
			})
			round = append(round, se.Events[len(se.Events)-1])
		}
	}
	flushRound()
	return se, sc.Err()
}

// IsEOF reports whether err signals source exhaustion.
func IsEOF(err error) bool { return err == io.EOF }

// --- Pipeline ---

// Pipeline converts source batches into persisted slices.
type Pipeline struct {
	Extractor slice.Extractor
	Store     slice.Store
	Index     slice.Index
	Scope     slice.Scope
	Project   string
}

// Run drains the source, extracting and persisting slices per session.
// It returns per-session counters.
func (p Pipeline) Run(src Source) (map[string]int, error) {
	stats := map[string]int{}
	for {
		se, err := src.Next()
		if err != nil {
			if IsEOF(err) {
				return stats, nil
			}
			return stats, err
		}
		slices, err := p.Extractor.Extract(se.Transcript, slice.SliceMeta{
			SourceSession: se.SessionID,
			ProjectSlug:   p.Project,
		})
		if err != nil {
			return stats, fmt.Errorf("ingest %s: extract: %w", se.SessionID, err)
		}
		n := 0
		for _, sl := range slices {
			sl.Scope = p.Scope
			if err := p.Store.Put(sl); err != nil {
				return stats, fmt.Errorf("ingest %s: put %s: %w (persisted %d of %d slices before failure)",
					se.SessionID, sl.ID, err, n, len(slices))
			}
			if err := p.Index.Insert(sl); err != nil {
				return stats, fmt.Errorf("ingest %s: index %s: %w", se.SessionID, sl.ID, err)
			}
			n++
		}
		stats[se.SessionID] = n
	}
}
