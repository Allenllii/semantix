package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"semantix/kernel/evolve"
	"semantix/kernel/usage"
)

// usageJSON is the --json envelope payload for `semantix usage` (M2-U22).
type usageJSON struct {
	Events         int     `json:"events"`
	TokensIn       int64   `json:"tokens_in"`
	TokensOut      int64   `json:"tokens_out"`
	CacheHitTokens int64   `json:"cache_hit_tokens"`
	L3Reuses       int     `json:"l3_reuses"`
	InjectedTokens int64   `json:"injected_tokens"`
	SliceHits      int     `json:"slice_hits"`
	CostPaidUSD    float64 `json:"cost_paid_usd"`
	CostNoCacheUSD float64 `json:"cost_no_cache_usd"`
	SavingsUSD     float64 `json:"savings_usd"`
	SavingsRate    float64 `json:"savings_rate"`
}

// runUsage summarizes the usage log and reports cost savings (Issue #60 /
// U17). With --evolve-db it also feeds cost/latency signals into the
// evolution engine and prints the adjusted params.
func runUsage(args []string, stdout io.Writer, deps dependencies) int {
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	db := fs.String("db", filepath.Join(".semantix", "usage.jsonl"), "usage log path (default .semantix/usage.jsonl)")
	costMiss := fs.Float64("cost-miss", cfgFloat(deps.resolved, "cost.input_price_usd", usage.DefaultCostMissPerMTok), "USD per 1M tokens at cache miss")
	costHit := fs.Float64("cost-hit", cfgFloat(deps.resolved, "cost.cache_price_usd", usage.DefaultCostHitPerMTok), "USD per 1M tokens at cache hit")
	evolveDB := fs.String("evolve-db", "", "optional evolve engine state dir (feeds cost signals and prints adjusted params)")
	jsonOut := fs.Bool("json", false, "output as JSON envelope")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0 // --help is a successful request
		}
		return 2
	}

	s, err := usage.Summarize(*db, *costMiss, *costHit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "usage:", err)
		return 1 // runtime/IO error, not a usage mistake
	}

	if *jsonOut {
		data := usageJSON{
			Events: s.Events, TokensIn: s.TokensIn, TokensOut: s.TokensOut,
			CacheHitTokens: s.CacheHitTokens, L3Reuses: s.L3Reuses, InjectedTokens: s.InjectedTokens,
			SliceHits: s.SliceHits,
			CostPaidUSD: s.CostPaidUSD, CostNoCacheUSD: s.CostNoCacheUSD,
			SavingsUSD: s.SavingsUSD, SavingsRate: s.SavingsRate,
		}
		if err := writeJSON(stdout, okEnvelope("usage", data)); err != nil {
			fmt.Fprintln(os.Stderr, "usage:", err)
			return 1
		}
		return 0
	}

	// Iconic summary for humans (Issue #152 / U28: vibe-coder readable).
	savingsBar := barChart(s.SavingsRate, 10)
	fmt.Fprintf(stdout, "💰 节省成本  %s  $%.6f\n", savingsBar, s.SavingsUSD)
	fmt.Fprintf(stdout, "📈 节省率    %.1f%%\n", s.SavingsRate*100)
	fmt.Fprintf(stdout, "🧠 L3 复用   %d\n", s.L3Reuses)
	fmt.Fprintf(stdout, "📦 命中切片  %d\n", s.SliceHits)
	fmt.Fprintln(stdout)

	// Raw counters keep the machine-readable key\tvalue shape.
	fmt.Fprintf(stdout, "events\t%d\n", s.Events)
	fmt.Fprintf(stdout, "tokens_in\t%d\n", s.TokensIn)
	fmt.Fprintf(stdout, "tokens_out\t%d\n", s.TokensOut)
	fmt.Fprintf(stdout, "cache_hit_tokens\t%d\n", s.CacheHitTokens)
	fmt.Fprintf(stdout, "l3_reuses\t%d\n", s.L3Reuses)
	fmt.Fprintf(stdout, "injected_tokens\t%d\n", s.InjectedTokens)
	fmt.Fprintf(stdout, "slice_hits\t%d\n", s.SliceHits)
	fmt.Fprintf(stdout, "cost_paid_usd\t%.6f\n", s.CostPaidUSD)
	fmt.Fprintf(stdout, "cost_no_cache_usd\t%.6f\n", s.CostNoCacheUSD)
	fmt.Fprintf(stdout, "savings_usd\t%.6f\n", s.SavingsUSD)
	fmt.Fprintf(stdout, "savings_rate\t%.4f\n", s.SavingsRate)

	if *evolveDB != "" {
		if err := feedEvolve(*evolveDB, s, stdout); err != nil {
			fmt.Fprintln(os.Stderr, "usage: evolve:", err)
			return 1 // runtime error
		}
	}
	return 0
}

// feedEvolve loads (or creates) an evolution engine and records cost/latency
// signals derived from the summary, then prints the current params. The
// persisted state carries the epoch so repeated runs advance it.
func feedEvolve(dir string, s *usage.Summary, stdout io.Writer) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	statePath := filepath.Join(dir, "params.json")
	e := evolve.New(evolve.Config{})
	epoch := uint64(1)
	if b, err := os.ReadFile(statePath); err == nil && len(b) > 0 {
		var st evolveState
		if err := json.Unmarshal(b, &st); err == nil {
			_ = e.Apply(st.Params) // ignore freeze errors on load; defaults remain
			if st.Epoch > 0 {
				epoch = st.Epoch + 1
			}
		}
	}
	// cost signal: 1 - savings rate (higher savings → lower cost signal)
	costVal := 1 - s.SavingsRate
	if costVal < 0 {
		costVal = 0
	}
	if costVal > 1 {
		costVal = 1
	}
	_ = e.RecordSignal(evolve.Signal{Name: "cost", Value: costVal, Epoch: epoch})
	_ = e.RecordSignal(evolve.Signal{Name: "latency", Value: costVal, Epoch: epoch})

	p := e.Params()
	if b, err := json.Marshal(evolveState{Params: p, Epoch: epoch}); err == nil {
		if err := os.WriteFile(statePath, b, 0o600); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "evolve_epoch\t%d\n", epoch)
	fmt.Fprintf(stdout, "evolve_tau_l2\t%.3f\n", p.TauL2)
	fmt.Fprintf(stdout, "evolve_inject_cap\t%.3f\n", p.InjectCap)
	return nil
}

// evolveState is the persisted evolution state (params + last epoch).
type evolveState struct {
	Params evolve.Params `json:"params"`
	Epoch  uint64        `json:"epoch"`
}

// barChart renders an ASCII bar (█ filled, ░ empty) for a ratio in [0,1].
func barChart(ratio float64, width int) string {
	if width <= 0 {
		return ""
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio*float64(width) + 0.5)
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
