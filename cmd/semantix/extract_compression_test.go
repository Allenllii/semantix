package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

func TestExtractReportsAggregateCompression(t *testing.T) {
	input := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(input, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	extractor := &fakeExtractor{items: []*slice.Slice{
		{
			ID:      "compressed",
			Content: []byte("short"),
			Meta: slice.SliceMeta{
				CompressionVersion: "rules-v1",
				OriginalBytes:      10,
				StoredBytes:        5,
			},
		},
		{ID: "legacy", Content: []byte("legacy")},
		{
			ID:      "filtered-context",
			Type:    slice.Context,
			Content: []byte("x"),
			Meta: slice.SliceMeta{
				CompressionVersion: "rules-v1",
				OriginalBytes:      100,
				StoredBytes:        1,
			},
		},
	}}
	store := newFakeStore()
	deps := dependencies{
		newExtractor: func() slice.Extractor { return extractor },
		openStore:    func(string) (slice.Store, error) { return store, nil },
		newIndex:     func() slice.Index { return bm25.New() },
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"extract", "--input", input, "--scope", "user", "--db", filepath.Join(t.TempDir(), "slices.db")}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	for _, field := range []string{"raw_bytes=16", "stored_bytes=11", "compression_ratio=0.3125"} {
		if !strings.Contains(stdout.String(), field) {
			t.Fatalf("stdout = %q, missing %q", stdout.String(), field)
		}
	}
	if store.items["filtered-context"] != nil {
		t.Fatal("filtered Context slice was unexpectedly stored")
	}
}

func TestExtractionCompressionRejectsInconsistentMetadata(t *testing.T) {
	items := []*slice.Slice{{
		Content: []byte("content"),
		Meta: slice.SliceMeta{
			CompressionVersion: "rules-v1",
			OriginalBytes:      100,
			StoredBytes:        3,
		},
	}}
	raw, stored, ratio := extractionCompression(items)
	if raw != 7 || stored != 7 || ratio != 0 {
		t.Fatalf("stats = raw %d stored %d ratio %.4f, want 7/7/0", raw, stored, ratio)
	}
}
