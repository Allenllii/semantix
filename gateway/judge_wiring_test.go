package gateway

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/judge"
	"semantix/kernel/slice"
)

// newTestGatewayCfg builds a test gateway with a custom config mutation
// (retriever kind / judge wiring), sharing newTestGateway's defaults.
func newTestGatewayCfg(t *testing.T, upURL string, mut func(*Config)) *Gateway {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "test-key"
	cfg.Store.DB = filepath.Join(dir, "db.jsonl")
	cfg.Store.DepsRoot = dir
	cfg.Ingest.SessionsDir = filepath.Join(dir, "sessions")
	cfg.Ingest.UsageLog = filepath.Join(dir, "usage.jsonl")
	cfg.Upstreams = []UpstreamConfig{{
		Name: "deepseek", BaseURL: upURL, APIKey: "up-key",
		ModelAlias: []string{"deepseek-chat"}, UpstreamModel: "deepseek-chat",
		Vendor: "deepseek",
	}}
	mut(cfg)
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

func judgeURL(t *testing.T) string {
	t.Helper()
	// A judge endpoint that always approves; only reachability matters here.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`yes`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestNewWiresJudgeFromConfig(t *testing.T) {
	g := newTestGatewayCfg(t, "http://127.0.0.1:1", func(c *Config) {
		c.Cache.JudgeAPIKey = "jk"
		c.Cache.JudgeBaseURL = judgeURL(t)
		c.Cache.JudgeModel = "judge-model"
	})
	d := g.decider
	if d.Judge == nil {
		t.Fatal("Judge must be wired when judge_api_key is set")
	}
	// The gateway wraps the LLM judge in a verdict cache + background warm
	// (W3), so the decider sees the decorator.
	cj, ok := d.Judge.(*judge.CachedJudge)
	if !ok {
		t.Fatalf("Judge type = %T, want *judge.CachedJudge", d.Judge)
	}
	if _, ok := cj.Inner.(*judge.LLMJudge); !ok {
		t.Fatalf("inner Judge type = %T, want *judge.LLMJudge", cj.Inner)
	}
}

func TestNewWithoutJudgeKeyLeavesJudgeNil(t *testing.T) {
	g := newTestGatewayCfg(t, "http://127.0.0.1:1", func(*Config) {})
	if g.decider.Judge != nil {
		t.Fatalf("Judge = %v, want nil when no judge_api_key", g.decider.Judge)
	}
}

func TestNewRejectsBadJudgeConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "k"
	cfg.Store.DB = filepath.Join(dir, "db.jsonl")
	cfg.Cache.JudgeAPIKey = "jk"
	cfg.Cache.JudgeBaseURL = "https://judge/v1"
	cfg.Cache.JudgeModel = "m"
	cfg.Cache.JudgeProtocol = "gemini" // NewLLMJudge rejects non-openai/anthropic
	cfg.Upstreams = []UpstreamConfig{{
		Name: "ds", BaseURL: "https://u/v1", APIKey: "k",
		ModelAlias: []string{"m"}, UpstreamModel: "m", Vendor: "deepseek",
	}}
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "judge") {
		t.Fatalf("want judge config error, got %v", err)
	}
}

// TestGatewayRetrieverKindsL3Hit proves the retriever switch works through
// the full HTTP path: a seeded Result slice is found and replayed with zero
// upstream calls under vector and hybrid retrieval, not just bm25.
func TestGatewayRetrieverKindsL3Hit(t *testing.T) {
	for _, kind := range []string{"bm25", "vector", "hybrid"} {
		t.Run(kind, func(t *testing.T) {
			up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"unused"}}]}`}
			upSrv := httptest.NewServer(up.handler())
			defer upSrv.Close()
			g := newTestGatewayCfg(t, upSrv.URL, func(c *Config) {
				c.Retrieval.Retriever = kind
			})
			srv := httptest.NewServer(g)
			defer srv.Close()

			chash, _ := contextHash([]chatMessage{msg("user", "hello world")})
			seed(t, g, &slice.Slice{
				ID: "l3-" + kind, Type: slice.Result, Scope: slice.Project,
				Content: []byte("hello world hello world cached answer"),
				Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat", Origin: slice.OriginSessionAuto},
			})

			resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, body %s", resp.StatusCode, out)
			}
			if resp.Header.Get("x-semantix-cache") != "hit" {
				t.Errorf("%s: x-semantix-cache = %q, want hit (body %s)", kind, resp.Header.Get("x-semantix-cache"), out)
			}
			if n := up.callCount(); n != 0 {
				t.Errorf("%s: upstream calls = %d, want 0", kind, n)
			}
		})
	}
}
