package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

func TestSubstituteEnv(t *testing.T) {
	lookup := func(name string) (string, bool, error) {
		if name == "SET" {
			return "value-1", true, nil
		}
		return "", false, nil
	}
	got, err := substituteEnvFunc("a ${SET} b ${MISSING}", lookup)
	if err == nil || !strings.Contains(err.Error(), "${MISSING}") {
		t.Fatalf("unresolved placeholder must error, got %q, %v", got, err)
	}
	got, err = substituteEnvFunc("a ${SET} b", lookup)
	if err != nil || got != "a value-1 b" {
		t.Fatalf("substituteEnvFunc = %q, %v", got, err)
	}
	// Literal-looking text without placeholders is untouched.
	got, _ = substituteEnvFunc("no placeholders", lookup)
	if got != "no placeholders" {
		t.Fatalf("plain text mutated: %q", got)
	}
	// Unclosed placeholder stays literal (no false failure).
	got, err = substituteEnvFunc("${UNCLOSED", lookup)
	if err != nil || got != "${UNCLOSED" {
		t.Fatalf("unclosed placeholder: %q, %v", got, err)
	}
}

func TestLoadConfigFileWithEnvSubstitution(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "semantix-gateway.toml")
	content := `
[server]
addr = ":9090"
gateway_key = "${TEST_GATEWAY_KEY}"

[store]
db = "~/.semantix/gateway.jsonl"
scope = "user"

[retrieval]
top_k = 7
budget = 2048

[cache]
ttl_seconds = 3600

[ingest]
sessions_dir = "~/sessions"

[[upstreams]]
name = "ds"
base_url = "https://api.deepseek.com/v1"
api_key = "${TEST_UPSTREAM_KEY}"
model_alias = ["deepseek-chat", "ds-chat"]
upstream_model = "deepseek-chat"
vendor = "deepseek"
`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_GATEWAY_KEY", "gw-secret")
	t.Setenv("TEST_UPSTREAM_KEY", "up-secret")
	home := t.TempDir()
	t.Setenv("USERPROFILE", home) // Windows home for ~ expansion
	if err := os.MkdirAll(filepath.Join(home, ".semantix"), 0o700); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Options{ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9090" {
		t.Errorf("addr = %q", cfg.Addr)
	}
	if cfg.GatewayKey != "gw-secret" {
		t.Errorf("gateway key not env-substituted: %q", cfg.GatewayKey)
	}
	if cfg.Scope != slice.User {
		t.Errorf("scope = %v", cfg.Scope)
	}
	if cfg.Retrieval.TopK != 7 || cfg.Retrieval.Budget != 2048 {
		t.Errorf("retrieval = %+v", cfg.Retrieval)
	}
	if cfg.Cache.TTLSeconds != 3600 {
		t.Errorf("ttl = %d", cfg.Cache.TTLSeconds)
	}
	if cfg.Ingest.SessionsDir != filepath.Join(home, "sessions") {
		t.Errorf("sessions dir = %q", cfg.Ingest.SessionsDir)
	}
	if !strings.HasPrefix(cfg.StoreDB, home) {
		t.Errorf("store db not home-expanded: %q", cfg.StoreDB)
	}
	if len(cfg.Upstreams) != 1 {
		t.Fatalf("upstreams = %d", len(cfg.Upstreams))
	}
	up := cfg.Upstreams[0]
	if up.APIKey != "up-secret" || up.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("upstream = %+v", up)
	}
	if len(up.ModelAlias) != 2 || up.UpstreamModel != "deepseek-chat" || up.Vendor != "deepseek" {
		t.Errorf("upstream fields = %+v", up)
	}
}

func TestLoadUnresolvedPlaceholderFails(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "semantix-gateway.toml")
	content := "[server]\ngateway_key = \"${NOT_SET_ANYWHERE}\"\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(Options{ConfigPath: configPath}); err == nil ||
		!strings.Contains(err.Error(), "${NOT_SET_ANYWHERE}") {
		t.Fatalf("unresolved placeholder must fail startup, got %v", err)
	}
}

func TestLoadL3SafeRequiresDepsRoot(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "semantix-gateway.toml")
	content := "[cache]\nl3_safe = true\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(Options{ConfigPath: configPath}); err == nil ||
		!strings.Contains(err.Error(), "deps_root") {
		t.Fatalf("l3_safe without deps_root must fail, got %v", err)
	}
}

func TestLoadEnvOverridesAndDefaults(t *testing.T) {
	t.Setenv("SEMANTIX_GATEWAY_ADDR", ":7777")
	t.Setenv("SEMANTIX_GATEWAY_DB", filepath.Join(t.TempDir(), "gw.db"))
	cfg, err := Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":7777" {
		t.Errorf("env addr not applied: %q", cfg.Addr)
	}
	if cfg.Scope != slice.Project {
		t.Errorf("default scope = %v", cfg.Scope)
	}
	if cfg.Retrieval.TopK != 5 || cfg.Retrieval.Budget != 4096 {
		t.Errorf("default retrieval = %+v", cfg.Retrieval)
	}
	if cfg.Cache.TTLSeconds != 86400 {
		t.Errorf("default ttl = %d", cfg.Cache.TTLSeconds)
	}
	if !cfg.Disable {
		// explicit flag override path
		if cfg2, err := Load(Options{Disable: true}); err != nil || !cfg2.Disable {
			t.Fatalf("flag disable not applied: %v", err)
		}
	}
	if cfg.Cache.L3Safe {
		t.Errorf("l3_safe must default false (fail-closed)")
	}
}

func TestLoadValidation(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "bad.toml")
	content := `
[[upstreams]]
name = "no-base"
model_alias = ["x"]
api_key = "k"
`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(Options{ConfigPath: configPath}); err == nil ||
		!strings.Contains(err.Error(), "base_url") {
		t.Fatalf("upstream without base_url must fail, got %v", err)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := expandHome("~/x/y"); got != filepath.Join(home, "x", "y") {
		t.Errorf("expandHome = %q", got)
	}
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("absolute path mutated: %q", got)
	}
	if got := expandHome(""); got != "" {
		t.Errorf("empty path mutated: %q", got)
	}
}

func TestUpstreamSetResolve(t *testing.T) {
	s := newUpstreamSet([]Upstream{
		{Name: "ds", ModelAlias: []string{"deepseek-chat", "ds-chat"}, UpstreamModel: "deepseek-chat"},
		{Name: "claude", ModelAlias: []string{"claude-sonnet"}, UpstreamModel: "claude-sonnet-4-20250514"},
	})
	if up, ok := s.resolve("ds-chat"); !ok || up.Name != "ds" {
		t.Fatalf("alias resolve failed: %+v %v", up, ok)
	}
	if _, ok := s.resolve("nope"); ok {
		t.Fatal("unknown model must not resolve")
	}
	if got := s.upstreamModel("claude-sonnet"); got != "claude-sonnet-4-20250514" {
		t.Errorf("upstream model mapping = %q", got)
	}
	if got := s.upstreamModel("deepseek-chat"); got != "deepseek-chat" {
		t.Errorf("passthrough model = %q", got)
	}
	aliases := s.aliases()
	if len(aliases) != 3 {
		t.Fatalf("aliases = %v", aliases)
	}
	for i := 1; i < len(aliases); i++ {
		if aliases[i-1] >= aliases[i] {
			t.Fatalf("aliases not sorted: %v", aliases)
		}
	}
}
