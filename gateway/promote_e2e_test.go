package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"semantix/kernel/zone"
)

// countingJudgeServer answers "yes" and counts every judge call, so tests
// can assert promotion skips the judge on the second grey hit (Issue #280).
func countingJudgeServer(t *testing.T) (string, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"yes"}}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &calls
}

// promotingGateway wires the real judge + promotion stores + consensus=2
// and forces every candidate into the grey band.
func promotingGateway(t *testing.T, upURL, judgeBase string) (*Gateway, string) {
	t.Helper()
	dir := t.TempDir()
	g := newTestGatewayCfg(t, upURL, func(c *Config) {
		c.Cache.JudgeAPIKey = "jk"
		c.Cache.JudgeBaseURL = judgeBase
		c.Cache.JudgeModel = "judge-model"
		c.Cache.PromoteDB = filepath.Join(dir, "promote.jsonl")
		c.Cache.PromoteConsensus = 2
	})
	z := zoneZones(t)
	g.decider.Zones = &z
	return g, filepath.Join(dir, "promote.jsonl")
}

func zoneZones(t *testing.T) zone.Zones {
	t.Helper()
	return zone.Zones{TauHigh: 1.5, TauLow: 0.5, AbsHigh: 0.7, AbsLow: 0.45}
}

// End-to-end (Issue #280 spec §验收): a grey candidate approved under the
// dual-rubric consensus gets promoted; the SAME query+slice on a later hit
// is served from the promotion entry — zero additional judge calls.
func TestPromoteSecondHitSkipsJudge(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"unused"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	judgeBase, calls := countingJudgeServer(t)
	g, promoteDB := promotingGateway(t, upSrv.URL, judgeBase)
	srv := httptest.NewServer(g)
	defer srv.Close()
	seedGreyCandidate(t, g, "grey-promote", "hello world")

	// First hit: primary judge + consensus secondary = 2 judge calls.
	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("x-semantix-cache") != "hit" {
		t.Fatalf("first hit: status=%d cache=%q body=%s", resp.StatusCode, resp.Header.Get("x-semantix-cache"), out)
	}
	if calls.Load() != 2 {
		t.Fatalf("judge calls after first hit = %d, want 2 (primary + consensus secondary)", calls.Load())
	}
	// The promotion entry must be persisted with the exact query.
	entryBytes, err := os.ReadFile(promoteDB)
	if err != nil {
		t.Fatalf("read promote db: %v", err)
	}
	if !strings.Contains(string(entryBytes), "hello world") {
		t.Fatalf("promotion entry missing from db:\n%s", entryBytes)
	}

	// Second hit on the same query: promotion hit, judge skipped entirely.
	resp2, out2 := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp2.StatusCode != http.StatusOK || resp2.Header.Get("x-semantix-cache") != "hit" {
		t.Fatalf("second hit: status=%d cache=%q body=%s", resp2.StatusCode, resp2.Header.Get("x-semantix-cache"), out2)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("judge calls after second hit = %d, want still 2 (promotion skipped the judge)", got)
	}
}

// consensus=1 keeps the single-judgement baseline: one judge call per
// grey hit, still promoted (no consensus wrap).
func TestPromoteBaselineConsensus1(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"unused"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	judgeBase, calls := countingJudgeServer(t)
	g := newTestGatewayCfg(t, upSrv.URL, func(c *Config) {
		c.Cache.JudgeAPIKey = "jk"
		c.Cache.JudgeBaseURL = judgeBase
		c.Cache.JudgeModel = "judge-model"
		c.Cache.PromoteDB = filepath.Join(t.TempDir(), "promote.jsonl")
		c.Cache.PromoteConsensus = 1
	})
	z := zoneZones(t)
	g.decider.Zones = &z
	srv := httptest.NewServer(g)
	defer srv.Close()
	seedGreyCandidate(t, g, "grey-base", "hello world")

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("x-semantix-cache") != "hit" {
		t.Fatalf("hit: status=%d cache=%q body=%s", resp.StatusCode, resp.Header.Get("x-semantix-cache"), out)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("judge calls at consensus=1 = %d, want 1 (single judgement)", got)
	}
	// Second hit skips the judge (promotion written at baseline too).
	_, _ = postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if got := calls.Load(); got != 1 {
		t.Fatalf("judge calls after second hit = %d, want still 1", got)
	}
}
