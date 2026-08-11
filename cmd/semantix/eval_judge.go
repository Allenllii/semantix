package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"semantix/kernel/judge"
)

// runEvalJudge evaluates LLM-judge authenticity against a human-audited
// sample (Issue #8 acceptance ①/③/⑥): per-pair consistency vs the oracle,
// false-approve rate ε_fa, and the incremental error upper bound
// Δerr_upper = ε_fa × p_prom. Strategy value and judge authenticity are
// reported separately (the eval command owns the former).
// Returns 0 (pass), 3 (consistency below threshold) or 2 (usage/io error).
func runEvalJudge(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("eval-judge", flag.ContinueOnError)
	auditPath := fs.String("audit", "testdata/judge-audit.tsv", "human-audited sample: query\tcached_answer\toracle(approve|reject)")
	stub := fs.String("stub", "", "deterministic judge for CI: yes|no|error (overrides --judge-*)")
	baseURL := fs.String("judge-base-url", "", "OpenAI/Anthropic compatible endpoint (with --stub empty)")
	model := fs.String("judge-model", "", "judge model name")
	protocol := fs.String("judge-protocol", "openai", "openai|anthropic")
	apiKey := os.Getenv("SEMANTIX_JUDGE_API_KEY")
	pProm := fs.Float64("p-prom", 0.3, "promoted traffic share for the error-upper bound")
	minConsistency := fs.Float64("min-consistency", 95.0, "fail threshold: consistency %% (exit 3 when below)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	f, err := os.Open(*auditPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval-judge:", err)
		return 2
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
			fmt.Fprintln(os.Stderr, "eval-judge: malformed audit line:", line)
			return 2
		}
		pairs = append(pairs, pair{q: parts[0], a: parts[1], oracle: parts[2] == "approve"})
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "eval-judge:", err)
		return 2
	}
	if len(pairs) == 0 {
		fmt.Fprintln(os.Stderr, "eval-judge: audit sample empty")
		return 2
	}

	var j judge.Judge
	switch {
	case *stub != "":
		j = stubJudge(*stub)
	case *baseURL == "" || *model == "" || apiKey == "":
		fmt.Fprintln(os.Stderr, "eval-judge: need --stub OR --judge-base-url+--judge-model+SEMANTIX_JUDGE_API_KEY")
		return 2
	default:
		var err error
		j, err = judge.NewLLMJudge(judge.LLMConfig{
			BaseURL:  *baseURL,
			Model:    *model,
			Protocol: *protocol,
			APIKey:   apiKey,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "eval-judge:", err)
			return 2
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

	fmt.Fprintf(stdout, "# eval-judge: %d pairs, stub=%q protocol=%s model=%s\n", len(pairs), *stub, *protocol, *model)
	fmt.Fprintf(stdout, "consistency_%%\tfalse_approve_%%\tΔerr_upper_%%\tpass\n")
	fmt.Fprintf(stdout, "%.1f\t%.1f\t%.3f\t%v\n", consistency, epsFA, deltaUpper, pass)
	for i := range pairs {
		fmt.Fprintf(stdout, "%-40s\t%-40s\toracle=%v\tjudge=%s\n",
			truncRune(pairs[i].q, 40), truncRune(pairs[i].a, 40), pairs[i].oracle, pairs[i].verdict)
	}
	if !pass {
		fmt.Fprintln(os.Stderr, fmt.Sprintf("eval-judge: consistency %.1f%% < %.1f%%", consistency, *minConsistency))
		return 3
	}
	return 0
}

// exitCodeError carries a non-zero process exit code.
type exitCodeError struct {
	code int
	msg  string
}

// stubJudge returns a deterministic judge for CI runs.
func stubJudge(mode string) judge.Judge {
	if mode == "yes" {
		return yesJudge{}
	}
	return judge.NoopJudge{} // no → reject; error mode → Confirm returns false via Noop
}

// yesJudge approves everything (stub for CI upper-bound runs).
type yesJudge struct{}

func (yesJudge) Confirm(context.Context, judge.Candidate) (bool, error) { return true, nil }

// truncRune truncates s to at most n runes for tabular output.
func truncRune(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
