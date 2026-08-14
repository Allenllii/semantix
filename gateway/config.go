// Package gateway implements the Semantix Gateway (Issue #133): an
// OpenAI-compatible HTTP layer that sits in front of upstream LLM providers
// (typically behind a New API relay channel) and runs every request through
// the kernel's three-layer semantic cache before forwarding:
//
//   - L3: verified-result reuse (kernel/cache + kernel/fingerprint) — a hit
//     returns the cached response with zero upstream calls;
//   - L2: semantic-slice injection (kernel/inject) — on a miss, relevant
//     stored slices are appended to the system prompt so the model skips
//     repeated exploration;
//   - L1: byte-stable injection blocks keep the upstream prefix-cache warm.
//
// The gateway reuses kernel packages only (cache/inject/fingerprint/judge/
// promote/slice/ingest/usage); kernel never imports gateway. Deployment form:
// `semantix serve` (a long-running process), configurable via
// semantix-gateway.toml + SEMANTIX_GATEWAY_* env vars.
package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"semantix/kernel/slice"
)

// Config is the effective gateway configuration after
// flags > env > semantix-gateway.toml > built-in defaults.
type Config struct {
	Addr       string
	GatewayKey string
	// StoreDB is the slice store path (JSONL). The L3 cache shares this
	// store (design decision D4: L3 entries are Result slices).
	StoreDB string
	Scope   slice.Scope
	// Disable makes the gateway a pure pass-through (ablation switch,
	// design §3.3): no L3 lookup, no L2 injection.
	Disable bool

	Retrieval struct {
		TopK   int
		Budget int
	}
	Cache struct {
		// TTLSeconds bounds how old a cached L3 entry may be (0 = never).
		TTLSeconds int64
		// L3Safe opts into L3 write-back for gateway-produced responses.
		// DepsRoot must also be set: entries are captured with a
		// fingerprint of the configured project root, so any file change
		// invalidates them (design §3.5: dep-less entries never enter L3).
		L3Safe   bool
		DepsRoot string
	}
	Ingest struct {
		// SessionsDir is where request/response pairs are bypassed to
		// session JSONL files (design §3.7). Empty disables write-memory.
		SessionsDir string
		// Extract runs the ingest pipeline on bypassed session files
		// (default true when SessionsDir is set).
		Extract bool
	}
	// UsageDB records per-request usage events (kernel/usage.Recorder).
	// Empty disables recording.
	UsageDB string

	Upstreams []Upstream
}

// Upstream is one configured upstream LLM endpoint (design §3.8).
type Upstream struct {
	// Name is the upstream identifier (channel name on the New API side).
	Name string
	// BaseURL is the OpenAI-compatible upstream base, e.g.
	// https://api.deepseek.com/v1
	BaseURL string
	// APIKey is the upstream bearer key (from env, never the config file
	// itself).
	APIKey string
	// ModelAlias lists the client-visible model names routed here
	// (design §4.2: New API channel model names).
	ModelAlias []string
	// UpstreamModel is the real model name sent upstream (design §4.2
	// mapping target). Empty means the client model name passes through.
	UpstreamModel string
	// Vendor classifies the provider (design §3.8): deepseek | openai |
	// moonshot | anthropic. Reserved for vendor-specific behavior.
	Vendor string
	// TimeoutSec bounds a single upstream round trip (default 60).
	TimeoutSec int
}

// Options carries explicit flag-level overrides (highest precedence).
type Options struct {
	ConfigPath string
	Addr       string
	GatewayKey string
	StoreDB    string
	Scope      string
	Disable    bool
}

// fileConfig mirrors semantix-gateway.toml (design §3.9).
type fileConfig struct {
	Server struct {
		Addr       string `toml:"addr"`
		GatewayKey string `toml:"gateway_key"`
	} `toml:"server"`
	Store struct {
		DB    string `toml:"db"`
		Scope string `toml:"scope"`
	} `toml:"store"`
	Retrieval struct {
		TopK   int `toml:"top_k"`
		Budget int `toml:"budget"`
	} `toml:"retrieval"`
	Cache struct {
		TTLSeconds int64  `toml:"ttl_seconds"`
		L3Safe     bool   `toml:"l3_safe"`
		DepsRoot   string `toml:"deps_root"`
	} `toml:"cache"`
	Ingest struct {
		SessionsDir string `toml:"sessions_dir"`
		Extract     *bool  `toml:"extract"`
	} `toml:"ingest"`
	Usage struct {
		DB string `toml:"db"`
	} `toml:"usage"`
	Upstreams []struct {
		Name           string   `toml:"name"`
		BaseURL        string   `toml:"base_url"`
		APIKey         string   `toml:"api_key"`
		ModelAlias     []string `toml:"model_alias"`
		UpstreamModel  string   `toml:"upstream_model"`
		Vendor         string   `toml:"vendor"`
		TimeoutSeconds int      `toml:"timeout_seconds"`
	} `toml:"upstreams"`
}

