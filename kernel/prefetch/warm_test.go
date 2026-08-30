package prefetch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/embed"
)

// Research plan W5: real-resource warm-up kinds execute through the Runner
// but persist nothing — a warmed embedding or a page-cached file has no
// reuse content of its own.
func TestRunnerWarmKindsExecuteWithoutPersisting(t *testing.T) {
	st := newTestStore(t, filepath.Join(t.TempDir(), "slices.jsonl"))
	r := &Runner{Store: st, Executor: EmbeddingWarmer{Embedder: embed.HashEmbedder{Dim: 8}}}
	ids, err := r.Run(context.Background(), []PrefetchTask{
		{Kind: "embedding", Key: "predicted next tool query", Cost: 10, Locality: LocalityLocal},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("warm kind persisted %d slices, want 0", len(ids))
	}
	items, err := st.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("store has %d slices after a warm-only run, want 0", len(items))
	}
	// Egress red line still applies to warm kinds.
	r2 := &Runner{Store: st, Executor: EmbeddingWarmer{Embedder: embed.HashEmbedder{Dim: 8}}}
	if _, err := r2.Run(context.Background(), []PrefetchTask{{Kind: "embedding", Key: "x", Locality: LocalityEgress}}); err != nil {
		t.Fatal(err)
	}
	if r2.BlockedEgress.Load() != 1 {
		t.Fatalf("BlockedEgress = %d, want 1", r2.BlockedEgress.Load())
	}
	// An operator can opt a kind back into persistence.
	r3 := &Runner{Store: st, Executor: &fakeExecutor{results: map[string][]byte{"k": []byte("c")}}, WarmKinds: map[string]bool{}}
	ids, err = r3.Run(context.Background(), []PrefetchTask{{Kind: "embedding", Key: "k", Locality: LocalityLocal}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Fatalf("persisting opt-out returned %d ids, want 1", len(ids))
	}
}

func TestFileWarmerReadsInsideRootAndRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := FileWarmer{Root: root}
	if _, err := w.Execute(context.Background(), PrefetchTask{Kind: "file", Key: "a.txt", Locality: LocalityLocal}); err != nil {
		t.Fatalf("in-root read failed: %v", err)
	}
	for _, key := range []string{"../outside.txt", "/etc/passwd", "sub/../../etc/passwd"} {
		if _, err := w.Execute(context.Background(), PrefetchTask{Kind: "file", Key: key, Locality: LocalityLocal}); err == nil {
			t.Fatalf("key %q must be rejected", key)
		} else if !strings.Contains(err.Error(), "escapes root") && !strings.Contains(err.Error(), "file-warm") {
			t.Fatalf("key %q rejected with unexpected error: %v", key, err)
		}
	}
	if _, err := w.Execute(context.Background(), PrefetchTask{Kind: "file", Key: "missing.txt", Locality: LocalityLocal}); err == nil {
		t.Fatal("missing file must error")
	}
}

// KindRouter dispatches by task kind so one Runner can drive the assembler
// and the warm executors together.
type KindRouter struct {
	ByKind map[string]Executor
}

func (r KindRouter) Execute(ctx context.Context, t PrefetchTask) ([]byte, error) {
	e, ok := r.ByKind[t.Kind]
	if !ok {
		return nil, nil // unknown kind: nothing to do, not an error
	}
	return e.Execute(ctx, t)
}

func TestKindRouterDrivesAssemblerAndWarmers(t *testing.T) {
	st := newTestStore(t, filepath.Join(t.TempDir(), "slices.jsonl"))
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ix := bm25.New()
	seedIndexedSlice(t, st, ix, "s1", "readFile usage documentation")
	r := &Runner{Store: st, Executor: KindRouter{ByKind: map[string]Executor{
		"slice-assembly": &SliceAssembler{Index: ix, K: 2},
		"embedding":      EmbeddingWarmer{Embedder: embed.HashEmbedder{Dim: 8}},
		"file":           FileWarmer{Root: root},
	}}}
	// Warm kinds persist nothing; the assembler kind would persist but is
	// not exercised here — assert the mixed plan runs clean.
	ids, err := r.Run(context.Background(), []PrefetchTask{
		{Kind: "embedding", Key: "q", Cost: 5, Locality: LocalityLocal},
		{Kind: "file", Key: "b.go", Cost: 5, Locality: LocalityLocal},
	})
	if err != nil || len(ids) != 0 {
		t.Fatalf("warm tasks: ids=%v err=%v, want none", ids, err)
	}
}
