package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// eval implements the Issue #7 acceptance comparison: single-threshold vs
// three-region policy on an oracle-labeled evaluation set. The set is a TSV
// file whose rows are `class<TAB>query`. The first trainFrac rows are
// indexed as slices; the remaining rows are replayed as queries. The oracle
// labels (equivalence classes, Issue #2 method) say whether a top-1 hit is
// semantically reusable: relevant iff it shares the query's class.
//
// Reported metrics per policy:
//
//	reuse_rate  = queries judged reusable (L3 hit) / total
//	error_rate  = reusable-but-not-relevant / reusable  (negative-hit share)
//
// The three-region policy has no judge wired in this offline evaluation, so
// grey-zone candidates are conservatively NOT reused (Krites §3.1: verify
// before reuse) — exactly the behavior a judge would gate.
type evalOptions struct {
	set        string
	trainFrac  float64
	scope      slice.Scope
	singleTau  float64 // absolute BM25 score for the single-threshold baseline
	greyTarget float64 // alarm threshold, 0 disables
	strict     bool
	zf         zoneFlagSet
}

func runEval(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "write JSON envelope output (§4.2)")
	var opt evalOptions
	fs.StringVar(&opt.set, "set", "", "oracle evaluation set TSV: class<TAB>query")
	fs.Float64Var(&opt.trainFrac, "train-frac", 0.7, "fraction of rows indexed as training slices (0-1)")
	var scopeName string
	fs.StringVar(&scopeName, "scope", "project", "scope: project|user|session")
	fs.Float64Var(&opt.singleTau, "single-tau", 1.0, "absolute BM25 score threshold for the single-threshold baseline")
	fs.Float64Var(&opt.greyTarget, "grey-target", 30.0, "grey-zone traffic ratio alarm threshold in percent (0 disables)")
	fs.BoolVar(&opt.strict, "strict", false, "return exit code 3 when the grey-zone ratio exceeds --grey-target")
	opt.zf = *addZoneFlags(fs)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0 // --help is a successful request
		}
		if wantsJSON(args) {
			_ = writeErrorEnvelope(stdout, "eval", 2, err.Error())
		}
		return 2
	}
	// fail emits the §4.2 failure envelope (ok:false, error.code per §4.3)
	// when --json is on, keeps the human-readable text otherwise, and
	// returns the exit code — every path stays parseable for a JSON
	// consumer (U35, Issue #166).
	fail := func(code int, msg string) int {
		if *jsonOut {
			_ = writeErrorEnvelope(stdout, "eval", code, msg)
		} else {
			fmt.Fprintf(stdout, "eval: %s\n", msg)
		}
		return code
	}
	if err := opt.zf.validate(); err != nil {
		return fail(2, err.Error())
	}
	if opt.set == "" {
		return fail(2, "Usage: semantix eval --set <oracle.tsv> [--train-frac 0.7] [--single-tau 1.0] [--tau-* ...]")
	}
	if math.IsNaN(opt.trainFrac) || math.IsInf(opt.trainFrac, 0) || opt.trainFrac <= 0 || opt.trainFrac >= 1 {
		return fail(2, "--train-frac must be in (0,1)")
	}
	if math.IsNaN(opt.singleTau) || math.IsInf(opt.singleTau, 0) || opt.singleTau < 0 {
		return fail(2, "invalid --single-tau")
	}
	scope, err := parseScope(scopeName)
	if err != nil {
		return fail(2, err.Error())
	}
	opt.scope = scope

	rows, err := readEvalSet(opt.set)
	if err != nil {
		return fail(1, err.Error())
	}
	if len(rows) < 2 {
		return fail(1, fmt.Sprintf("need at least 2 rows in the evaluation set (have %d)", len(rows)))
	}

	split := int(float64(len(rows)) * opt.trainFrac)
	if split < 1 {
		split = 1
	}
	if split >= len(rows) {
		split = len(rows) - 1
	}
	train := rows[:split]
	replay := rows[split:]

	idx := bm25.New()
	trainClass := make(map[string]string, len(train))
	for i, row := range train {
		id := evalSliceID(row.query, i)
		if err := idx.Insert(&slice.Slice{ID: id, Type: slice.Prompt, Scope: opt.scope, Content: []byte(row.query)}); err != nil {
			return fail(1, fmt.Sprintf("index: %v", err))
		}
		trainClass[id] = row.class
	}

	z := opt.zf.zones()
	// single-threshold and three-region accumulators.
	var sReuse, sErr, sTotal int // single: judged-reusable, reusable-but-wrong, queries
	var tReuse, tErr, tTotal int // three-region: same
	var zoneCount [3]int

	for _, row := range replay {
		hits, err := idx.Search(row.query, 2, opt.scope)
		if err != nil {
			return fail(1, fmt.Sprintf("search: %v", err))
		}
		top1 := 0.0
		top1ID := ""
		top2 := 0.0
		if len(hits) > 0 {
			top1 = hits[0].Score
			top1ID = hits[0].Slice.ID
			if len(hits) > 1 {
				top2 = hits[1].Score
			}
		}
		relevant := top1ID != "" && trainClass[top1ID] == row.class
		grey := classifyTop1(z, top1, top2)

		// Single-threshold policy: score >= tau is "reuse", no oracle check.
		sTotal++
		if top1 >= opt.singleTau {
			sReuse++
			if !relevant {
				sErr++
			}
		}
		// Three-region policy: only clear hits are reused; grey/miss are not.
		tTotal++
		zoneCount[int(grey)]++
		if grey == zone.Hit {
			tReuse++
			if !relevant {
				tErr++
			}
		}
	}

	rate := func(reuse, errN, total int) (float64, float64) {
		if total == 0 {
			return 0, 0
		}
		r := 100 * float64(reuse) / float64(total)
		e := 0.0
		if reuse > 0 {
			e = 100 * float64(errN) / float64(reuse)
		}
		return r, e
	}
	sr, se := rate(sReuse, sErr, sTotal)
	tr, te := rate(tReuse, tErr, tTotal)

	// Issue #7 acceptance: the three-region policy must not be worse on
	// error rate than the single-threshold baseline on the same set.
	verdict := "PASS"
	if sReuse > 0 && te > se+1e-9 {
		verdict = "FAIL (three-region error rate above single-threshold baseline)"
	}
	greyRatio := 0.0
	if tTotal > 0 {
		greyRatio = 100 * float64(zoneCount[int(zone.Grey)]) / float64(tTotal)
	}
	warn := ""
	if opt.greyTarget > 0 && greyRatio > opt.greyTarget {
		warn = fmt.Sprintf("grey_ratio=%.1f%% exceeds target %.1f%% (Issue #7 alarm; retune --tau-*)", greyRatio, opt.greyTarget)
	}

	if *jsonOut {
		data := evalData{
			Set:       opt.set,
			Rows:      len(rows),
			Train:     len(train),
			Replay:    len(replay),
			SingleTau: opt.singleTau,
			Tau: evalTau{
				TauHigh: z.TauHigh, TauLow: z.TauLow, AbsHigh: z.AbsHigh, AbsLow: z.AbsLow,
			},
			Policies: []evalPolicy{
				{Name: "single", Reuse: sReuse, Error: sErr, ReuseRate: sr, ErrorRate: se},
				{Name: "three", Reuse: tReuse, Error: tErr, ReuseRate: tr, ErrorRate: te},
			},
			Zones: evalZones{
				Hit: zoneCount[int(zone.Hit)], Grey: zoneCount[int(zone.Grey)], Miss: zoneCount[int(zone.Miss)],
			},
			GreyRatio: greyRatio,
			Verdict:   verdict,
			Warn:      warn,
		}
		// --strict turns the grey-zone alarm into exit code 3 (CI gate);
		// the JSON consumer still gets a parseable failure envelope.
		if warn != "" && opt.strict {
			_ = writeErrorEnvelope(stdout, "eval", 3, warn)
			return 3
		}
		if err := writeEnvelope(stdout, "eval", data); err != nil {
			return fail(1, err.Error())
		}
		return 0
	}

	fmt.Fprintln(stdout, "# Issue #7 acceptance: single-threshold vs three-region on oracle set")
	fmt.Fprintf(stdout, "# set=%s rows=%d train=%d replay=%d single_tau=%.2f tau=(high %.2f / low %.2f abs %.2f / %.2f)\n",
		opt.set, len(rows), len(train), len(replay), opt.singleTau, z.TauHigh, z.TauLow, z.AbsHigh, z.AbsLow)
	fmt.Fprintln(stdout, "policy\treuse\terror\treuse_rate\terror_rate")
	fmt.Fprintln(stdout, "single\ttotal\terror\trate\trate")
	fmt.Fprintf(stdout, "single\t%d\t%d\t%.1f%%\t%.1f%%\n", sReuse, sErr, sr, se)
	fmt.Fprintf(stdout, "three\t%d\t%d\t%.1f%%\t%.1f%%\n", tReuse, tErr, tr, te)
	fmt.Fprintf(stdout, "# zones: hit=%d grey=%d miss=%d grey_ratio=%.1f%%\n",
		zoneCount[int(zone.Hit)], zoneCount[int(zone.Grey)], zoneCount[int(zone.Miss)], greyRatio)
	fmt.Fprintf(stdout, "# verdict: %s\n", verdict)

	if warn != "" {
		fmt.Fprintf(stdout, "# WARN: %s\n", warn)
		if opt.strict {
			return 3
		}
	}
	return 0
}

