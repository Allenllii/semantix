package control

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemanticReviewAppliesOnlyToBoundaryTasks(t *testing.T) {
	if semanticReviewApplies("fix a local CSV parser and run tests", "") {
		t.Fatal("local task should skip semantic review")
	}
	if !semanticReviewApplies("delete_cookie must preserve SameSite and security invariants", "") {
		t.Fatal("cookie boundary task should use semantic review")
	}
	if !semanticReviewApplies("fix the bug", "diff --git a/auth/middleware.go b/auth/middleware.go\n+ check authorization") {
		t.Fatal("security-sensitive actual diff should use semantic review")
	}
	if semanticReviewApplies("fix the bug", "diff --git a/parser.py b/parser.py\n+ return value.strip()") {
		t.Fatal("small local actual diff should skip semantic review")
	}
}

func TestSemanticReviewDiffIncludesStagedChanges(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	path := filepath.Join(root, "auth.go")
	if err := os.WriteFile(path, []byte("package auth\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "auth.go")
	run("commit", "-m", "base")
	if err := os.WriteFile(path, []byte("package auth\n// authorization check\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "auth.go")
	if got := semanticReviewDiff(context.Background(), root); !strings.Contains(got, "authorization check") {
		t.Fatalf("staged diff missing from semantic review evidence: %q", got)
	}
}

func TestSemanticReviewReceiptsAreBounded(t *testing.T) {
	got := semanticReviewReceipts("tests passed\nplain line\n测试通过\nPASS focused\ntest 4\ntest 5\ntest 6\ntest 7\ntest 8\ntest 9")
	if len(got) != 6 {
		t.Fatalf("receipts=%d, want 6", len(got))
	}
}
