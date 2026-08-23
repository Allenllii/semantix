package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRecorderAppendAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	r, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Trust("s-1", "import", "user-curated"); err != nil {
		t.Fatal(err)
	}
	if err := r.Origin("s-2", "session-auto", "ingest"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	var first map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first["action"] != "slice_trust" || first["slice_id"] != "s-1" ||
		first["from_origin"] != "import" || first["to_origin"] != "user-curated" || first["at"] == "" {
		t.Fatalf("trust line = %v", first)
	}
	var second map[string]string
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	if second["action"] != "slice_origin" || second["origin"] != "session-auto" || second["channel"] != "ingest" {
		t.Fatalf("origin line = %v", second)
	}
}

func TestRecorderNilSafeAndBestEffort(t *testing.T) {
	var r *Recorder
	if err := r.Trust("s", "import", "user-curated"); err != nil {
		t.Fatalf("nil recorder must be a no-op, got %v", err)
	}
	if err := r.Record("x", nil); err != nil {
		t.Fatalf("nil recorder record must be a no-op, got %v", err)
	}
}

func TestRecorderPerm0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are not enforced on Windows")
	}
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	if _, err := NewRecorder(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("audit log perm = %o, want 600", perm)
	}
}
