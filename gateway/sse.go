package gateway

import (
	"bytes"
	"encoding/json"
	"strings"
)

// maxSSEAggregateBytes bounds the assistant content accumulated out of a
// relayed SSE stream. Past this limit the aggregator stops accumulating
// (the relay continues untouched) and Complete() reports false so the
// caller fails closed — a hostile or broken upstream cannot grow memory
// without bound.
const maxSSEAggregateBytes = 1 << 20 // 1 MiB

// maxSSEPendingBytes bounds the data payload lines collected for one SSE
// event before its terminating blank line. Without this separate bound, an
// upstream could send many individually valid lines and grow pending without
// ever reaching flushEvent.
const maxSSEPendingBytes = maxSSEAggregateBytes

// maxSSELineBytes bounds a single buffered SSE line. A pathological
// upstream emitting one endless line (no newline) would otherwise grow
// lineBuf without bound; crossing the bound also fails closed.
const maxSSELineBytes = 64 * 1024

// sseAggregator parses an OpenAI chat.completion.chunk SSE stream while it
// is being relayed verbatim (design §3.4: never reorder/rewrite the
// upstream stream). It accumulates choices[0].delta.content and tracks a
// clean termination: a "data: [DONE]" event or a non-null finish_reason on
// the first choice.
//
// Tolerant by design: malformed lines and events are skipped without
// affecting the relay. Only two facts matter to the caller — Complete()
// (clean termination AND no overflow) and Content().
type sseAggregator struct {
	lineBuf      bytes.Buffer // partial line carried across Feed calls
	pending      []string     // data payload lines of the event being assembled
	pendingBytes int          // pending payload bytes, including join newlines
	content      strings.Builder
	overflow     bool
	done         bool
	sawUsage     bool   // a data event carried a top-level "usage" field
	eventID      string // id of the first chat.completion.chunk event
	limit        int    // content aggregation bound
	lineMax      int    // per-line buffer bound
}

func newSSEAggregator(limit int) *sseAggregator {
	return &sseAggregator{limit: limit, lineMax: maxSSELineBytes}
}

// Feed processes the exact bytes being relayed. Calls may split events and
// lines arbitrarily (the relay reads in 32 KiB chunks). Once a bound is
// crossed the aggregator stops parsing entirely: the stream still relays,
// but Complete() stays false so nothing is persisted.
func (a *sseAggregator) Feed(p []byte) {
	if a.overflow {
		return
	}
	if a.lineBuf.Len()+len(p) > a.lineMax {
		a.overflow = true
		a.lineBuf.Reset()
		return
	}
	a.lineBuf.Write(p)
	for {
		line, err := a.lineBuf.ReadString('\n')
		if err != nil {
			// Partial trailing line: keep it buffered for the next call.
			a.lineBuf.Reset()
			a.lineBuf.WriteString(line)
			return
		}
		a.handleLine(strings.TrimRight(line, "\r\n"))
		if a.overflow {
			a.lineBuf.Reset()
			return
		}
	}
}

func (a *sseAggregator) handleLine(line string) {
	switch {
	case strings.TrimSpace(line) == "":
		a.flushEvent()
	case strings.HasPrefix(line, "data:"):
		payload := strings.TrimPrefix(line, "data:")
		payload = strings.TrimPrefix(payload, " ")
		additional := len(payload)
		if len(a.pending) > 0 {
			additional++ // strings.Join inserts one newline between data lines
		}
		if a.pendingBytes+additional > maxSSEPendingBytes {
			a.pending = nil
			a.pendingBytes = 0
			a.overflow = true
			return
		}
		a.pending = append(a.pending, payload)
		a.pendingBytes += additional
	}
	// event:/id:/retry: lines carry no payload; ignored.
}

func (a *sseAggregator) flushEvent() {
	if len(a.pending) == 0 {
		return
	}
	payload := strings.Join(a.pending, "\n") // SSE joins multi-line data with \n
	a.pending = nil
	a.pendingBytes = 0
	if payload == "[DONE]" {
		a.done = true
		return
	}
	var evt struct {
		ID      string `json:"id"`
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		// Usage is detected structurally (not by string match) so a
		// content fragment mentioning "usage" can never misfire. The
		// raw bytes are enough: presence (non-null) is all the caller
		// needs to decide whether to synthesize a usage chunk.
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &evt); err != nil {
		return // tolerant: skip malformed events
	}
	if a.eventID == "" && evt.ID != "" {
		a.eventID = evt.ID
	}
	if len(evt.Usage) > 0 && string(evt.Usage) != "null" {
		a.sawUsage = true
	}
	if len(evt.Choices) == 0 {
		return // usage-only (or empty) event
	}
	first := evt.Choices[0]
	if first.FinishReason != nil && *first.FinishReason != "" {
		a.done = true
	}
	if a.overflow {
		return
	}
	delta := first.Delta.Content
	if delta == "" {
		return
	}
	if a.content.Len()+len(delta) > a.limit {
		a.overflow = true
		return
	}
	a.content.WriteString(delta)
}

// Complete reports whether the stream terminated cleanly ([DONE] or
// finish_reason) without exceeding the aggregation bound. Callers must not
// persist partial content when this is false (fail-closed).
func (a *sseAggregator) Complete() bool {
	return a.done && !a.overflow
}

// SawUsage reports whether any data event carried a top-level "usage"
// field — the upstream already provided usage accounting, so the caller
// must not synthesize a usage chunk.
func (a *sseAggregator) SawUsage() bool {
	return a.sawUsage
}

// Overflowed reports whether an aggregation bound was crossed. Callers
// must not synthesize usage or persist content in that case (fail-closed).
func (a *sseAggregator) Overflowed() bool {
	return a.overflow
}

// EventID returns the id of the first chat.completion.chunk event, so a
// synthesized usage chunk can reuse the stream id instead of minting a new
// one (clients that track the stream by id must not see a foreign id).
// Empty when the stream carried no id.
func (a *sseAggregator) EventID() string {
	return a.eventID
}

// Content returns the accumulated choices[0].delta.content.
func (a *sseAggregator) Content() string {
	return a.content.String()
}
