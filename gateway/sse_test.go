package gateway

import (
	"strings"
	"testing"
)

func feedAll(a *sseAggregator, stream string) {
	a.Feed([]byte(stream))
}

func TestSSEAggregatorFullStream(t *testing.T) {
	a := newSSEAggregator(maxSSEAggregateBytes)
	feedAll(a, ""+
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"},\"index\":0}]}\n\n"+
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello \"},\"index\":0}]}\n\n"+
		"data: {\"choices\":[{\"delta\":{\"content\":\"streaming \"},\"index\":0}]}\n\n"+
		"data: {\"choices\":[{\"delta\":{\"content\":\"world\"},\"index\":0}]}\n\n"+
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\",\"index\":0}]}\n\n"+
		"data: [DONE]\n\n")
	if !a.Complete() {
		t.Error("Complete() = false, want true for a cleanly terminated stream")
	}
	if got := a.Content(); got != "hello streaming world" {
		t.Errorf("Content() = %q, want %q", got, "hello streaming world")
	}
}

// chunkFeed feeds the stream in tiny fixed-size pieces, forcing lines and
// events to split across Feed calls — the relay reads 32 KiB chunks, so
// this exercises the real cross-call buffering path.
func TestSSEAggregatorSplitFeeds(t *testing.T) {
	stream := "" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"alpha \"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"beta\"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\",\"index\":0}]}\n\n" +
		"data: [DONE]\n\n"
	a := newSSEAggregator(maxSSEAggregateBytes)
	for i := 0; i < len(stream); i += 7 {
		end := i + 7
		if end > len(stream) {
			end = len(stream)
		}
		a.Feed([]byte(stream[i:end]))
	}
	if !a.Complete() {
		t.Error("Complete() = false, want true")
	}
	if got := a.Content(); got != "alpha beta" {
		t.Errorf("Content() = %q, want %q", got, "alpha beta")
	}
}

func TestSSEAggregatorCRLF(t *testing.T) {
	a := newSSEAggregator(maxSSEAggregateBytes)
	feedAll(a, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}]}\r\n\r\ndata: [DONE]\r\n\r\n")
	if !a.Complete() || a.Content() != "ok" {
		t.Errorf("CRLF stream: Complete=%v Content=%q", a.Complete(), a.Content())
	}
}

func TestSSEAggregatorNoTerminator(t *testing.T) {
	a := newSSEAggregator(maxSSEAggregateBytes)
	feedAll(a, "data: {\"choices\":[{\"delta\":{\"content\":\"half\"},\"index\":0}]}\n\n")
	if a.Complete() {
		t.Error("Complete() = true without [DONE] or finish_reason, want false (fail-closed)")
	}
}

func TestSSEAggregatorDoneWithTrailingSpace(t *testing.T) {
	// "data: [DONE] " is not the canonical terminator: fail closed rather
	// than risk persisting a stream that was not cleanly closed.
	a := newSSEAggregator(maxSSEAggregateBytes)
	feedAll(a, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"},\"index\":0}]}\n\ndata: [DONE] \n\n")
	if a.Complete() {
		t.Error("Complete() = true for a non-canonical [DONE] payload, want false")
	}
}

func TestSSEAggregatorEmptyFinishReason(t *testing.T) {
	// a null-ish empty finish_reason must not count as a termination signal
	a := newSSEAggregator(maxSSEAggregateBytes)
	feedAll(a, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"\"},\"index\":0}]}\n\n")
	if a.Complete() {
		t.Error("Complete() = true for empty finish_reason, want false")
	}
}

func TestSSEAggregatorMultiLineDataTolerated(t *testing.T) {
	// multi-line data payloads are valid SSE but not OpenAI chunk JSON;
	// they must be skipped without disturbing later events
	a := newSSEAggregator(maxSSEAggregateBytes)
	feedAll(a, "data: line one\ndata: line two\n\n"+
		"data: {\"choices\":[{\"delta\":{\"content\":\"after\"},\"index\":0}]}\n\n"+
		"data: [DONE]\n\n")
	if !a.Complete() || a.Content() != "after" {
		t.Errorf("multi-line data: Complete=%v Content=%q, want true/%q", a.Complete(), a.Content(), "after")
	}
}

func TestSSEAggregatorBadJSONTolerated(t *testing.T) {
	a := newSSEAggregator(maxSSEAggregateBytes)
	feedAll(a, "data: {not json}\n\n"+
		"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}]}\n\n"+
		"data: [DONE]\n\n")
	if !a.Complete() || a.Content() != "ok" {
		t.Errorf("bad JSON: Complete=%v Content=%q, want true/%q", a.Complete(), a.Content(), "ok")
	}
}

