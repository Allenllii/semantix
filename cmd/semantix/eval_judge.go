package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"semantix/kernel/judge"
)

// runEvalJudge evaluates LLM-judge authenticity against a human-audited
// sample (Issue #8 acceptance ①/③/⑥): per-pair consistency vs the oracle,
// false-approve rate ε_fa, and the incremental error upper bound
// Δerr_upper = ε_fa × p_prom. Strategy value and judge authenticity are
// reported separately (the eval command owns the former).
// Exit codes (U19 contract): 0 pass · 1 runtime/IO error (missing audit
// file, scan failure) · 2 usage/input-format error (bad flag, malformed
// audit line, empty sample, missing judge backend) · 3 consistency below
// threshold.
func runEvalJudge(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("eval-judge", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "write JSON envelope output (§4.2)")
	auditPath := fs.String("audit", "testdata/judge-audit.tsv", "human-audited sample: query\tcached_answer\toracle(approve|reject)")
	stub := fs.String("stub", "", "deterministic judge for CI: yes|no|error (overrides --judge-*)")
	baseURL := fs.String("judge-base-url", "", "OpenAI/Anthropic compatible endpoint (with --stub empty)")
	model := fs.String("judge-model", "", "judge model name")
	protocol := fs.String("judge-protocol", "openai", "openai|anthropic")
	apiKey := os.Getenv("SEMANTIX_JUDGE_API_KEY")
	pProm := fs.Float64("p-prom", 0.3, "promoted traffic share for the error-upper bound")
	minConsistency := fs.Float64("min-consistency", 95.0, "fail threshold: consistency %% (exit 3 when below)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0 // --help is a successful request
		}
		if wantsJSON(args) {
			_ = writeErrorEnvelope(stdout, "eval-judge", 2, err.Error())
		}
		return 2
	}
	// fail emits the §4.2 failure envelope (ok:false, error.code per §4.3)
	// when --json is on, keeps the stderr text otherwise, and returns the
	// exit code — every path stays parseable for a JSON consumer (U35,
	// Issue #166).
	fail := func(code int, msg string) int {
		if *jsonOut {
			_ = writeErrorEnvelope(stdout, "eval-judge", code, msg)
		} else {
			fmt.Fprintln(os.Stderr, "eval-judge:", msg)
		}
		return code
	}
	if math.IsNaN(*pProm) || math.IsInf(*pProm, 0) || *pProm < 0 || *pProm > 1 {
		return fail(2, "invalid --p-prom (want 0 <= v <= 1)")
	}
	if math.IsNaN(*minConsistency) || math.IsInf(*minConsistency, 0) || *minConsistency < 0 || *minConsistency > 100 {
		return fail(2, "invalid --min-consistency (want 0 <= v <= 100)")
	}

	f, err := os.Open(*auditPath)
	if err != nil {
		return fail(1, err.Error()) // runtime/IO error, not a usage mistake
	}
	defer f.Close()

	type pair struct {
		q, a    string
		oracle  bool
		judgeOK bool
		verdict string
	}
	var pairs []pair
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return fail(2, "malformed audit line: "+line)
		}
		pairs = append(pairs, pair{q: parts[0], a: parts[1], oracle: parts[2] == "approve"})
	}
	if err := sc.Err(); err != nil {
		return fail(1, err.Error()) // runtime/IO error
	}
	if len(pairs) == 0 {
		return fail(2, "audit sample empty")
	}

	var j judge.Judge
	switch {
	case *stub != "":
		var err error
		if j, err = stubJudge(*stub); err != nil {
			return fail(2, err.Error())
		}
	case *baseURL == "" || *model == "" || apiKey == "":
		return fail(2, "need --stub OR --judge-base-url+--judge-model+SEMANTIX_JUDGE_API_KEY")
	default:
		var err error
		j, err = judge.NewLLMJudge(judge.LLMConfig{
			BaseURL:  *baseURL,
			Model:    *model,
			Protocol: *protocol,
			APIKey:   apiKey,
		})
		if err != nil {
			return fail(2, err.Error())
		}
	}

	agree, fa := 0, 0
	for i := range pairs {
		ok, err := j.Confirm(context.Background(), judge.Candidate{
			Query:   pairs[i].q,
			Content: pairs[i].a,
		})
		if err != nil {
			pairs[i].verdict = "error"
			continue // conservative: error ≠ approval
		}
		pairs[i].judgeOK = ok
		if ok == pairs[i].oracle {
			agree++
		}
		if ok && !pairs[i].oracle {
			fa++ // false approve
		}
		pairs[i].verdict = map[bool]string{true: "approve", false: "reject"}[ok]
	}

	n := float64(len(pairs))
	consistency := 100 * float64(agree) / n
	epsFA := 100 * float64(fa) / n
	deltaUpper := epsFA * (*pProm) / 100 // percent of promoted traffic
	pass := consistency >= *minConsistency

	if *jsonOut {
		data := evalJudgeData{
			Pairs:          len(pairs),
			Stub:           *stub,
			Protocol:       *protocol,
			Model:          *model,
			Consistency:    consistency,
			FalseApprove:   epsFA,
			DeltaUpper:     deltaUpper,
			Pass:           pass,
			MinConsistency: *minConsistency,
			PProm:          *pProm,
		}
		// Gate not met (exit 3, U19 §4.3): the JSON consumer still gets a
		// parseable failure envelope with error.code=3 (U35 acceptance).
		if !pass {
			_ = writeErrorEnvelope(stdout, "eval-judge", 3,
				fmt.Sprintf("consistency %.1f%% < %.1f%%", consistency, *minConsistency))
			return 3
		}
		if err := writeEnvelope(stdout, "eval-judge", data); err != nil {
			return fail(1, err.Error())
		}
		return 0
	}

	fmt.Fprintf(stdout, "# eval-judge: %d pairs, stub=%q protocol=%s model=%s\n", len(pairs), *stub, *protocol, *model)
	fmt.Fprintf(stdout, "consistency_%%\tfalse_approve_%%\tΔerr_upper_%%\tpass\n")
	fmt.Fprintf(stdout, "%.1f\t%.1f\t%.3f\t%v\n", consistency, epsFA, deltaUpper, pass)
	for i := range pairs {
		fmt.Fprintf(stdout, "%-40s\t%-40s\toracle=%v\tjudge=%s\n",
			truncRune(pairs[i].q, 40), truncRune(pairs[i].a, 40), pairs[i].oracle, pairs[i].verdict)
	}
	if !pass {
		return fail(3, fmt.Sprintf("consistency %.1f%% < %.1f%%", consistency, *minConsistency))
	}
	return 0
}

