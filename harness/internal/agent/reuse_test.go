package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"semantix/harness/internal/event"
	"semantix/harness/internal/semantix"
)

func TestReuseNoticeHiddenWhenNoHits(t *testing.T) {
	for _, r := range []semantix.ReuseSummary{
		{},                         // kernel off / no hits
		{Hits: 0, SavingsUSD: 0.5}, // savings without hits → nothing to show
	} {
		if _, ok := reuseNotice(r); ok {
			t.Errorf("reuseNotice(%+v) emitted, want hidden", r)
		}
	}
}

func TestReuseNoticePayload(t *testing.T) {
	r := semantix.ReuseSummary{Hits: 3, SavingsUSD: 0.0042, Sources: []string{"boot-1", "boot-2"}}
	ev, ok := reuseNotice(r)
	if !ok {
		t.Fatal("reuseNotice hidden, want emitted")
	}
	if ev.Kind != event.Notice {
		t.Errorf("kind = %v, want Notice", ev.Kind)
	}
	if ev.Code != event.NoticeCodeSemantixReuse {
		t.Errorf("code = %q, want %q", ev.Code, event.NoticeCodeSemantixReuse)
	}
	if !strings.Contains(ev.Text, "📦 3 slices") {
		t.Errorf("text = %q, want slice count", ev.Text)
	}
	var got semantix.ReuseSummary
	if err := json.Unmarshal([]byte(ev.Detail), &got); err != nil {
		t.Fatalf("detail not JSON: %v", err)
	}
	if got.Hits != 3 || got.SavingsUSD != 0.0042 || len(got.Sources) != 2 {
		t.Errorf("decoded summary = %+v, want round-trip", got)
	}
}