func TestSSEAggregatorOnlyFirstChoice(t *testing.T) {
	a := newSSEAggregator(maxSSEAggregateBytes)
	feedAll(a, "data: {\"choices\":[{\"delta\":{\"content\":\"first \"},\"index\":0},{\"delta\":{\"content\":\"second\"},\"index\":1}]}\n\n"+
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\",\"index\":0}]}\n\n"+
		"data: [DONE]\n\n")
	if got := a.Content(); got != "first " {
		t.Errorf("Content() = %q, want only choices[0] content", got)
	}
}

func TestSSEAggregatorContentOverflow(t *testing.T) {
	a := newSSEAggregator(16) // tiny bound: "hello world" (11) fits, more trips it
	feedAll(a, "data: {\"choices\":[{\"delta\":{\"content\":\"hello world\"},\"index\":0}]}\n\n"+
		"data: {\"choices\":[{\"delta\":{\"content\":\"overflow\"},\"index\":0}]}\n\n"+
		"data: [DONE]\n\n")
	if a.Complete() {
		t.Error("Complete() = true past the content bound, want false (fail-closed)")
	}
}

func TestSSEAggregatorLineOverflow(t *testing.T) {
	a := newSSEAggregator(maxSSEAggregateBytes)
	// a single endless line (no newline) must trip the line bound, not grow
	// the buffer without limit
	feedAll(a, "data: "+strings.Repeat("x", maxSSELineBytes+1))
	if a.Complete() {
		t.Error("Complete() = true past the line bound, want false")
	}
	// once overflowed, further feeds are ignored (no growth, no parsing)
	feedAll(a, "data: [DONE]\n\n")
	if a.Complete() {
		t.Error("Complete() = true after overflow, want false")
	}
}

func TestSSEAggregatorPendingOverflow(t *testing.T) {
	a := newSSEAggregator(maxSSEAggregateBytes)
	payload := strings.Repeat("x", maxSSELineBytes/2)

	// Each data line stays below the per-line bound, but one event without a
	// terminating blank line must not let pending payloads grow without bound.
	for i := 0; i <= maxSSEAggregateBytes/(len(payload)+1); i++ {
		feedAll(a, "data: "+payload+"\n")
	}

	if !a.Overflowed() {
		t.Fatal("Overflowed() = false past the pending event bound, want true")
	}
	if a.Complete() {
		t.Error("Complete() = true past the pending event bound, want false")
	}
	if len(a.pending) != 0 {
		t.Errorf("len(pending) = %d after overflow, want 0", len(a.pending))
	}

	// Once overflowed, a later terminator must not make the stream complete.
	feedAll(a, "\ndata: [DONE]\n\n")
	if a.Complete() {
		t.Error("Complete() = true after pending overflow, want false")
	}
}

func TestSSEAggregatorSawUsage(t *testing.T) {
	cases := []struct {
		name   string
		stream string
		want   bool
	}{
		{"no usage", "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n", false},
		{"usage block", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1}}\n\ndata: [DONE]\n\n", true},
		{"usage with choices", "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}],\"usage\":{\"prompt_tokens\":1}}\n\ndata: [DONE]\n\n", true},
		{"null usage", "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}],\"usage\":null}\n\ndata: [DONE]\n\n", false},
		// a content fragment mentioning "usage" must never misfire
		{"content mentions usage", "data: {\"choices\":[{\"delta\":{\"content\":\"about usage tracking\"}}]}\n\ndata: [DONE]\n\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newSSEAggregator(maxSSEAggregateBytes)
			feedAll(a, c.stream)
			if got := a.SawUsage(); got != c.want {
				t.Errorf("SawUsage() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestSSEAggregatorUsageRaw covers the #291 capture: the raw usage object of
// the last usage-bearing frame is retained for real-accounting metering.
func TestSSEAggregatorUsageRaw(t *testing.T) {
	a := newSSEAggregator(maxSSEAggregateBytes)
	feedAll(a, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}],\"usage\":{\"prompt_tokens\":1}}\n\n"+
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":2620,\"completion_tokens\":15,\"prompt_tokens_details\":{\"cached_tokens\":2560}}}\n\n"+
		"data: [DONE]\n\n")
	nu, ok := parseUsageRaw(a.UsageRaw())
	if !ok {
		t.Fatalf("UsageRaw did not parse: %q", a.UsageRaw())
	}
	// Last frame wins: OpenAI streams put the authoritative usage last.
	if nu.Prompt != 2620 || nu.CacheHit != 2560 || nu.Completion != 15 {
		t.Fatalf("normalized usage = %+v", nu)
	}

	empty := newSSEAggregator(maxSSEAggregateBytes)
	feedAll(empty, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n")
	if empty.UsageRaw() != nil {
		t.Fatalf("stream without usage must return nil raw, got %q", empty.UsageRaw())
	}
}