// evalJudgeData is the --json payload for eval-judge (U35, §4.2): the
// Issue #8 judge-authenticity metrics. Fields are additive.
type evalJudgeData struct {
	Pairs          int     `json:"pairs"`
	Stub           string  `json:"stub,omitempty"`
	Protocol       string  `json:"protocol"`
	Model          string  `json:"model"`
	Consistency    float64 `json:"consistency_pct"`
	FalseApprove   float64 `json:"false_approve_pct"`
	DeltaUpper     float64 `json:"delta_upper_pct"`
	Pass           bool    `json:"pass"`
	MinConsistency float64 `json:"min_consistency"`
	PProm          float64 `json:"p_prom"`
}

// exitCodeError carries a non-zero process exit code.
type exitCodeError struct {
	code int
	msg  string
}

// stubJudge returns a deterministic judge for CI runs. Modes: yes →
// approve everything; no → reject everything; error → every Confirm
// fails (exercises the verdict="error" path). An unknown mode is a usage
// mistake — it must not silently fall back to "no" and mask a CI typo.
func stubJudge(mode string) (judge.Judge, error) {
	switch mode {
	case "yes":
		return yesJudge{}, nil
	case "no":
		return judge.NoopJudge{}, nil
	case "error":
		return errJudge{}, nil
	default:
		return nil, usagef("invalid --stub %q (want yes, no, or error)", mode)
	}
}

// yesJudge approves everything (stub for CI upper-bound runs).
type yesJudge struct{}

func (yesJudge) Confirm(context.Context, judge.Candidate) (bool, error) { return true, nil }

// errJudge fails every Confirm with an error (CI exercise of the judge
// error path; errors are conservative: verdict "error" ≠ approval).
type errJudge struct{}

func (errJudge) Confirm(context.Context, judge.Candidate) (bool, error) {
	return false, errors.New("stub judge error")
}

// truncRune truncates s to at most n runes for tabular output.
func truncRune(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