// DefaultConfig returns the built-in defaults.
func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	db := filepath.Join(home, ".semantix", "gateway.jsonl")
	if home == "" {
		db = ".semantix/gateway.jsonl"
	}
	c := &Config{
		// Loopback by default: the gateway carries API keys and must sit
		// behind New API (compose internal network) — a wildcard bind is
		// an explicit operator choice.
		Addr:      "127.0.0.1:8080",
		StoreDB:   db,
		Scope:     slice.Project,
		Retrieval: struct{ TopK, Budget int }{TopK: 5, Budget: 4096},
		Cache: struct {
			TTLSeconds int64
			L3Safe     bool
			DepsRoot   string
		}{TTLSeconds: 86400},
		Ingest: struct {
			SessionsDir string
			Extract     bool
		}{},
	}
	c.Ingest.Extract = true
	return c
}

// substituteEnv expands ${VAR} in the raw TOML text. An unset variable is
// an error (fail fast, never leak the literal placeholder onward).
func substituteEnv(text string) (string, error) {
	return substituteEnvFunc(text, func(name string) (string, bool, error) {
		v, ok := os.LookupEnv(name)
		if !ok {
			return "", false, fmt.Errorf("config: ${%s} is not set (refusing to start with a literal placeholder)", name)
		}
		return v, true, nil
	})
}

