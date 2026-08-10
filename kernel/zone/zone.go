// Package zone implements the grey-zone three-region classifier proposed by
// Krites (arXiv:2602.13165) and vCache (arXiv:2502.03771): a single
// similarity threshold cannot separate "rephrased, same intent" from
// "different intent" — the distributions overlap in the middle. We split the
// similarity axis into clear-hit / grey / clear-miss with two thresholds and
// treat the grey zone as "verify before reuse" instead of deciding blindly.
package zone

// Zone is the three-region verdict for a retrieval candidate.
type Zone int

const (
	// Miss: clearly not reusable (do not reuse, call backend).
	Miss Zone = iota
	// Grey: ambiguous — verify (LLM judge, Issue #8) before reuse; the
	// injector must not place grey slices into the prompt without verdict.
	Grey
	// Hit: clearly reusable.
	Hit
)

// String renders the zone as a stable lowercase tag for JSON/CLI output.
func (z Zone) String() string {
	switch z {
	case Hit:
		return "hit"
	case Grey:
		return "grey"
	default:
		return "miss"
	}
}

// Zones is the two-threshold configuration (Krites §3.1). Because BM25 and
// cosine scores live on different scales, classification uses a relative
// confidence (score/top1) combined with absolute floors that only bind on
// the bounded cosine scale (BM25 scores are typically >> 1 and never trip
// the absolute guards).
type Zones struct {
	TauHigh float64 // relative confidence >= TauHigh -> Hit
	TauLow  float64 // relative confidence >= TauLow  -> Grey
	AbsHigh float64 // absolute score floor for Hit (top1 >= AbsHigh)
	AbsLow  float64 // absolute score floor for Grey (top1 >= AbsLow)
}

// Default returns the tuned default thresholds.
func Default() Zones {
	return Zones{TauHigh: 0.8, TauLow: 0.55, AbsHigh: 0.7, AbsLow: 0.45}
}

// Classify maps (score, top1) to a Zone. Non-positive scores are always Miss
// (negative cosine or empty result set).
func (z Zones) Classify(score, top1 float64) Zone {
	if top1 <= 0 || score <= 0 {
		return Miss
	}
	switch {
	case top1 < z.AbsLow:
		return Miss
	case score/top1 >= z.TauHigh && top1 >= z.AbsHigh:
		return Hit
	case score/top1 >= z.TauLow && top1 >= z.AbsLow:
		return Grey
	default:
		return Miss
	}
}
