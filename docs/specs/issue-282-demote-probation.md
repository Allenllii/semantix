# Spec: prefetch demote probation (Issue 282)

Issue 255 defined the complementary hit/waste feedback stream and Issue 254 the
`Epsilon` escape from the `MinConf` exclusion. This addendum defines how a
*demoted* candidate re-enters `MatrixPrefetcher.Plan` without changing the
frozen `Prefetcher` interface.

## Problem

Demotion is an absorbing state. `Plan` skips demoted candidates, so a demoted
key is never proposed, never reaches the harness feedback path
(`harness/semantix/evolution.go` emits `PrefetchHit`/`PrefetchWaste` only for
planned targets), and therefore never receives another `ObserveHit` or
`ObserveWaste`. Both EWMAs freeze and `waste / hit` stays above
`WasteHitLimit` forever.

Issue 255 states that "later hits may restore it once the ratio returns below
the limit". That promise is unreachable through the production feedback path:
the only test covering it (`TestWastePenaltyRecoversAfterSustainedHits`) calls
`ObserveHit` directly, which the running system can never do for a demoted key.
The arithmetic recovers; the system does not.

The consequence is a monotone decline in prefetch coverage: one bad window
permanently removes a transition edge, and edges are never returned.

## Probation probe

`Config.ProbationInterval` (default 10) admits one demoted candidate every Nth
eligible `Plan` call for that transition table:

- a `Plan` call is *eligible* when the last tool has a learned transition
  table; calls that return early (no history, unknown tool) do not advance the
  counter;
- **the counter is keyed by the last tool**, not global. The demoted set is
  rebuilt per `last` from `m.bigrams[last]`, so a single global counter would
  put each tool in its own residue class: under any periodic interleaving whose
  period shares a factor with `ProbationInterval` — a two-tool alternation with
  the default N=10, for instance — whole transition tables would never receive
  a probation round and the absorbing state would survive verbatim;
- on every Nth eligible call for that tool, one candidate excluded by demotion
  is admitted as a probe. The slot rotates through that tool's demoted set, so
  a leader that never recovers cannot starve the others out of the channel;
- the rotation order is probability descending, **with the candidate key as
  tie-break**: `sort.Slice` is not stable and map iteration order is
  randomized, so probability alone would make the schedule nondeterministic
  when candidates tie;
- **probation ignores `MinConf`.** The `Epsilon` escape of Issue 254
  deliberately skips demoted candidates, so gating probation on `MinConf` too
  would leave demoted *and* below-`MinConf` candidates unreachable by both
  probes. That state is reachable in practice: probation wastes feed
  `prefetch_waste` into evolve, which raises `PrefetchConf`, which can push a
  demoted candidate below `MinConf`;
- the probe occupies one of the `TopK` slots: normal candidates are capped at
  `TopK - 1` on a probation round, and the probe is appended last so healthy
  candidates keep priority. With `TopK == 1` the probation round therefore
  spends its only slot on the probe — one round in `ProbationInterval` is the
  price of recovery being possible at all with a single slot;
- the probe still respects `MaxCost`; it is dropped when the budget cannot fund
  one `BaseCost`.

Following the other knobs in `Config`, a zero value selects the default. A
negative `ProbationInterval` disables probation and restores the previous
absorbing behaviour.

## Composition with the Epsilon escape

The two mechanisms cover disjoint exclusion reasons and together cover the
union:

| candidate state | channel |
|---|---|
| demoted (any probability) | probation (this spec) |
| below `MinConf`, not demoted | `Epsilon` escape (Issue 254) |

Probation appends its task before the `Epsilon` block, whose guard is
`len(tasks) == 0`. A round that already produced a probation probe therefore
never also fires the epsilon probe, so the two can never double-book a slot or
exceed `MaxCost`.

## Recovery semantics

The probe changes nothing about `demoted()`. It only restores the *input* the
decision rule needs:

- probe hits → `ObserveHit` → complementary decay lifts `hit`, ages `waste`;
  once `waste / hit` falls back below `WasteHitLimit` the candidate leaves
  probation on its own and competes normally again;
- probe wastes → `ObserveWaste` → the ratio stays above the limit and the
  candidate remains demoted until the next probation round.

Recovery therefore costs at most one slot per `ProbationInterval` rounds per
tool and requires sustained evidence, not a single lucky hit.

## Determinism

The probe is driven by a counter and a totally-ordered rotation, not by
randomness: two prefetchers fed the same call sequence produce identical plans.
This is deliberate and differs from the `Epsilon` escape, which is
probabilistic.

## Acceptance

- A demoted candidate is proposed again within `ProbationInterval` eligible
  `Plan` calls **for its own last tool**, using only `Plan` and feedback for
  planned targets — no direct `ObserveHit` on an unplanned key.
- Under two-tool alternation and four-tool round-robin, every tool's demoted
  successors are probed and recover; none is starved.
- Sustained probe hits fully restore the candidate; sustained probe wastes keep
  it demoted.
- A demoted candidate below `MinConf` is still probed.
- The rotation is identical across runs when candidates tie on probability.
- The probe never proposes a writer and never exceeds `TopK` or `MaxCost`.
- A negative `ProbationInterval` reproduces the previous absorbing behaviour.
- `go test -race ./kernel/prefetch` passes.

## Non-goals

This change does not alter `demoted()`, the `WasteHitLimit` default, the
transition probabilities, the `Prefetcher` interface, the `Epsilon` escape, or
persistence of feedback across restarts.
