# Spec: prefetch feedback decay (Issue 255)

Issue 62 defines the offline Planner MVP. This addendum defines the online
`MatrixPrefetcher` hit/waste feedback semantics without changing the frozen
`Prefetcher` interface.

## Problem

Hit and waste were previously updated as independent positive-only EWMAs. A
candidate with an old hit EWMA above `1 / WasteHitLimit` could therefore never
be demoted: repeated waste approached 1 while the hit value never aged.

## Feedback stream

For decay factor `alpha`, every observation updates both sides of one binary
feedback stream:

```text
hit:   hit = EWMA(hit, 1, alpha); waste = EWMA(waste, 0, alpha)
waste: hit = EWMA(hit, 0, alpha); waste = EWMA(waste, 1, alpha)
```

This mirrors the complementary EWMA already consumed by `kernel/evolve`.
Unobserved candidates keep their current state.

## Decision rule

The existing N12 rule remains authoritative:

- zero waste is never demoted;
- waste with zero hits is demoted;
- otherwise demote when `waste / hit > WasteHitLimit` (default `3.0`).

The strict `>` comparison and public configuration defaults do not change.
Repeated waste must eventually demote a formerly healthy candidate, while
later hits may restore it once the ratio returns below the limit.

## Acceptance

- A candidate with established hit history is demoted after sustained waste.
- One waste observation does not demote an otherwise healthy candidate.
- Sustained hits can recover a candidate with prior waste history.
- `go test -race ./kernel/prefetch` passes.

## Non-goals

This change does not add a sliding window, persist feedback, tune the default
threshold, alter transition probabilities, or change scheduling interfaces.
