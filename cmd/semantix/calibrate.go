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
	"semantix/kernel/usage"
)

// runCalibrate delivers the L3 negative-observability calibration report
// (Issue #262 §4): offline judge-vs-oracle calibration (confusion matrix,
// consistency, ε_fa / ε_fr, precision/recall/F1, Δerr_upper) and the
// runtime summary from the gateway usage log (L3 reuses, per-reason
// rejects, suspected false hits). The two口径 are reported in separate
// blocks — offline (oracle) and runtime (usage log) are never merged.
//
// Exit codes (U19 contract): 0 pass · 1 audit IO error · 2 usage/input
// error (no --audit and no --usage, malformed audit line, bad judge
// backend) · 3 consistency below --min-consistency.
func runCalibrate(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "write JSON envelope output (§4.2)")
	auditPath := fs.String("audit", "", "human-audited oracle sample: query\tcached_answer\toracle(approve|reject)")
	usagePath := fs.String("usage", "", "gateway usage log (kernel/usage JSONL); missing → runtime block shows N/A")
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
			_ = writeErrorEnvelope(stdout, "calibrate", 2, err.Error())
		}
		return 2
	}
	fail := func(code int, msg string) int {
		if *jsonOut {
			_ = writeErrorEnvelope(stdout, "calibrate", code, msg)
		} else {
			fmt.Fprintln(os.Stderr, "calibrate:", msg)
		}
		return code
	}
	if math.IsNaN(*pProm) || math.IsInf(*pProm, 0) || *pProm < 0 || *pProm > 1 {
		return fail(2, "invalid --p-prom (want 0 <= v <= 1)")
	}
	if math.IsNaN(*minConsistency) || math.IsInf(*minConsistency, 0) || *minConsistency < 0 || *minConsistency > 100 {
		return fail(2, "invalid --min-consistency (want 0 <= v <= 100)")
	}
	if *auditPath == "" && *usagePath == "" {
		return fail(2, "need at least one of --audit <oracle.tsv> or --usage <usage.jsonl>")
	}

	var audit *calibrateAudit
	if *auditPath != "" {
		a, code, msg := runCalibrateAudit(*auditPath, *stub, *baseURL, *model, *protocol, apiKey, *pProm)
		if code != 0 {
			return fail(code, msg)
		}
		audit = a
	}

	var runtime *calibrateRuntime
	if *usagePath != "" {
		// The usage log is an observational input: a missing/unreadable log
		// yields N/A, never a hard failure — a fresh gateway has no events.
		r, err := runCalibrateRuntime(*usagePath)
		if err != nil {
			if *jsonOut {
				runtime = &calibrateRuntime{NA: true}
			} else {
				fmt.Fprintf(stdout, "# runtime (usage log): N/A — %v\n", err)
			}
		} else {
			runtime = r
		}
	}

	pass := true
	if audit != nil {
		pass = audit.Consistency >= *minConsistency
	}

	if *jsonOut {
		data := calibrateData{
			Audit:          audit,
			Runtime:        runtime,
			Pass:           pass,
			MinConsistency: *minConsistency,
		}
		if !pass {
			_ = writeErrorEnvelope(stdout, "calibrate", 3,
				fmt.Sprintf("consistency %.1f%% < %.1f%%", audit.Consistency, *minConsistency))
			return 3
		}
		if err := writeEnvelope(stdout, "calibrate", data); err != nil {
			return fail(1, err.Error())
		}
		return 0
	}

	if audit != nil {
		fmt.Fprintln(stdout, "# offline (oracle): judge vs human audit")
		fmt.Fprintf(stdout, "confusion: tp=%d fp=%d tn=%d fn=%d\n",
			audit.Confusion.TP, audit.Confusion.FP, audit.Confusion.TN, audit.Confusion.FN)
		fmt.Fprintln(stdout, "consistency_pct\tfalse_approve_pct\tfalse_reject_pct\tprecision\trecall\tf1\tdelta_upper_pct")
		fmt.Fprintf(stdout, "%.1f\t%.1f\t%.1f\t%.3f\t%.3f\t%.3f\t%.3f\n",
			audit.Consistency, audit.FalseApprove, audit.FalseReject,
			audit.Precision, audit.Recall, audit.F1, audit.DeltaUpper)
	}
	if runtime != nil {
		fmt.Fprintln(stdout, "# runtime (usage log): gateway L3 decisions")
		// judge_error is a separate column from judge_reject on purpose: a judge
		// that could not be reached is unavailability, not a verdict (Issue #245).
		// Header, format string and argument list are three hand-maintained
		// parallel lists with no compiler check — keep the positions aligned.
		fmt.Fprintln(stdout, "l3_reuses\tl3_grey\tjudge_reject\tjudge_error\tjudge_approved\trules_reject\tfingerprint_reject\tisolated_reject\tfalse_hits\tfalse_hit_rate_pct")
		fmt.Fprintf(stdout, "%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n",
			runtime.L3Reuses, runtime.L3GreyCandidates, runtime.L3JudgeReject, runtime.L3JudgeError,
			runtime.L3JudgeApproved, runtime.L3RulesReject, runtime.L3FingerprintReject,
			runtime.L3IsolatedReject, runtime.L3FalseHits, runtime.rateText())
	}
	if pass {
		fmt.Fprintln(stdout, "# verdict: PASS")
	} else {
		return fail(3, fmt.Sprintf("consistency %.1f%% < %.1f%%", audit.Consistency, *minConsistency))
	}
	return 0
}

