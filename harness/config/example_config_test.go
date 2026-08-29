package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExampleConfigDefaultsToGLM pins the shipped package behavior (B1): the
// config template a fresh download installs defaults to GLM and keeps DeepSeek
// available so the user can switch. It loads the real semantix-agent.example.toml so
// the file and the loader can never drift apart silently.
func TestExampleConfigDefaultsToGLM(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "semantix-agent.example.toml"))
	if err != nil {
		t.Fatalf("read example config: %v", err)
	}

	project := t.TempDir()
	home := t.TempDir()
	launch := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "semantix-agent.toml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(launch)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatalf("load example config: %v", err)
	}

	if cfg.DefaultModel != "glm" {
		t.Fatalf("default_model = %q, want glm", cfg.DefaultModel)
	}
	if _, ok := cfg.ResolveModel(cfg.DefaultModel); !ok {
		t.Fatalf("default_model %q does not resolve to any provider/model", cfg.DefaultModel)
	}
	glm, ok := cfg.Provider("glm")
	if !ok {
		t.Fatalf("glm provider missing: %+v", cfg.Providers)
	}
	if glm.APIKeyEnv != "GLM_API_KEY" || glm.Kind != "openai" {
		t.Fatalf("glm provider = %+v, want openai / GLM_API_KEY", glm)
	}
	// DeepSeek must remain so the user can switch by editing default_model.
	if _, ok := cfg.Provider("deepseek"); !ok {
		t.Fatalf("deepseek provider must remain available to switch to: %+v", cfg.Providers)
	}
}
