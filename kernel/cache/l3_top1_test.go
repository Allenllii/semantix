package cache

import (
	"context"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

// ---- Issue #241: the zone denominator must be scoped to the candidates
// DecideL3 actually considers (Result-typed), not to the raw hit list. ----

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

// TestDecideL3Top1ScopedToResultCandidates replays the ten repeated tasks of
// the GW4 acceptance (docs/reports/gateway-m1-acceptance.md §5.2). Every task
// was asked verbatim twice, so every Result candidate SHOULD be reusable; the
// store also holds the byte-identical Prompt slice, which BM25 always ranks
// first. With top1 taken from the unfiltered hit list only t07 (rel 0.826)
// cleared TauHigh — the other nine were pushed into grey/miss by their own
// prompt twin. Scoping top1 to the Result candidates must make all ten reuse
// without a judge.
func TestDecideL3Top1ScopedToResultCandidates(t *testing.T) {
	cases := []struct {
		task          string
		promptScore   float64
		resultScore   float64
		oldZoneRelDoc string // zone under the pre-fix denominator, for the failure message
	}{
		{"t07", 56.97, 47.03, "hit 0.826"},
		{"t04", 65.31, 47.13, "grey 0.722"},
		{"t05", 50.83, 36.66, "grey 0.721"},
		{"t10", 66.57, 44.95, "grey 0.675"},
		{"t08", 43.01, 25.90, "grey 0.602"},
		{"t02", 59.78, 35.46, "grey 0.593"},
		{"t03", 59.33, 34.62, "grey 0.584"},
		{"t01", 54.49, 30.86, "grey 0.566"},
		{"t09", 79.26, 42.94, "miss 0.542"},
		{"t06", 65.72, 34.50, "miss 0.525"},
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
			if res == nil {
				t.Fatalf("%s: repeated task must reuse; the prompt twin (%.2f) depressed the Result (%.2f) to %s",
					c.task, c.promptScore, c.resultScore, c.oldZoneRelDoc)
			}
			if res.SliceID != c.task+"-result" {
				t.Fatalf("%s: SliceID = %q, want %s-result", c.task, res.SliceID, c.task)
			}
		})
	}
}

// TestDecideL3PromptTwinDoesNotDepressResultBM25 is the same defect on the
// real retriever: the store holds a Prompt slice byte-identical to the query
// (what the gateway writes for every request) plus the Result slice that
// answers it. BM25 length normalization always ranks the short prompt twin
// first, so the answer must not be judged relative to it.
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
	if err := idx.Insert(&slice.Slice{
		ID: "gw-result", Type: slice.Result, Scope: slice.Project,
		Content: []byte("正则表达式的回溯灾难来自嵌套量词与可选分支的组合爆炸。" +
			"在 Go 里标准库 regexp 使用 RE2 引擎，没有回溯，最坏情况仍是线性时间，" +
			"所以只要避免手写回溯匹配器就不会遇到灾难性回溯；" +
			"若必须使用第三方回溯引擎，则应当消除嵌套量词、限定重复次数并加上超时。"),
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

// ---- fail-closed guards: these must hold BEFORE and AFTER the fix ----

// TestDecideL3DissimilarResultRejectedByAbsoluteFloor: on the bounded
// (cosine) scale the absolute floors are the guard, and scoping top1 to the
// Result candidates makes them STRICTER, never looser — top1_result <=
// top1_all, so a candidate that could not clear AbsHigh/AbsLow before still
// cannot clear it after.
func TestDecideL3DissimilarResultRejectedByAbsoluteFloor(t *testing.T) {
	cases := []struct {
		name    string
		hits    []slice.Hit
		wantNil bool
	}{
		{
			// 0.30 < AbsLow 0.45 -> Miss, both denominators.
			name: "below AbsLow is miss",
			hits: []slice.Hit{
				hitOf("p", slice.Prompt, 0.92),
				hitOf("r-dissimilar", slice.Result, 0.30),
			},
			wantNil: true,
		},
		{
			// 0.50 clears AbsLow but not AbsHigh 0.70 -> Grey even at
			// rel 1.0; without a judge that is a conservative reject. A
			// lone Result candidate must NOT become an unconditional Hit.
			name: "between AbsLow and AbsHigh is grey",
			hits: []slice.Hit{
				hitOf("p", slice.Prompt, 0.92),
				hitOf("r-weak", slice.Result, 0.50),
			},
			wantNil: true,
		},
		{
			// 0.75 clears AbsHigh: a genuinely similar Result still reuses.
			name: "above AbsHigh reuses",
			hits: []slice.Hit{
				hitOf("p", slice.Prompt, 0.92),
				hitOf("r-similar", slice.Result, 0.75),
			},
			wantNil: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &L3Decider{Index: &fixedIndex{hits: c.hits}, Root: t.TempDir()}
			res, err := d.DecideL3(context.Background(), Query{UserInput: "q", Scope: slice.Project})
			if err != nil {
				t.Fatal(err)
			}
			if c.wantNil && res != nil {
				t.Fatalf("must fail closed, got reuse of %s", res.SliceID)
			}
			if !c.wantNil && res == nil {
				t.Fatal("similar Result above the absolute floor must reuse")
			}
		})
	}
}

// TestDecideL3WeakResultNotPromotedWhenTopResultRejected: the Result-scoped
// denominator must keep discriminating INSIDE the Result set. When the best
// Result is rejected by another gate (here: context isolation), the runner-up
// is not promoted — it is still measured against the best Result's score.
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
		// Unchanged semantics: a lone Result is its own top1 (rel 1.0) and
		// reuses once it clears AbsHigh — the behavior L3 already had when
		// no Prompt twin happened to be in the store.
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