// runCalibrateAudit runs the judge over the oracle sample and computes the
// offline calibration metrics. Returns the audit block, or a non-zero
// exit code + message (1 IO, 2 usage/backend).
func runCalibrateAudit(auditPath, stub, baseURL, model, protocol, apiKey string, pProm float64) (*calibrateAudit, int, string) {
	f, err := os.Open(auditPath)
	if err != nil {
		return nil, 1, err.Error()
	}
	defer f.Close()

	type pair struct {
		q, a    string
		oracle  bool
		judgeOK bool
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
			return nil, 2, "malformed audit line: " + line
		}
		pairs = append(pairs, pair{q: parts[0], a: parts[1], oracle: parts[2] == "approve"})
	}
	if err := sc.Err(); err != nil {
		return nil, 1, err.Error()
	}
	if len(pairs) == 0 {
		return nil, 2, "audit sample empty"
	}

	var j judge.Judge
	switch {
	case stub != "":
		var err error
		if j, err = stubJudge(stub); err != nil {
			return nil, 2, err.Error()
		}
	case baseURL == "" || model == "" || apiKey == "":
		return nil, 2, "need --stub OR --judge-base-url+--judge-model+SEMANTIX_JUDGE_API_KEY"
	default:
		var err error
		j, err = judge.NewLLMJudge(judge.LLMConfig{
			BaseURL:  baseURL,
			Model:    model,
			Protocol: protocol,
			APIKey:   apiKey,
		})
		if err != nil {
			return nil, 2, err.Error()
		}
	}

	out := &calibrateAudit{
		Pairs:    len(pairs),
		Stub:     stub,
		Protocol: protocol,
		Model:    model,
		PProm:    pProm,
	}
	agree := 0
	for i := range pairs {
		ok, err := j.Confirm(context.Background(), judge.Candidate{
			Query:   pairs[i].q,
			Content: pairs[i].a,
		})
		if err != nil {
			continue // conservative: error ≠ approval (matches eval-judge)
		}
		pairs[i].judgeOK = ok
		if ok == pairs[i].oracle {
			agree++
		}
		switch {
		case ok && pairs[i].oracle:
			out.Confusion.TP++
		case ok && !pairs[i].oracle:
			out.Confusion.FP++
		case !ok && !pairs[i].oracle:
			out.Confusion.TN++
		default:
			out.Confusion.FN++
		}
	}
	n := float64(len(pairs))
	out.Consistency = 100 * float64(agree) / n
	out.FalseApprove = 100 * float64(out.Confusion.FP) / n
	out.FalseReject = 100 * float64(out.Confusion.FN) / n
	tp := float64(out.Confusion.TP)
	if tp+float64(out.Confusion.FP) > 0 {
		out.Precision = tp / (tp + float64(out.Confusion.FP))
	}
	if tp+float64(out.Confusion.FN) > 0 {
		out.Recall = tp / (tp + float64(out.Confusion.FN))
	}
	if out.Precision+out.Recall > 0 {
		out.F1 = 2 * out.Precision * out.Recall / (out.Precision + out.Recall)
	}
	out.DeltaUpper = out.FalseApprove * pProm / 100 // percent of promoted traffic
	return out, 0, ""
}

