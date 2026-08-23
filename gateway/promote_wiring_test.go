package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"semantix/kernel/judge"
)

// New wires the promote decision when promote_db is configured, with the
// consensus gate when promote_consensus=2 and a judge exists; both stay
// nil when promote_db is absent (Issue #280).
func TestNewWiresPromoteDecision(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "test-key"
	cfg.Store.DB = filepath.Join(dir, "db.jsonl")
	cfg.Store.DepsRoot = dir
	cfg.Ingest.SessionsDir = filepath.Join(dir, "sessions")
	cfg.Upstreams = []UpstreamConfig{{
		Name: "deepseek", BaseURL: "http://127.0.0.1:1", APIKey: "up-key",
		ModelAlias: []string{"deepseek-chat"}, UpstreamModel: "deepseek-chat",
		Vendor: "deepseek",
	}}
	cfg.Cache.PromoteDB = filepath.Join(dir, "promote.jsonl")
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	if g.decider.Promote == nil {
		t.Fatal("Promote must be wired when promote_db is set")
	}
	// No judge configured → consensus stays nil (nothing to wrap).
	if g.decider.Consensus != nil {
		t.Fatal("Consensus must be nil without a judge")
	}

	// With a judge + consensus=2, the consensus gate wraps it.
	g2 := newTestGatewayCfg(t, "http://127.0.0.1:1", func(c *Config) {
		c.Cache.JudgeAPIKey = "jk"
		c.Cache.JudgeBaseURL = judgeURL(t)
		c.Cache.JudgeModel = "judge-model"
		c.Cache.PromoteDB = filepath.Join(t.TempDir(), "promote.jsonl")
		c.Cache.PromoteConsensus = 2
	})
	if g2.decider.Consensus == nil {
		t.Fatal("Consensus must wrap the judge when promote_consensus=2")
	}
	if _, ok := g2.decider.Consensus.(judge.Judge); !ok {
		t.Fatalf("Consensus type = %T, want judge.Judge", g2.decider.Consensus)
	}

	// consensus=1 keeps the single-judgement baseline (no wrap).
	g3 := newTestGatewayCfg(t, "http://127.0.0.1:1", func(c *Config) {
		c.Cache.JudgeAPIKey = "jk"
		c.Cache.JudgeBaseURL = judgeURL(t)
		c.Cache.JudgeModel = "judge-model"
		c.Cache.PromoteDB = filepath.Join(t.TempDir(), "promote.jsonl")
		c.Cache.PromoteConsensus = 1
	})
	if g3.decider.Consensus != nil {
		t.Fatal("Consensus must stay nil at consensus=1 (baseline)")
	}

	// promote_db absent → nothing wired.
	g4 := newTestGatewayCfg(t, "http://127.0.0.1:1", func(c *Config) {})
	if g4.decider.Promote != nil || g4.decider.Consensus != nil {
		t.Fatal("Promote/Consensus must stay nil without promote_db")
	}
}

// The promotion state persists next to the configured file and the
// rejection lessons live in an independent file (never injected).
func TestNewPromoteStoresCreated(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "test-key"
	cfg.Store.DB = filepath.Join(dir, "db.jsonl")
	cfg.Store.DepsRoot = dir
	cfg.Ingest.SessionsDir = filepath.Join(dir, "sessions")
	cfg.Upstreams = []UpstreamConfig{{
		Name: "deepseek", BaseURL: "http://127.0.0.1:1", APIKey: "up-key",
		ModelAlias: []string{"deepseek-chat"}, UpstreamModel: "deepseek-chat",
		Vendor: "deepseek",
	}}
	cfg.Cache.PromoteDB = filepath.Join(dir, "promote.jsonl")
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	for _, p := range []string{filepath.Join(dir, "promote.jsonl"), filepath.Join(dir, "rejections.jsonl")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("store file %s: %v", p, err)
		}
	}
}

func TestValidatePromoteConfig(t *testing.T) {
	base := func() *Config {
		cfg := DefaultConfig()
		cfg.Server.GatewayKey = "test-key"
		cfg.Upstreams = []UpstreamConfig{{
			Name: "deepseek", BaseURL: "http://127.0.0.1:1", APIKey: "up-key",
			ModelAlias: []string{"deepseek-chat"}, UpstreamModel: "deepseek-chat",
			Vendor: "deepseek",
		}}
		return cfg
	}
	valid := []struct{ name string; mut func(*Config) }{
		{"consensus 1", func(c *Config) { c.Cache.PromoteConsensus = 1 }},
		{"consensus 2", func(c *Config) { c.Cache.PromoteConsensus = 2 }},
		{"ttl 0", func(c *Config) { c.Cache.PromoteTTLSeconds = 0 }},
		{"reject -1", func(c *Config) { c.Cache.RejectLimit = -1 }},
	}
	for _, tc := range valid {
		t.Run("valid "+tc.name, func(t *testing.T) {
			c := base()
			tc.mut(c)
			if err := c.validate(); err != nil {
				t.Fatalf("unexpected: %v", err)
			}
		})
	}
	invalid := []struct{ name string; mut func(*Config) }{
		{"consensus 3", func(c *Config) { c.Cache.PromoteConsensus = 3 }},
		{"consensus -1", func(c *Config) { c.Cache.PromoteConsensus = -1 }},
		{"ttl negative", func(c *Config) { c.Cache.PromoteTTLSeconds = -1 }},
		{"reject -2", func(c *Config) { c.Cache.RejectLimit = -2 }},
	}
	for _, tc := range invalid {
		t.Run("invalid "+tc.name, func(t *testing.T) {
			c := base()
			tc.mut(c)
			if err := c.validate(); err == nil {
				t.Fatalf("must reject %s", tc.name)
			}
		})
	}
}
