package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gateway.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validConfig = `
[server]
addr = ":8080"
gateway_key = "${GW_KEY}"

[store]
db = "~/.semantix/gw.jsonl"
scope = "project"
deps_root = "."

[retrieval]
retriever = "bm25"
top_k = 5
budget = 4096

[cache]
ttl_seconds = 86400

[ingest]
sessions_dir = "~/.semantix/sessions"
usage_log = "~/.semantix/gw-usage.jsonl"

[[upstreams]]
name = "deepseek"
base_url = "https://api.deepseek.com/v1"
api_key = "${DS_KEY}"
model_alias = ["deepseek-chat", "ds-chat"]
upstream_model = "deepseek-chat"
vendor = "deepseek"
`

func TestLoadExpandsEnvAndHome(t *testing.T) {
	t.Setenv("GW_KEY", "k1")
	t.Setenv("DS_KEY", "k2")
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.GatewayKey != "k1" || cfg.Upstreams[0].APIKey != "k2" {
		t.Errorf("env expansion failed: key=%q upkey=%q", cfg.Server.GatewayKey, cfg.Upstreams[0].APIKey)
	}
	home, _ := os.UserHomeDir()
	wantDB := filepath.Join(home, ".semantix", "gw.jsonl")
	if cfg.Store.DB != wantDB {
		t.Errorf("db = %q, want %q", cfg.Store.DB, wantDB)
	}
	if cfg.Upstreams[0].UpstreamModel != "deepseek-chat" || cfg.Upstreams[0].Vendor != "deepseek" {
		t.Errorf("upstream fields = %#v", cfg.Upstreams[0])
	}
}

func TestLoadFailsOnUnresolvedEnv(t *testing.T) {
	os.Unsetenv("GW_KEY")
	os.Unsetenv("DS_KEY")
	_, err := Load(writeConfig(t, validConfig))
	if err == nil || !strings.Contains(err.Error(), "GW_KEY") {
		t.Fatalf("want unresolved-env error naming GW_KEY, got %v", err)
	}
}

func TestLoadValidation(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(*Config)
		msg   string
	}{
		{"missing addr", func(c *Config) { c.Server.Addr = "" }, "addr"},
		{"missing key", func(c *Config) { c.Server.GatewayKey = "" }, "gateway_key"},
		{"missing db", func(c *Config) { c.Store.DB = "" }, "db"},
		{"no upstreams", func(c *Config) { c.Upstreams = nil }, "upstreams"},
		{"bad scope", func(c *Config) { c.Store.Scope = "all" }, "scope"},
		{"anthropic vendor", func(c *Config) { c.Upstreams[0].Vendor = "anthropic" }, "vendor"},
		{"dup alias", func(c *Config) {
			c.Upstreams = append(c.Upstreams, c.Upstreams[0])
			c.Upstreams[1].Name = "second"
		}, "alias"},
		{"missing api_key", func(c *Config) { c.Upstreams[0].APIKey = "" }, "api_key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultConfig()
			c.Server.GatewayKey = "k"
			c.Store.DB = "x.jsonl"
			c.Upstreams = []UpstreamConfig{{
				Name: "ds", BaseURL: "https://u/v1", APIKey: "k",
				ModelAlias: []string{"m"}, UpstreamModel: "m", Vendor: "deepseek",
			}}
			tc.mut(c)
			err := c.validate()
			if err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("want error containing %q, got %v", tc.msg, err)
			}
		})
	}
}

func TestModelListAndUpstreamFor(t *testing.T) {
	c := DefaultConfig()
	c.Upstreams = []UpstreamConfig{
		{Name: "a", ModelAlias: []string{"b-model", "a-model"}},
		{Name: "b", ModelAlias: []string{"c-model"}},
	}
	got := c.ModelList()
	want := []string{"a-model", "b-model", "c-model"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ModelList = %v, want %v", got, want)
	}
	if u, ok := c.UpstreamFor("b-model"); !ok || u.Name != "a" {
		t.Errorf("UpstreamFor(b-model) = %#v, %v", u, ok)
	}
	if _, ok := c.UpstreamFor("nope"); ok {
		t.Error("UpstreamFor(nope) must be false")
	}
}

// TestModelAliasEnvSubstitution: ${VAR} references inside model_alias must
// expand (regression: the old code took the address of a loop copy, making
// the substitution a silent no-op).
func TestModelAliasEnvSubstitution(t *testing.T) {
	t.Setenv("GW_KEY", "k1")
	t.Setenv("DS_KEY", "k2")
	t.Setenv("ALIAS_EXTRA", "ds-chat")
	body := strings.Replace(validConfig, `model_alias = ["deepseek-chat", "ds-chat"]`,
		`model_alias = ["deepseek-chat", "${ALIAS_EXTRA}"]`, 1)
	cfg, err := Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	aliases := cfg.Upstreams[0].ModelAlias
	if len(aliases) != 2 || aliases[1] != "ds-chat" {
		t.Fatalf("model_alias env substitution failed: %v", aliases)
	}
}

// TestHealthTimeoutValidation: health_timeout_seconds must be >= 0
// (0 disables the upstream probe, negatives are rejected).
func TestHealthTimeoutValidation(t *testing.T) {
	body := strings.Replace(validConfig, "gateway_key = \"${GW_KEY}\"",
		"gateway_key = \"${GW_KEY}\"\nhealth_timeout_seconds = -1", 1)
	t.Setenv("GW_KEY", "k1")
	if _, err := Load(writeConfig(t, body)); err == nil {
		t.Error("Load accepted health_timeout_seconds = -1, want error")
	}
}

// TestDeployConfigExampleLoads (issue #183 acceptance): the shipped
// deploy/semantix-gateway.toml.example must stay parseable by the loader —
// every ${VAR} it references resolves, and the decoded values match the
// documented contract. Guards the example against config drift.
func TestDeployConfigExampleLoads(t *testing.T) {
	path := filepath.Join("..", "deploy", "semantix-gateway.toml.example")
	t.Setenv("SEMANTIX_GATEWAY_KEY", "test-channel-key")
	t.Setenv("DEEPSEEK_API_KEY", "test-upstream-key")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	if cfg.Server.GatewayKey != "test-channel-key" {
		t.Errorf("gateway_key = %q, want env-resolved value", cfg.Server.GatewayKey)
	}
	if cfg.Server.HealthTimeoutSeconds != 3 {
		t.Errorf("health_timeout_seconds = %d, want 3", cfg.Server.HealthTimeoutSeconds)
	}
	if len(cfg.Upstreams) != 1 {
		t.Fatalf("upstreams = %d, want 1", len(cfg.Upstreams))
	}
	up := cfg.Upstreams[0]
	if up.Name != "deepseek" || up.APIKey != "test-upstream-key" || up.Vendor != "deepseek" {
		t.Errorf("upstream = %#v, want deepseek with env-resolved key", up)
	}
	if cfg.Store.DB != "/data/gateway.jsonl" || cfg.Ingest.SessionsDir != "/data/sessions" {
		t.Errorf("store/ingest paths not container-friendly: db=%q sessions=%q", cfg.Store.DB, cfg.Ingest.SessionsDir)
	}
}
