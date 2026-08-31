package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"semantix/harness/config"
	"semantix/harness/control"
)

func TestCurrentWorkspaceBranchReadsGitBranch(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "-C", root, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	if got := currentWorkspaceBranch(context.Background(), root); got != "master" && got != "main" {
		t.Fatalf("branch = %q, want the default branch name", got)
	}
}

func TestServeStatusUsesWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{WorkspaceRoot: root, SessionDir: sessions, Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["cwd"] != root || got["workspaceRoot"] != root {
		t.Fatalf("status workspace identity = %#v, want %q", got, root)
	}
	if _, ok := got["branch"].(string); !ok {
		t.Fatalf("status branch = %#v, want string", got["branch"])
	}
}
