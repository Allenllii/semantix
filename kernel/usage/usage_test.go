package usage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecorderAppendAndSummarize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	r, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	evs := []Event{
		{SessionID: "s1", Turn: 1, TokensIn: 1000, TokensOut: 200, CacheHitToken: 600},
		{SessionID: "s1", Turn: 2, TokensIn: 1200, TokensOut: 250, CacheHitToken: 1000, InjectedTokens: 300},
		{SessionID: "s2", Turn: 1, TokensIn: 800, TokensOut: 150, L3Reuse: true},
	}
	for _, e := range evs {
		if err := r.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Summarize(path, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if s.Events != 3 {
		t.Fatalf("events = %d, want 3", s.Events)
	}
	if s.TokensIn != 3000 || s.TokensOut != 600 {
		t.Fatalf("tokens = %d/%d, want 3000/600", s.TokensIn, s.TokensOut)
	}
	if s.CacheHitTokens != 1600 {
		t.Fatalf("cache hits = %d, want 1600", s.CacheHitTokens)
	}
	if s.L3Reuses != 1 || s.InjectedTokens != 300 {
		t.Fatalf("L3=%d injected=%d", s.L3Reuses, s.InjectedTokens)
	}
	// Billed: in 3000-1600-800=600 miss + 1600 hit + out 600-150=450 miss.
	// L3 turn (800 in + 150 out) excluded entirely.
	wantPaid := 600*0.27/1e6 + 1600*0.07/1e6 + 450*0.27/1e6
	wantNoCache := 3000*0.27/1e6 + 600*0.27/1e6
	if !approx(s.CostPaidUSD, wantPaid) {
		t.Fatalf("paid = %v, want %v", s.CostPaidUSD, wantPaid)
	}
	if !approx(s.CostNoCacheUSD, wantNoCache) {
		t.Fatalf("noCache = %v, want %v", s.CostNoCacheUSD, wantNoCache)
	}
	if !approx(s.SavingsUSD, wantNoCache-wantPaid) {
		t.Fatalf("savings = %v", s.SavingsUSD)
	}
	if s.SavingsRate <= 0 || s.SavingsRate >= 1 {
		t.Fatalf("savings rate = %v, want in (0,1)", s.SavingsRate)
	}
}

func TestSummarizeToleratesBadLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if _, err := NewRecorder(path); err != nil {
		t.Fatal(err)
	}
	bad := []byte("not json\n{\"turn\": 1, \"tokens_in\": 500, \"tokens_out\": 100}\n\ngarbage\n")
	// Direct write to create a mixed log.
	writeAll(t, path, bad)
	s, err := Summarize(path, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if s.Events != 1 {
		t.Fatalf("events = %d, want 1 (bad lines skipped)", s.Events)
	}
	if s.TokensIn != 500 {
		t.Fatalf("tokens_in = %d, want 500", s.TokensIn)
	}
}

func TestSummarizeEmptyLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if _, err := NewRecorder(path); err != nil {
		t.Fatal(err)
	}
	s, err := Summarize(path, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if s.Events != 0 || s.SavingsUSD != 0 || s.SavingsRate != 0 {
		t.Fatalf("empty log must yield zero summary, got %+v", s)
	}
}

func TestSummarizeMissingFile(t *testing.T) {
	if _, err := Summarize(filepath.Join(t.TempDir(), "nope.jsonl"), 1, 1); err == nil {
		t.Fatal("missing file must error")
	}
}

func approx(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

func writeAll(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
