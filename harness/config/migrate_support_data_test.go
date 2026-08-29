package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrateSupportDataDirDoesNotOverwriteExisting is the Issue #356
// regression: the directory-copy path used to recurse into copyFile with
// O_TRUNC and no existence check, silently overwriting newer user data under
// e.g. sessions/ or skills/ with the older legacy copy. Migration must keep the
// existing destination file, surface it as "kept existing", and still migrate
// non-colliding files.
func TestMigrateSupportDataDirDoesNotOverwriteExisting(t *testing.T) {
	legacy := t.TempDir()
	newDir := t.TempDir()

	write := func(root, rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(root, rel string) string {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	// Colliding file: legacy has OLD, destination already has NEWer user data.
	write(legacy, "sessions/foo.jsonl", "OLD")
	write(newDir, "sessions/foo.jsonl", "NEW")
	// Non-colliding legacy file that should migrate cleanly.
	write(legacy, "skills/brand-new.md", "S")

	warnings := migrateSupportData(legacy, newDir)

	if got := read(newDir, "sessions/foo.jsonl"); got != "NEW" {
		t.Fatalf("existing user file was overwritten: got %q, want %q", got, "NEW")
	}
	if got := read(newDir, "skills/brand-new.md"); got != "S" {
		t.Fatalf("non-colliding legacy file should have migrated: got %q", got)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "kept existing") {
		t.Fatalf("expected a kept-existing warning surfacing the skip, got: %v", warnings)
	}
}
