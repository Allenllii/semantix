package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestServeRejectsExtraArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"serve", "--addr", ":1", "extra"}, &stdout, &stderr, productionDependencies())
	if code != 2 {
		t.Fatalf("serve with positional args: code = %d, want 2; stderr %q", code, stderr.String())
	}
}

func TestServeUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"serve", "--bogus"}, &stdout, &stderr, productionDependencies())
	if code != 2 {
		t.Fatalf("serve --bogus: code = %d, want 2; stderr %q", code, stderr.String())
	}
}

// TestServeWithoutUpstreamsFails: a gateway with nothing to forward to must
// refuse to start (runtime error, not a silently empty service).
func TestServeWithoutUpstreamsFails(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "semantix-gateway.toml")
	if err := os.WriteFile(configPath, []byte("[server]\naddr = \":0\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEMANTIX_GATEWAY_CONFIG", configPath)
	var stdout, stderr bytes.Buffer
	code := run([]string{"serve"}, &stdout, &stderr, productionDependencies())
	if code != 1 {
		t.Fatalf("serve without upstreams: code = %d, want 1; stderr %q", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("no upstreams")) {
		t.Fatalf("stderr = %q, want no-upstreams diagnostic", stderr.String())
	}
}
