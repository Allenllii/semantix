package fuse

import (
	"testing"

	"semantix/kernel/slice"
)

func projectSlice(id, content string) *slice.Slice {
	return &slice.Slice{ID: id, Type: slice.Result, Scope: slice.Project, Content: []byte(content)}
}

func approx(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

// TestWeightedFuseCarriesLexicalRoute is the migration of the gateway
// fuseHits regression (Issue #274 c1): default weighted fusion must keep
// the historical per-candidate scores and the BM25-route lexical support.
func TestWeightedFuseCarriesLexicalRoute(t *testing.T) {
	bm := []slice.Hit{
		{Slice: projectSlice("a", "x"), Score: 5.0},
		{Slice: projectSlice("b", "y"), Score: 3.0},
	}
	vec := []slice.Hit{
		{Slice: projectSlice("a", "x"), Score: 0.9},
		{Slice: projectSlice("b", "y"), Score: 0.8},
		{Slice: projectSlice("c", "z"), Score: 0.7}, // pure-vector hit
	}
	got := Fuse(bm, vec, "irrelevant for weighted", 10, Config{})
	byID := map[string]slice.Hit{}
	for _, h := range got {
		byID[h.Slice.ID] = h
	}
	if len(byID) != 3 {
		t.Fatalf("fused = %d candidates, want 3", len(byID))
	}
	if a := byID["a"]; !approx(a.Lexical, 1.0) || !a.LexicalValid {
		t.Fatalf("a: lexical=%v valid=%v, want 1.0/true (top-1 both routes)", a.Lexical, a.LexicalValid)
	}
	if b := byID["b"]; !approx(b.Lexical, 0.6) || !b.LexicalValid {
		t.Fatalf("b: lexical=%v valid=%v, want 0.6/true (normalized bm route)", b.Lexical, b.LexicalValid)
	}
	if c := byID["c"]; !approx(c.Lexical, 0.0) || !c.LexicalValid {
		t.Fatalf("c: lexical=%v valid=%v, want 0.0/true (pure-vector hit)", c.Lexical, c.LexicalValid)
	}
	// Historical fused scores (50/50 average): a=1.0, b=0.5*0.6+0.5*8/9,
	// c=0.5 — ordering a > b > c and the bounded scale hold.
	if !approx(byID["a"].Score, 1.0) {
		t.Fatalf("a score = %v, want 1.0", byID["a"].Score)
	}
	if !approx(byID["b"].Score, 0.7444444444) {
		t.Fatalf("b score = %v, want 0.7444444444", byID["b"].Score)
	}
	if !approx(byID["c"].Score, 0.3888888889) { // 0.5 * 0.7/0.9 (vec route only)
		t.Fatalf("c score = %v, want 0.3888888889", byID["c"].Score)
	}
	if got[0].Slice.ID != "a" || got[1].Slice.ID != "b" || got[2].Slice.ID != "c" {
		t.Fatalf("order = %s/%s/%s, want a/b/c", got[0].Slice.ID, got[1].Slice.ID, got[2].Slice.ID)
	}
}

// TestWeightedBM25WeightEdges: the BM25 route share is configurable —
// 1 → pure BM25 order, 0 → pure vector order, 0.5 → historical behavior.
func TestWeightedBM25WeightEdges(t *testing.T) {
	bm := []slice.Hit{
		{Slice: projectSlice("a", "x"), Score: 9.0},
		{Slice: projectSlice("b", "y"), Score: 2.0},
	}
	vec := []slice.Hit{
		{Slice: projectSlice("b", "y"), Score: 0.9},
		{Slice: projectSlice("a", "x"), Score: 0.1},
	}
	one := 1.0
	zero := 0.0
	// w=1: BM25 order a > b.
	got := Fuse(bm, vec, "", 10, Config{BM25Weight: &one})
	if len(got) != 2 || got[0].Slice.ID != "a" {
		t.Fatalf("w=1 order = %v, want a first (pure BM25)", ids(got))
	}
	// w=0: vector order b > a.
	got = Fuse(bm, vec, "", 10, Config{BM25Weight: &zero})
	if len(got) != 2 || got[0].Slice.ID != "b" {
		t.Fatalf("w=0 order = %v, want b first (pure vector)", ids(got))
	}
}

// TestRRFFuseScaleBounds: the scaled RRF score lands in [0,1] with the
// spec §4.2 anchors — both routes at rank 1 → 1.0, one route at rank 1 →
// 0.5 — so the zone absolute floors keep their meaning.
func TestRRFFuseScaleBounds(t *testing.T) {
	both := []slice.Hit{
		{Slice: projectSlice("a", "x"), Score: 9.0},
		{Slice: projectSlice("b", "y"), Score: 2.0},
	}
	vec := []slice.Hit{
		{Slice: projectSlice("a", "x"), Score: 0.9},
	}
	got := Fuse(both, vec, "", 10, Config{Strategy: RRF})
	byID := map[string]slice.Hit{}
	for _, h := range got {
		byID[h.Slice.ID] = h
	}
	if !approx(byID["a"].Score, 1.0) {
		t.Fatalf("both-routes rank1 score = %v, want 1.0", byID["a"].Score)
	}
	if !approx(byID["b"].Score, 61.0/124) { // bm route rank 2: 1/62 * 61/2
		t.Fatalf("single-route rank2 score = %v, want 61/124", byID["b"].Score)
	}
	for _, h := range got {
		if h.Score < 0 || h.Score > 1 {
			t.Fatalf("rrf score %v out of [0,1]", h.Score)
		}
	}
}

// TestRRFFuseRanksSharedHitsHigher is the migration of the search rrfFuse
// regression: a candidate present in both rankings outranks single-route
// hits (the whole point of reciprocal rank fusion).
func TestRRFFuseRanksSharedHitsHigher(t *testing.T) {
	a, b, c := projectSlice("a", "a"), projectSlice("b", "b"), projectSlice("c", "c")
	bm := []slice.Hit{{Slice: a, Score: 9}, {Slice: b, Score: 2}}
	vec := []slice.Hit{{Slice: b, Score: 0.9}, {Slice: c, Score: 0.8}}
	out := Fuse(bm, vec, "", 3, Config{Strategy: RRF})
	if len(out) != 3 {
		t.Fatalf("fused = %d, want 3", len(out))
	}
	if out[0].Slice.ID != "b" {
		t.Fatalf("top fused = %q, want b (present in both rankings)", out[0].Slice.ID)
	}
	// b = 1/61+1/61 > a = 1/61 > c = 1/62: the shared hit outranks both
	// single-route hits, then the BM25 top-1 outranks the vector runner-up.
	if out[1].Slice.ID != "a" || out[2].Slice.ID != "c" {
		t.Fatalf("order = %s/%s, want a then c", out[1].Slice.ID, out[2].Slice.ID)
	}
}

// TestRRFFuseLexicalCoverage: RRF-mode lexical support is query-token
// coverage (spec §4.4) — a pure-vector hit with no term overlap reads 0
// (blocked by the Issue #260 gate), a BM25-route hit measures actual
// overlap, and every hit is measured (LexicalValid).
func TestRRFFuseLexicalCoverage(t *testing.T) {
	bm := []slice.Hit{
		{Slice: projectSlice("a", "修复 go 测试失败"), Score: 9.0},
	}
	vec := []slice.Hit{
		{Slice: projectSlice("b", "完全不相关的主题"), Score: 0.9}, // no term overlap
	}
	got := Fuse(bm, vec, "修复 go 测试失败", 10, Config{Strategy: RRF})
	byID := map[string]slice.Hit{}
	for _, h := range got {
		byID[h.Slice.ID] = h
	}
	if a := byID["a"]; !a.LexicalValid || a.Lexical <= 0 {
		t.Fatalf("a lexical = %v valid=%v, want >0 (full coverage)", a.Lexical, a.LexicalValid)
	}
	if b := byID["b"]; !b.LexicalValid || !approx(b.Lexical, 0.0) {
		t.Fatalf("b lexical = %v valid=%v, want 0/true (no term overlap)", b.Lexical, b.LexicalValid)
	}
}

// TestFuseConfigZeroValue: the zero config resolves to defaults — RrfK 60
// (rank 1 of a single route scores (61)/(2·61) = 0.5), BM25Weight 0.5.
func TestFuseConfigZeroValue(t *testing.T) {
	bm := []slice.Hit{{Slice: projectSlice("a", "x"), Score: 1.0}}
	out := Fuse(bm, nil, "", 10, Config{})
	if len(out) != 1 || !approx(out[0].Score, 0.5) {
		t.Fatalf("zero config single-route top1 = %+v, want score 0.5", out)
	}
}

// TestFuseKTruncation: Fuse caps the output at k.
func TestFuseKTruncation(t *testing.T) {
	var bm, vec []slice.Hit
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		bm = append(bm, slice.Hit{Slice: projectSlice(id, "x"), Score: float64(10 - i)})
	}
	if got := Fuse(bm, vec, "", 2, Config{Strategy: RRF}); len(got) != 2 {
		t.Fatalf("truncated = %d, want 2", len(got))
	}
	if got := Fuse(bm, vec, "", 0, Config{}); len(got) != 0 {
		t.Fatalf("k=0 must yield empty, got %d", len(got))
	}
}