// runCalibrateRuntime summarizes the gateway usage log into the runtime
// observability block.
func runCalibrateRuntime(path string) (*calibrateRuntime, error) {
	s, err := usage.Summarize(path, usage.DefaultCostMissPerMTok, usage.DefaultCostHitPerMTok)
	if err != nil {
		return nil, err
	}
	r := &calibrateRuntime{
		Events:              s.Events,
		L3Reuses:            s.L3Reuses,
		L3GreyCandidates:    s.L3GreyCandidates,
		L3JudgeReject:       s.L3JudgeReject,
		L3JudgeError:        s.L3JudgeError,
		L3JudgeApproved:     s.L3JudgeApproved,
		L3RulesReject:       s.L3RulesReject,
		L3FingerprintReject: s.L3FingerprintReject,
		L3IsolatedReject:    s.L3IsolatedReject,
		L3FalseHits:         s.L3FalseHits,
	}
	if s.L3Reuses > 0 {
		rate := 100 * float64(s.L3FalseHits) / float64(s.L3Reuses)
		r.L3FalseHitRate = &rate
	}
	return r, nil
}

// calibrateData is the --json envelope payload (Issue #262 §4.2): the
// offline and runtime blocks stay separate — never merged.
type calibrateData struct {
	Audit          *calibrateAudit    `json:"audit,omitempty"`
	Runtime        *calibrateRuntime  `json:"runtime,omitempty"`
	Pass           bool               `json:"pass"`
	MinConsistency float64            `json:"min_consistency"`
}

// calibrateAudit is the offline judge-vs-oracle calibration block.
type calibrateAudit struct {
	Pairs         int               `json:"pairs"`
	Stub          string            `json:"stub,omitempty"`
	Protocol      string            `json:"protocol"`
	Model         string            `json:"model"`
	Confusion     calibrateConfusion `json:"confusion"`
	Consistency   float64           `json:"consistency_pct"`
	FalseApprove  float64           `json:"false_approve_pct"`
	FalseReject   float64           `json:"false_reject_pct"`
	Precision     float64           `json:"precision"`
	Recall        float64           `json:"recall"`
	F1            float64           `json:"f1"`
	DeltaUpper    float64           `json:"delta_upper_pct"`
	PProm         float64           `json:"p_prom"`
}

type calibrateConfusion struct {
	TP int `json:"tp"`
	FP int `json:"fp"`
	TN int `json:"tn"`
	FN int `json:"fn"`
}

// calibrateRuntime is the usage-log observability block. NA marks a
// missing/unreadable log in JSON output; L3FalseHitRate is nil (omitted)
// when there were no L3 reuses to normalize against.
type calibrateRuntime struct {
	NA                  bool     `json:"na,omitempty"`
	Events              int      `json:"events"`
	L3Reuses            int      `json:"l3_reuses"`
	L3GreyCandidates    int      `json:"l3_grey_candidates"`
	L3JudgeReject       int      `json:"l3_judge_reject"`
	L3JudgeError        int      `json:"l3_judge_error"`
	L3JudgeApproved     int      `json:"l3_judge_approved"`
	L3RulesReject       int      `json:"l3_rules_reject"`
	L3FingerprintReject int      `json:"l3_fingerprint_reject"`
	L3IsolatedReject    int      `json:"l3_isolated_reject"`
	L3FalseHits         int      `json:"l3_false_hits"`
	L3FalseHitRate      *float64 `json:"l3_false_hit_rate_pct,omitempty"`
}

// rateText renders the false-hit rate for TSV output ("N/A" without reuses).
func (r *calibrateRuntime) rateText() string {
	if r.L3FalseHitRate == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.1f", *r.L3FalseHitRate)
}
