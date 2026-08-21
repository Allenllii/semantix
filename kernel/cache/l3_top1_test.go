package cache

import (
	"context"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

// ---- Issue #241: the L3 zone verdict must split its denominator ----
//
// DecideL3 took top1 from the raw hit list and only then filtered to
// Result-typed slices, so every reusable candidate was measured against a
// slice it never competed with. In the gateway the store always holds a
// Prompt slice byte-identical to the query; under BM25 that twin ranks
// first by a wide margin (GW4 acceptance measured 43-79 vs the Results'
// 26-47), which depressed every Result's score/top1 for a reason unrelated
// to reusability: 8 of 10 repeated tasks fell into grey and 2 into miss,
// leaving L3's hit rate to be decided by whether a judge was configured
// (1/10 without, 7/10 with) rather than by retrieval quality.
//
// The fix is NOT the issue's option 1 (denominator = best Result only):
// that makes the best Result's ratio 1.0 unconditionally and guts the
// relative axis — Allenllii's review on the issue showed it turns into
// "any query reuses the best Result" on the legacy CLI path. Instead
// zone.ClassifyL3 takes three arguments and splits the two jobs the old
// denominator was doing: prominence among Result peers (resultTop1) and
// batch-relevance scale anchor (globalTop1, usually the prompt twin).

// fixedIndex is a slice.Index stub returning a preset hit list. Real BM25
// scores are corpus-dependent; these tests need exact numbers so the zone
// arithmetic (score/top1 against TauHigh/TauLow/AbsHigh/AbsLow) is asserted
// on the same values the GW4 acceptance measured in production.
type fixedIndex struct{ hits []slice.Hit }

func (f *fixedIndex) Search(query string, k int, scope slice.Scope) ([]slice.Hit, error) {
	if k <= 0 {
		return []slice.Hit{}, nil
	}
	n := len(f.hits)
	if k < n {
		n = k
	}
	out := make([]slice.Hit, n)
	copy(out, f.hits[:n])
	return out, nil
}

func (f *fixedIndex) Insert(s *slice.Slice) error { return nil }
func (f *fixedIndex) Remove(id string) error      { return nil }

// hitOf builds a scored hit. Result slices are marked L3Safe with no Deps so
// verified() passes on its explicit-opt-in path: these tests exercise the
// retrieval/zone gate, not the dependency chain (covered in l3_test.go).
func hitOf(id string, typ slice.SliceType, score float64) slice.Hit {
	return slice.Hit{
		Score: score,
		Slice: &slice.Slice{
			ID:      id,
			Type:    typ,
			Scope:   slice.Project,
			Content: []byte("content of " + id),
			Meta:    slice.SliceMeta{L3Safe: typ == slice.Result},
		},
	}
}

// stampedHit is hitOf plus the gateway context/model stamp.
func stampedHit(id string, typ slice.SliceType, score float64, ctxHash, model string) slice.Hit {
	h := hitOf(id, typ, score)
	h.Slice.Meta.ContextHash = ctxHash
	h.Slice.Meta.Model = model
	return h
}

// TestDecideL3TwoAxisReplay replays the ten repeated tasks of the GW4
// acceptance (docs/reports/gateway-m1-acceptance.md §5.2). Every task was
// asked verbatim twice, so every Result candidate SHOULD be reusable; the
// store also holds the byte-identical Prompt slice, which BM25 always ranks
// first. With the raw-list denominator only t07 (rel 0.826) cleared
// TauHigh. The two-axis verdict must restore all ten to reuse without a
// judge: real BM25 clusters rewritten same-task answers at 0.52–0.83 of
// their twin, above the AbsLow relevance floor.
func TestDecideL3TwoAxisReplay(t *testing.T) {
	cases := []struct {
		task        string
		promptScore float64
		resultScore float64
		wantReuse   bool
		oldZoneRel  string // zone under the pre-fix denominator, for the failure message
	}{
		{"t07", 56.97, 47.03, true, "hit 0.826"},
		{"t04", 65.31, 47.13, true, "grey 0.722"},
		{"t05", 50.83, 36.66, true, "grey 0.721"},
		{"t10", 66.57, 44.95, true, "grey 0.675"},
		{"t08", 43.01, 25.90, true, "grey 0.602"},
		{"t02", 59.78, 35.46, true, "grey 0.593"},
		{"t03", 59.33, 34.62, true, "grey 0.584"},
		{"t01", 54.49, 30.86, true, "grey 0.566"},
		{"t09", 79.26, 42.94, true, "miss 0.542"},
		{"t06", 65.72, 34.50, true, "miss 0.525"},
	}
	for _, c := range cases {
		t.Run(c.task, func(t *testing.T) {
			idx := &fixedIndex{hits: []slice.Hit{
				hitOf(c.task+"-prompt", slice.Prompt, c.promptScore),
				hitOf(c.task+"-result", slice.Result, c.resultScore),
			}}
			// No Judge: a grey verdict rejects conservatively, so reuse here
			// can only come from a real zone.Hit.
			d := &L3Decider{Index: idx, Root: t.TempDir()}
			res, err := d.DecideL3(context.Background(), Query{
				UserInput: c.task + " 重复任务", Scope: slice.Project,
			})
			if err != nil {
				t.Fatal(err)
			}
			if c.wantReuse && res == nil {
				t.Fatalf("%s: repeated task must reuse; the prompt twin (%.2f) depressed the Result (%.2f) to %s",
					c.task, c.promptScore, c.resultScore, c.oldZoneRel)
			}
			if c.wantReuse && res.SliceID != c.task+"-result" {
				t.Fatalf("%s: SliceID = %q, want %s-result", c.task, res.SliceID, c.task)
			}
			if !c.wantReuse && res != nil {
				t.Fatalf("%s: global rel %.3f is below the relevance floor, must fail closed, got reuse of %s",
					c.task, c.resultScore/c.promptScore, res.SliceID)
			}
		})
	}
}

// TestDecideL3PromptTwinDoesNotDepressResultBM25 is the same defect on the
// real retriever: the store holds a Prompt slice byte-identical to the query
// (what the gateway writes for every request) plus a Result slice that
// answers it with a rewritten version of the same task. BM25 length
// normalization always ranks the short prompt twin first, so the answer
// must be judged relative to its Result peers AND against the twin only as
// a scale anchor — not as the sole denominator.
func TestDecideL3PromptTwinDoesNotDepressResultBM25(t *testing.T) {
	const query = "如何在 Go 里避免正则表达式的回溯灾难"
	idx := bm25.New()
	if err := idx.Insert(&slice.Slice{
		ID: "gw-prompt", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte(query), // byte-identical twin, always BM25 top1
		Meta:    slice.SliceMeta{SourceSession: "s1"},
	}); err != nil {
		t.Fatal(err)
	}
	// Rewritten same-task answer: shares the query's key terms but is not
	// byte-identical. Real BM25 measures this at ~0.54 of the twin (above
	// the AbsLow relevance floor 0.45; same-field-different-task sits near
	// 0.16), so it must reuse — the raw-list denominator would have
	// rejected it as grey/miss.
	if err := idx.Insert(&slice.Slice{
		ID: "gw-result", Type: slice.Result, Scope: slice.Project,
		Content: []byte("在 Go 里避免正则表达式的回溯灾难：标准库 regexp 用 RE2 引擎无回溯，" +
			"避免嵌套量词、限定重复次数并加超时即可避免灾难。"),
		Meta: slice.SliceMeta{SourceSession: "s1", L3Safe: true},
	}); err != nil {
		t.Fatal(err)
	}
	d := &L3Decider{Index: idx, Root: t.TempDir()} // no judge: grey rejects
	res, err := d.DecideL3(context.Background(), Query{UserInput: query, Scope: slice.Project})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.SliceID != "gw-result" {
		t.Fatalf("repeated query must reuse its own Result slice, got %+v", res)
	}
}

// TestDecideL3WeakResultNotPromotedWhenTopResultRejected: the Result-scoped
// relative axis must keep discriminating INSIDE the Result set. When the
// best Result is rejected by another gate (here: context isolation), the
// runner-up is not promoted — it is still measured against the best
// Result's score (and fails the global relevance floor on top).
func TestDecideL3WeakResultNotPromotedWhenTopResultRejected(t *testing.T) {
	idx := &fixedIndex{hits: []slice.Hit{
		stampedHit("prompt-twin", slice.Prompt, 79.26, "ctx-a", "ds"),
		stampedHit("result-strong", slice.Result, 42.94, "ctx-b", "ds"), // wrong context
		stampedHit("result-weak", slice.Result, 12.00, "ctx-a", "ds"),   // rel 0.279 -> miss
	}}
	d := &L3Decider{Index: idx, Root: t.TempDir()}
	res, err := d.DecideL3(context.Background(), Query{
		UserInput: "q", Scope: slice.Project, ContextHash: "ctx-a", Model: "ds",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatalf("weak Result must not be promoted after the top Result fails the context gate, got %s", res.SliceID)
	}
}

// TestDecideL3GreyPeerStillJudgeGated: a non-best Result peer that is below
// the hit bar (relSame < TauHigh) but above the relevance floor still lands
// in Grey and needs the judge — the relative axis stays alive and the judge
// path still works. The best Result is rejected by the context gate so the
// loop reaches the grey peer.
func TestDecideL3GreyPeerStillJudgeGated(t *testing.T) {
	idx := &fixedIndex{hits: []slice.Hit{
		stampedHit("prompt-twin", slice.Prompt, 56.97, "ctx-a", "ds"),
		stampedHit("result-best", slice.Result, 47.03, "ctx-b", "ds"), // wrong context -> skipped
		stampedHit("peer-grey", slice.Result, 35.00, "ctx-a", "ds"),   // relSame 0.744, relGlobal 0.615 -> Grey
	}}
	d := &L3Decider{Index: idx, Root: t.TempDir(), Judge: &mockJudge{confirm: true}}
	res, err := d.DecideL3(context.Background(), Query{UserInput: "q", Scope: slice.Project, ContextHash: "ctx-a", Model: "ds"})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.SliceID != "peer-grey" {
		t.Fatalf("judge-confirmed grey peer should be reused, got %+v", res)
	}

	d2 := &L3Decider{Index: idx, Root: t.TempDir()} // no judge -> conservative reject
	res2, err := d2.DecideL3(context.Background(), Query{UserInput: "q", Scope: slice.Project, ContextHash: "ctx-a", Model: "ds"})
	if err != nil {
		t.Fatal(err)
	}
	if res2 != nil {
		t.Fatalf("grey peer without judge must fail closed, got %+v", res2)
	}
}

// TestDecideL3Top1EdgeCases pins the degenerate candidate lists.
func TestDecideL3Top1EdgeCases(t *testing.T) {
	ctx := context.Background()
	q := Query{UserInput: "q", Scope: slice.Project}

	t.Run("empty hit list", func(t *testing.T) {
		d := &L3Decider{Index: &fixedIndex{}, Root: t.TempDir()}
		res, err := d.DecideL3(ctx, q)
		if err != nil || res != nil {
			t.Fatalf("no candidates must yield (nil, nil), got %v %v", res, err)
		}
	})

	t.Run("no Result candidates", func(t *testing.T) {
		// Only non-Result hits: nothing to classify, no denominator, no
		// reuse — and no division by a Prompt slice's score.
		d := &L3Decider{Index: &fixedIndex{hits: []slice.Hit{
			hitOf("p1", slice.Prompt, 79.26),
			hitOf("p2", slice.Prompt, 40.00),
		}}, Root: t.TempDir()}
		res, err := d.DecideL3(ctx, q)
		if err != nil || res != nil {
			t.Fatalf("Prompt-only hits must yield (nil, nil), got %v %v", res, err)
		}
	})

	t.Run("single Result candidate", func(t *testing.T) {
		// Unchanged semantics: a lone Result is its own top1 on both axes
		// (relSame 1.0, relGlobal 1.0) and reuses once it clears AbsHigh —
		// the behavior L3 already had when no Prompt twin happened to be
		// in the store.
		d := &L3Decider{Index: &fixedIndex{hits: []slice.Hit{
			hitOf("only", slice.Result, 42.94),
		}}, Root: t.TempDir()}
		res, err := d.DecideL3(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if res == nil || res.SliceID != "only" {
			t.Fatalf("single Result candidate must reuse, got %+v", res)
		}
	})

	t.Run("weak lone result fails closed", func(t *testing.T) {
		// Absolute floors still bind on the scale anchor: a lone weak BM25
		// hit (below AbsHigh) must not self-certify to Hit just because it
		// is its own top1.
		d := &L3Decider{Index: &fixedIndex{hits: []slice.Hit{
			hitOf("weak", slice.Result, 0.50),
		}}, Root: t.TempDir()}
		res, err := d.DecideL3(ctx, q)
		if err != nil || res != nil {
			t.Fatalf("weak lone Result must fail closed, got %v %v", res, err)
		}
	})

	t.Run("non-positive scores are miss", func(t *testing.T) {
		d := &L3Decider{Index: &fixedIndex{hits: []slice.Hit{
			hitOf("zero", slice.Result, 0),
			hitOf("neg", slice.Result, -1),
		}}, Root: t.TempDir()}
		res, err := d.DecideL3(ctx, q)
		if err != nil || res != nil {
			t.Fatalf("non-positive scores must fail closed, got %v %v", res, err)
		}
	})
}
