// Package scheddemo drives the Agile-2 scheduling demo (Issue #193): the same
// task family is run once with the kernel scheduler on (kernel/sched.RuleDecider
// governs parallel grouping, model tier, tool suspension, and budget action) and
// once with it off (an empty-plan decider → the harness static ReadOnly grouping,
// no suspension, no tier, no budget action). The scheduling decisions and the
// harness behavior they change are real production code paths; only the
// per-tool durations and the token→USD price model are labeled synthetic
// fixtures (see docs/reports/agile2-scheduling-demo.md §honesty).
package scheddemo

// TierPrice is one model tier's synthetic price, USD per 1,000,000 tokens.
type TierPrice struct {
	InputPerM  float64
	OutputPerM float64
}

// TierPrices is the synthetic price table, sourced from DeepSeek V4 published
// pricing as of 2026-08 (the legacy deepseek-chat/reasoner names retired
// 2026-07): V4-Flash bills $0.14/M input (cache-miss) + $0.28/M output; V4-Pro
// bills $0.435/M input + $0.87/M output (standard, non-peak). These are fixture
// constants — the tier *decision* is real, the absolute USD is illustrative.
var TierPrices = map[string]TierPrice{
	"flash": {InputPerM: 0.14, OutputPerM: 0.28},
	"pro":   {InputPerM: 0.435, OutputPerM: 0.87},
}

// RoundCostUSD computes one round's synthetic cost from its input/output token
// estimate and the tier the scheduler chose. An empty or unknown tier is the
// OFF baseline (no kernel tiering decision) and bills at the capable (pro) tier:
// without kernel tiering an operator provisions the strong model statically, so
// the OFF arm is never made to look artificially cheap.
func RoundCostUSD(tier string, inputTokens, outputTokens int) float64 {
	p, ok := TierPrices[tier]
	if !ok {
		p = TierPrices["pro"]
	}
	return float64(inputTokens)/1e6*p.InputPerM + float64(outputTokens)/1e6*p.OutputPerM
}
