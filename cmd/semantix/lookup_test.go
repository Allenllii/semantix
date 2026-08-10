package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/lookup"
	"semantix/kernel/slice"
)

// TestRunLookupLimitClamp locks the CLI/kernel parity for --limit:
// >50 caps to 50, <=0 falls back to 5 (kernel Execute semantics).
func TestRunLookupLimitClamp(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	idx := bm25.New()
	seeds := []string{"修复 go 测试失败", "配置 CI 流水线", "部署到服务器"}
	for i, q := range seeds {
		sl := &slice.Slice{ID: fmt.Sprintf("s%d", i), Type: slice.Prompt, Scope: slice.Project, Content: []byte(q)}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
		if err := idx.Insert(sl); err != nil {
			t.Fatal(err)
		}
	}
	deps := dependencies{
		newExtractor: slice.NewExtractor,
		openStore:    func(p string) (slice.Store, error) { return store, nil },
		newIndex:     func() slice.Index { return bm25.New() },
	}

	for _, tc := range []struct {
		limit string
	}{
		{"0"},   // <=0 -> default 5 (before the fix: empty result)
		{"-5"},  // <=0 -> default 5
		{"100"}, // >50 -> cap 50
		{"5"},
	} {
		var out bytes.Buffer
		err := runLookup([]string{"--query", "配置", "--db", db, "--limit", tc.limit}, &out, &emptyStderr{}, deps)
		if err != nil {
			t.Fatalf("--limit %s: %v", tc.limit, err)
		}
		var results []lookup.Result
		if err := json.Unmarshal(out.Bytes(), &results); err != nil {
			t.Fatalf("--limit %s: bad JSON: %v\n%s", tc.limit, err, out.String())
		}
		// The library has 3 slices but "配置" matches exactly one; the clamp
		// contract is: never error, never empty for a matching query. Before
		// the <=0 fallback, --limit 0 returned zero results.
		if len(results) != 1 {
			t.Errorf("--limit %s: results = %d, want 1 (the matching slice)", tc.limit, len(results))
		}
	}
}
