package agent

import (
	"testing"

	"semantix/harness/diff"
	"semantix/harness/provider"
)

func TestFileDiffFromChangeKeepsBackendMetadata(t *testing.T) {
	change := diff.Build("internal/cache.go", "package cache\nold\n", "package cache\nnew\n", diff.Modify)
	got := fileDiffFromChange(change, "internal/cache.go")
	if got.Path != "internal/cache.go" || got.Status != "modified" || got.Language != "go" {
		t.Fatalf("metadata = %+v", got)
	}
	if got.Added != 1 || got.Removed != 1 || got.Hunks != 1 || len(got.Lines) == 0 {
		t.Fatalf("stats/rows = %+v", got)
	}
	if got.Lines[len(got.Lines)-2].Kind != "deleted" || got.Lines[len(got.Lines)-1].Kind != "added" {
		t.Fatalf("change rows = %+v", got.Lines)
	}
}

func TestFileDiffFromChangeUsesCreateAndDeleteStatuses(t *testing.T) {
	created := fileDiffFromChange(diff.Build("new.txt", "", "hello\n", diff.Create), "new.txt")
	if created.Status != "added" || created.Added != 1 {
		t.Fatalf("create = %+v", created)
	}
	deleted := fileDiffFromChange(diff.Build("gone.txt", "hello\n", "", diff.Delete), "gone.txt")
	if deleted.Status != "deleted" || deleted.Removed != 1 {
		t.Fatalf("delete = %+v", deleted)
	}
}

func TestFileDiffFromToolCallKeepsLegacyDiffUsable(t *testing.T) {
	got := fileDiffFromToolCall(provider.ToolCall{
		Arguments: `{"path":"pkg/main.go"}`,
		Diff:      "@@ -1 +1 @@\n-old\n+new\n",
		Added:     1,
		Removed:   1,
	})
	if got.Path != "pkg/main.go" || got.Status != "modified" || len(got.Lines) != 3 {
		t.Fatalf("legacy payload = %+v", got)
	}
}