// evalData is the --json payload for eval (U35, §4.2): the Issue #7 policy
// comparison, zone distribution, and verdict. Fields are additive.
type evalData struct {
	Set       string       `json:"set"`
	Rows      int          `json:"rows"`
	Train     int          `json:"train"`
	Replay    int          `json:"replay"`
	SingleTau float64      `json:"single_tau"`
	Tau       evalTau      `json:"tau"`
	Policies  []evalPolicy `json:"policies"`
	Zones     evalZones    `json:"zones"`
	GreyRatio float64      `json:"grey_ratio"` // percent
	Verdict   string       `json:"verdict"`
	Warn      string       `json:"warn,omitempty"`
}

type evalTau struct {
	TauHigh float64 `json:"tau_high"`
	TauLow  float64 `json:"tau_low"`
	AbsHigh float64 `json:"abs_high"`
	AbsLow  float64 `json:"abs_low"`
}

type evalPolicy struct {
	Name      string  `json:"name"` // single | three
	Reuse     int     `json:"reuse"`
	Error     int     `json:"error"`
	ReuseRate float64 `json:"reuse_rate"` // percent
	ErrorRate float64 `json:"error_rate"` // percent
}

type evalZones struct {
	Hit  int `json:"hit"`
	Grey int `json:"grey"`
	Miss int `json:"miss"`
}

type evalRow struct {
	class string
	query string
}

// readEvalSet parses a TSV oracle set. Blank lines and # comments are
// ignored; rows must have exactly two tab-separated columns: class, query.
func readEvalSet(path string) ([]evalRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rows []evalRow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("%s:%d: want class<TAB>query, got %q", path, lineNo, line)
		}
		rows = append(rows, evalRow{class: parts[0], query: parts[1]})
	}
	return rows, sc.Err()
}

func evalSliceID(q string, i int) string {
	h := sha256.Sum256([]byte(q))
	return "e-" + hex.EncodeToString(h[:8])
}