// substituteEnvFunc is the testable core of substituteEnv.
func substituteEnvFunc(text string, lookup func(string) (string, bool, error)) (string, error) {
	var b strings.Builder
	for {
		start := strings.Index(text, "${")
		if start < 0 {
			b.WriteString(text)
			return b.String(), nil
		}
		end := strings.Index(text[start:], "}")
		if end < 0 {
			// Unclosed placeholder: treat as literal text.
			b.WriteString(text)
			return b.String(), nil
		}
		end += start
		name := text[start+2 : end]
		if !validEnvName(name) {
			b.WriteString(text[:end+1])
			text = text[end+1:]
			continue
		}
		b.WriteString(text[:start])
		v, ok, err := lookup(name)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("config: ${%s} is not set", name)
		}
		b.WriteString(v)
		text = text[end+1:]
	}
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// expandHome expands a leading ~ to the user home directory.
func expandHome(p string) string {
	if p == "" || p == "~" {
		if p == "~" {
			if home, err := os.UserHomeDir(); err == nil {
				return home
			}
		}
		return p
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// Load resolves the effective configuration.
func Load(opts Options) (*Config, error) {
	cfg := DefaultConfig()

	path := opts.ConfigPath
	explicit := path != ""
	if !explicit {
		if p := os.Getenv("SEMANTIX_GATEWAY_CONFIG"); p != "" {
			path = p
			explicit = true
		}
	}
	if path == "" {
		path = "semantix-gateway.toml"
	}

	if b, err := os.ReadFile(path); err == nil {
		text, err := substituteEnv(string(b))
		if err != nil {
			return nil, err
		}
		var fc fileConfig
		if _, err := toml.Decode(text, &fc); err != nil {
			return nil, fmt.Errorf("config: parse %s: %w", path, err)
		}
		if fc.Server.Addr != "" {
			cfg.Addr = fc.Server.Addr
		}
		if fc.Server.GatewayKey != "" {
			cfg.GatewayKey = fc.Server.GatewayKey
		}
		if fc.Store.DB != "" {
			cfg.StoreDB = fc.Store.DB
		}
		if fc.Store.Scope != "" {
			sc, err := parseScopeValue(fc.Store.Scope)
			if err != nil {
				return nil, fmt.Errorf("config: %w", err)
			}
			cfg.Scope = sc
		}
		if fc.Retrieval.TopK > 0 {
			cfg.Retrieval.TopK = fc.Retrieval.TopK
		}
		if fc.Retrieval.Budget > 0 {
			cfg.Retrieval.Budget = fc.Retrieval.Budget
		}
		if fc.Cache.TTLSeconds > 0 {
			cfg.Cache.TTLSeconds = fc.Cache.TTLSeconds
		}
		if fc.Cache.L3Safe {
			cfg.Cache.L3Safe = true
		}
		if fc.Cache.DepsRoot != "" {
			cfg.Cache.DepsRoot = fc.Cache.DepsRoot
		}
		if fc.Ingest.SessionsDir != "" {
			cfg.Ingest.SessionsDir = fc.Ingest.SessionsDir
		}
		if fc.Ingest.Extract != nil {
			cfg.Ingest.Extract = *fc.Ingest.Extract
		}
		if fc.Usage.DB != "" {
			cfg.UsageDB = fc.Usage.DB
		}
		for _, u := range fc.Upstreams {
			cfg.Upstreams = append(cfg.Upstreams, Upstream{
				Name:          u.Name,
				BaseURL:       u.BaseURL,
				APIKey:        u.APIKey,
				ModelAlias:    u.ModelAlias,
				UpstreamModel: u.UpstreamModel,
				Vendor:        u.Vendor,
				TimeoutSec:    u.TimeoutSeconds,
			})
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	} else if explicit {
		return nil, fmt.Errorf("config: file %s does not exist", path)
	}

	// Env layer (SEMANTIX_GATEWAY_*). Invalid values are hard errors
	// (a typo must surface at startup, not silently fall back to defaults).
	applyEnv := func(name string, apply func(string) error) error {
		if v, ok := os.LookupEnv(name); ok && v != "" {
			return apply(v)
		}
		return nil
	}
	if err := applyEnv("SEMANTIX_GATEWAY_ADDR", func(v string) error { cfg.Addr = v; return nil }); err != nil {
		return nil, err
	}
	if err := applyEnv("SEMANTIX_GATEWAY_KEY", func(v string) error { cfg.GatewayKey = v; return nil }); err != nil {
		return nil, err
	}
	if err := applyEnv("SEMANTIX_GATEWAY_DB", func(v string) error { cfg.StoreDB = v; return nil }); err != nil {
		return nil, err
	}
	if err := applyEnv("SEMANTIX_GATEWAY_SCOPE", func(v string) error {
		sc, err := parseScopeValue(v)
		if err != nil {
			return fmt.Errorf("config: SEMANTIX_GATEWAY_SCOPE: %w", err)
		}
		cfg.Scope = sc
		return nil
	}); err != nil {
		return nil, err
	}
	if err := applyEnv("SEMANTIX_GATEWAY_SESSIONS_DIR", func(v string) error { cfg.Ingest.SessionsDir = v; return nil }); err != nil {
		return nil, err
	}
	if err := applyEnv("SEMANTIX_GATEWAY_DEPS_ROOT", func(v string) error { cfg.Cache.DepsRoot = v; return nil }); err != nil {
		return nil, err
	}
	if err := applyEnv("SEMANTIX_GATEWAY_USAGE_DB", func(v string) error { cfg.UsageDB = v; return nil }); err != nil {
		return nil, err
	}
	if v := os.Getenv("SEMANTIX_GATEWAY_DISABLE"); v == "1" || strings.EqualFold(v, "true") {
		cfg.Disable = true
	}
	if v := os.Getenv("SEMANTIX_GATEWAY_L3_SAFE"); v == "1" || strings.EqualFold(v, "true") {
		cfg.Cache.L3Safe = true
	}

	// Flag layer (highest).
	if opts.Addr != "" {
		cfg.Addr = opts.Addr
	}
	if opts.GatewayKey != "" {
		cfg.GatewayKey = opts.GatewayKey
	}
	if opts.StoreDB != "" {
		cfg.StoreDB = opts.StoreDB
	}
	if opts.Scope != "" {
		sc, err := parseScopeValue(opts.Scope)
		if err != nil {
			return nil, err
		}
		cfg.Scope = sc
	}
	if opts.Disable {
		cfg.Disable = true
	}

	// Expand ~ in path fields.
	cfg.StoreDB = expandHome(cfg.StoreDB)
	cfg.Cache.DepsRoot = expandHome(cfg.Cache.DepsRoot)
	cfg.Ingest.SessionsDir = expandHome(cfg.Ingest.SessionsDir)
	cfg.UsageDB = expandHome(cfg.UsageDB)

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseScopeValue(v string) (slice.Scope, error) {
	switch v {
	case "session":
		return slice.Session, nil
	case "project":
		return slice.Project, nil
	case "user":
		return slice.User, nil
	default:
		return 0, fmt.Errorf("invalid scope %q (want session|project|user)", v)
	}
}

func (c *Config) validate() error {
	if c.Addr == "" {
		return fmt.Errorf("config: server.addr must not be empty")
	}
	if c.Retrieval.TopK <= 0 || c.Retrieval.Budget <= 0 {
		return fmt.Errorf("config: retrieval top_k/budget must be > 0")
	}
	if c.Cache.L3Safe && c.Cache.DepsRoot == "" {
		return fmt.Errorf("config: cache.l3_safe requires cache.deps_root (design §3.5: dep-less entries never enter L3)")
	}
	for i, u := range c.Upstreams {
		if u.BaseURL == "" {
			return fmt.Errorf("config: upstreams[%d]: base_url required", i)
		}
		if len(u.ModelAlias) == 0 {
			return fmt.Errorf("config: upstreams[%d]: at least one model_alias required", i)
		}
		if u.APIKey == "" {
			return fmt.Errorf("config: upstreams[%d]: api_key required (env-injected)", i)
		}
	}
	return nil
}
