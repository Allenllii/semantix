package main

import (
	"bufio"
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
// U17). With --evolve-db it also replays the log as cache_hit signals into
// the evolution engine and prints the adjusted params (Issue #220).
func runUsage(args []string, stdout io.Writer, deps dependencies) int {
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	db := fs.String("db", filepath.Join(".semantix", "usage.jsonl"), "usage log path (default .semantix/usage.jsonl)")
	costMiss := fs.Float64("cost-miss", cfgFloat(deps.resolved, "cost.input_price_usd", usage.DefaultCostMissPerMTok), "USD per 1M tokens at cache miss")
	costHit := fs.Float64("cost-hit", cfgFloat(deps.resolved, "cost.cache_price_usd", usage.DefaultCostHitPerMTok), "USD per 1M tokens at cache hit")
	evolveDB := fs.String("evolve-db", "", "optional evolve state dir (replays the log as cache_hit signals; params consumed by lookup/inject --evolve-db)")
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
			SliceHits:   s.SliceHits,
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
		if err := feedEvolve(*evolveDB, *db, stdout); err != nil {
			fmt.Fprintln(os.Stderr, "usage: evolve:", err)
			return 1 // runtime error
		}
	}
	return 0
}

// feedEvolve deterministically replays the usage log through a fresh
// evolution engine — one "cache_hit" signal per recorded turn (L3 reuse
// counts as a full hit), epoch = turn ordinal — then persists the resulting
// parameter snapshot for consumers (lookup/inject --evolve-db). Because the
// log is append-only, replaying from scratch on every run reproduces the
// same cumulative EWMA state, so params.json is a pure output: no engine
// internals need to survive between runs, and reruns are idempotent.
func feedEvolve(dir, logPath string, stdout io.Writer) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	e := evolve.New(evolve.Config{})
	epoch := uint64(0)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var ev usage.Event
		// Malformed lines are skipped, matching usage.Summarize tolerance.
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		var v float64
		switch {
		case ev.L3Reuse:
			v = 1 // turn fully served from the library: strongest hit signal
		case ev.TokensIn > 0:
			v = float64(ev.CacheHitToken) / float64(ev.TokensIn)
		default:
			continue // no usable signal in this turn
		}
		epoch++
		_ = e.RecordSignal(evolve.Signal{Name: "cache_hit", Value: v, Epoch: epoch})
	}
	if err := sc.Err(); err != nil {
		return err
	}

	p := e.Params()
	b, err := json.Marshal(evolveState{Params: p, Epoch: epoch})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "params.json"), b, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "evolve_epoch\t%d\n", epoch)
	fmt.Fprintf(stdout, "evolve_tau_l2\t%.3f\n", p.TauL2)
	fmt.Fprintf(stdout, "evolve_inject_cap\t%.3f\n", p.InjectCap)
	return nil
}

// evolveState is the persisted parameter snapshot (params + turns consumed).
// It is written by `usage --evolve-db` and read by `lookup`/`inject`
// --evolve-db; the file is derived state, never an input to the engine.
type evolveState struct {
	Params evolve.Params `json:"params"`
	Epoch  uint64        `json:"epoch"`
}

// loadEvolveState reads the persisted snapshot; a missing file is a silent
// no-op (fail-open, matching the kernel's degradation philosophy) while a
// corrupt file is a real error the operator should see.
func loadEvolveState(dir string) (*evolveState, error) {
	b, err := os.ReadFile(filepath.Join(dir, "params.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st evolveState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("evolve state %s: %w", filepath.Join(dir, "params.json"), err)
	}
	return &st, nil
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
