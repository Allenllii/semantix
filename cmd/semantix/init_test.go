package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInitCreatesFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "semantix.toml")

	var stdout, stderr bytes.Buffer
	code := run([]string{"init", "--config", cfg}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("semantix.toml not created: %v", err)
	}
	gitkeep := filepath.Join(dir, ".semantix", ".gitkeep")
	if _, err := os.Stat(gitkeep); err != nil {
		t.Fatalf(".semantix/.gitkeep not created: %v", err)
	}
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "[store]") || !strings.Contains(string(b), "[project]") {
		t.Fatalf("template missing sections: %q", string(b))
	}
}

func TestRunInitRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "semantix.toml")
	if err := os.WriteFile(cfg, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"init", "--config", cfg}, &stdout, &stderr, dependencies{})
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("stderr = %q, want overwrite hint", stderr.String())
	}
	b, _ := os.ReadFile(cfg)
	if string(b) != "existing" {
		t.Fatalf("file was modified: %q", string(b))
	}
}

func TestRunInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "semantix.toml")
	if err := os.WriteFile(cfg, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"init", "--config", cfg, "--force"}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), "[store]") {
		t.Fatalf("template not written on --force: %q", string(b))
	}
}

func TestRunInitGitkeepIdempotent(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "semantix.toml")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "--config", cfg}, &stdout, &stderr, dependencies{}); code != 0 {
		t.Fatalf("first init code = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"init", "--config", cfg, "--force"}, &stdout, &stderr, dependencies{}); code != 0 {
		t.Fatalf("second init code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "skipped") || !strings.Contains(stdout.String(), ".gitkeep") {
		t.Fatalf("stdout = %q, want skipped .gitkeep", stdout.String())
	}
}

func TestRunInitJSON(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "semantix.toml")

	var stdout, stderr bytes.Buffer
	code := run([]string{"init", "--config", cfg, "--json"}, &stdout, &stderr, dependencies{})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Created []string `json:"created"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v (%q)", err, stdout.String())
	}
	if !env.OK || len(env.Data.Created) < 2 {
		t.Fatalf("envelope = %#v", env)
	}
}