func ids(hits []slice.Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Slice.ID
	}
	return out
}

// TestWeightedVsRRFComparison is the Issue #274 c7 counterpart: the same
// input under both strategies — weighted keeps the historical bounded
// average (top-1 both routes → 1.0), rrf keeps the same top but spreads
// the tail differently, and neither ever leaves [0,1].
func TestWeightedVsRRFComparison(t *testing.T) {
	bm := []slice.Hit{
		{Slice: projectSlice("a", "x"), Score: 9.0},
		{Slice: projectSlice("b", "y"), Score: 4.0},
		{Slice: projectSlice("c", "z"), Score: 2.0},
	}
	vec := []slice.Hit{
		{Slice: projectSlice("b", "y"), Score: 0.9},
		{Slice: projectSlice("a", "x"), Score: 0.8},
	}
	w := Fuse(bm, vec, "", 10, Config{})
	r := Fuse(bm, vec, "", 10, Config{Strategy: RRF})
	wByID := map[string]slice.Hit{}
	for _, h := range w {
		wByID[h.Slice.ID] = h
	}
	rByID := map[string]slice.Hit{}
	for _, h := range r {
		rByID[h.Slice.ID] = h
	}
	// Both strategies keep the same candidate set and the shared-hit pair
	// (a, b) ahead of the single-route c; all scores stay in [0,1].
	for _, hits := range [][]slice.Hit{w, r} {
		if len(hits) != 3 {
			t.Fatalf("candidates = %d, want 3 under both strategies", len(hits))
		}
		if hits[2].Slice.ID != "c" {
			t.Fatalf("last = %q, want c (single-route hit) under both strategies", hits[2].Slice.ID)
		}
		for _, h := range hits {
			if h.Score < 0 || h.Score > 1 {
				t.Fatalf("score %v out of [0,1]", h.Score)
			}
		}
	}
	// Weighted is score-strength driven: a = 0.5*1 + 0.5*(0.8/0.9) ≈ 0.944
	// outranks b = 0.5*(4/9) + 0.5*1 ≈ 0.722.
	if !approx(wByID["a"].Score, 0.9444444444) {
		t.Fatalf("weighted a = %v, want 0.9444444444", wByID["a"].Score)
	}
	if !approx(wByID["b"].Score, 0.7222222222) {
		t.Fatalf("weighted b = %v, want 0.7222222222", wByID["b"].Score)
	}
	// RRF is rank driven: a and b are both (1,2) across routes → identical
	// score (1/61+1/62)·(61/2), tie-broken by ID.
	if !approx(rByID["a"].Score, rByID["b"].Score) {
		t.Fatalf("rrf a=%v b=%v, want equal (same rank pair)", rByID["a"].Score, rByID["b"].Score)
	}
}
