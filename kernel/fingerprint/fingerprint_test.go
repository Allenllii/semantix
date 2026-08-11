package fingerprint

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCaptureAndVerify(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module demo\n")
	write(t, dir, "pkg/a.go", "package a\n")
	deps, err := Capture(dir, []string{"go.mod", "pkg/a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 {
		t.Fatalf("deps = %d, want 2", len(deps))
	}
	changed, err := Verify(dir, deps)
	if err != nil || len(changed) != 0 {
		t.Fatalf("clean verify: changed=%v err=%v", changed, err)
	}

	// Content change is detected.
	write(t, dir, "go.mod", "module demo2\n")
	changed, _ = Verify(dir, deps)
	if len(changed) != 1 || changed[0] != "go.mod" {
		t.Fatalf("changed = %v, want [go.mod]", changed)
	}

	// Disappearing file is detected (go.mod is still "changed" from step 2 —
	// both are expected in the list).
	os.Remove(filepath.Join(dir, "pkg/a.go"))
	changed, _ = Verify(dir, deps)
	found := false
	for _, c := range changed {
		if c == "pkg/a.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("changed = %v, want it to include pkg/a.go", changed)
	}
}

func TestCaptureRejectsNonLocalPaths(t *testing.T) {
	for _, p := range []string{"/abs/path", "../up", "a/../../b"} {
		if _, err := Capture(t.TempDir(), []string{p}); err == nil {
			t.Errorf("path %q: want error", p)
		}
	}
}

func TestCaptureMissingFileErrors(t *testing.T) {
	if _, err := Capture(t.TempDir(), []string{"nope.txt"}); err == nil {
		t.Fatal("missing file must error (never silently trust partial capture)")
	}
}

func TestVerifyEmptyDepsClean(t *testing.T) {
	changed, err := Verify(t.TempDir(), nil)
	if err != nil || len(changed) != 0 {
		t.Fatalf("empty deps: changed=%v err=%v", changed, err)
	}
}
