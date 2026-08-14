package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunConfigDefaults(t *testing.T) {
	t.Chdir(t.TempDir()) // default ./semantix.toml absent → all defaults

	var stdout, stderr bytes.Buffer
	code := run([]string{"config"}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "store.db = .semantix/project.db\t# source=default") {
		t.Fatalf("stdout = %q, want default store.db", out)
	}
	if !strings.Contains(out, "project.name = my-project\t# source=default") {
		t.Fatalf("stdout = %q, want default project.name", out)
	}
}

func TestRunConfigFileSources(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "semantix.toml")
	if err := os.WriteFile(cfg, []byte("[store]\ndb = \"/tmp/custom.db\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "--config", cfg}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "store.db = /tmp/custom.db\t# source=file") {
		t.Fatalf("stdout = %q, want file source for store.db", out)
	}
	if !strings.Contains(out, "store.scope = project\t# source=default") {
		t.Fatalf("stdout = %q, want default source for store.scope", out)
	}
}

func TestRunConfigFlagOverride(t *testing.T) {
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "--db", "/flag.db"}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "store.db = /flag.db\t# source=flag") {
		t.Fatalf("stdout = %q, want flag source", stdout.String())
	}
}

func TestRunConfigEnvOverride(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SEMANTIX_DB", "/env.db")

	var stdout, stderr bytes.Buffer
	code := run([]string{"config"}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "store.db = /env.db\t# source=env") {
		t.Fatalf("stdout = %q, want env source", stdout.String())
	}
}

func TestRunConfigJSON(t *testing.T) {
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "--json"}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var env struct {
		OK      bool   `json:"ok"`
		Command string `json:"command"`
		Data    map[string]struct {
			Value  any    `json:"value"`
			Source string `json:"source"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v (%q)", err, stdout.String())
	}
	if !env.OK || env.Command != "config" {
		t.Fatalf("envelope = %#v", env)
	}
	if e, ok := env.Data["store.db"]; !ok || e.Source != "default" {
		t.Fatalf("data[store.db] = %#v, want default source", e)
	}
}

func TestRunConfigInvalidContent(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "semantix.toml")
	if err := os.WriteFile(cfg, []byte("[store]\nscope = \"team\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "--config", cfg}, &stdout, &stderr, dependencies{})
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "store.scope") {
		t.Fatalf("stderr = %q, want field location", stderr.String())
	}
}

func TestRunConfigMissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "--config", filepath.Join(t.TempDir(), "nope.toml")}, &stdout, &stderr, dependencies{})
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr = %q", code, stderr.String())
	}
}

func TestRunConfigHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "--help"}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
}
