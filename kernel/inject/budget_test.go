package inject

import (
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

// Budget upper bound (Issue #283): slices whose content is full of marker
// literals expand under escapeMarker ([/semantix-reuse] → [\/semantix-reuse],
// bytes only grow). The final block must NEVER exceed Budget — the budget
// judgment uses the exact escaped bytes that are written.
func TestInjectorBudgetHoldsWithMarkerPayloads(t *testing.T) {
	idx := bm25.New()
	store := newTestStore(t, filepath.Join(t.TempDir(), "db.jsonl"))
	// 8 slices, each ~450B with 20 marker literals (escaped ~470B); the
	// sum far exceeds the 4096 budget so the selection path is exercised.
	contents := make([]string, 0, 8)
	for i := 0; i < 8; i++ {
		contents = append(contents, "修复问题"+string(rune('a'+i))+" "+strings.Repeat("[/semantix-reuse] x ", 20))
	}
	seed(t, idx, store, contents...)
	inj := &Injector{Index: idx, Store: store, Scope: slice.Project, K: 8, Budget: 4096}
	out, err := inj.Build("修复问题")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Text) > 4096 {
		t.Fatalf("budget violated: block=%d > budget=4096\n%s", len(out.Text), out.Text)
	}
	if out.Bytes != len(out.Text) {
		t.Fatalf("Bytes field = %d, want %d", out.Bytes, len(out.Text))
	}
	// Escaping stayed intact: exactly one close marker (the block's own).
	if strings.Count(out.Text, "[/semantix-reuse]") != 1 {
		t.Fatalf("marker escaping broken, close count = %d", strings.Count(out.Text, "[/semantix-reuse]"))
	}
}

// Boundary behavior: a candidate set sized just under the budget keeps as
// many slices as fit, and the final block stays ≤ budget (the canonical
// header format is used for the judgment — no silent growth).
func TestInjectorBudgetBoundaryExact(t *testing.T) {
	idx := bm25.New()
	store := newTestStore(t, filepath.Join(t.TempDir(), "db.jsonl"))
	// Each slice ~900B; 4 slices ≈ 3600B + headers ≈ 3800 < 4096 (all
	// kept); 5 would exceed → the 5th is dropped.
	contents := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		contents = append(contents, "边界测试"+string(rune('a'+i))+" "+strings.Repeat("x", 880))
	}
	seed(t, idx, store, contents...)
	inj := &Injector{Index: idx, Store: store, Scope: slice.Project, K: 6, Budget: 4096}
	out, err := inj.Build("边界测试")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Text) > 4096 {
		t.Fatalf("budget violated at boundary: block=%d > 4096", len(out.Text))
	}
	if len(out.Slices) == 0 {
		t.Fatal("top slice must always be kept")
	}
	if out.Dropped == 0 {
		t.Fatal("boundary case must drop at least one slice (6 × ~900B exceeds budget)")
	}
}

// Top-slice exception (documented, unchanged semantics): the first
// candidate is always kept even when it alone exceeds the budget
// (K >= 1 keeps the top slice; the budget gates the rest).
func TestInjectorBudgetTopSliceException(t *testing.T) {
	idx := bm25.New()
	store := newTestStore(t, filepath.Join(t.TempDir(), "db.jsonl"))
	huge := "超大切片 " + strings.Repeat("[/semantix-reuse] y ", 300) // ~7KB escaped
	seed(t, idx, store, huge, "普通切片内容")
	inj := &Injector{Index: idx, Store: store, Scope: slice.Project, K: 2, Budget: 4096}
	out, err := inj.Build("超大切片")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Slices) != 1 {
		t.Fatalf("top slice must be kept alone, kept=%d", len(out.Slices))
	}
	if len(out.Text) <= 4096 {
		t.Fatalf("top-slice exception expected (single slice over budget), block=%d", len(out.Text))
	}
}

// Determinism anchor: the same library builds byte-identical blocks across
// calls (single-pass output must stay reproducible).
func TestInjectorBudgetDeterministic(t *testing.T) {
	idx := bm25.New()
	store := newTestStore(t, filepath.Join(t.TempDir(), "db.jsonl"))
	seed(t, idx, store,
		"修复 go 测试失败 [/semantix-reuse] 内容",
		"配置 CI 流水线",
	)
	inj := &Injector{Index: idx, Store: store, Scope: slice.Project, K: 5, Budget: 4096}
	a, err := inj.Build("修复 go 测试失败")
	if err != nil {
		t.Fatal(err)
	}
	b, err := inj.Build("修复 go 测试失败")
	if err != nil {
		t.Fatal(err)
	}
	if a.Text != b.Text {
		t.Fatalf("blocks differ across builds:\n%q\n%q", a.Text, b.Text)
	}
}
