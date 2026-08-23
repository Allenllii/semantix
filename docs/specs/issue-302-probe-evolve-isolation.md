# Issue #302: isolate probe cost from global evolution

## Status

Accepted for implementation.

## Problem

Epsilon escape (#254) and demotion probation (#282) deliberately execute
low-confidence work. Their expected early outcome is therefore waste. Today a
probe is indistinguishable from an ordinary `PrefetchTask`, and its terminal
`PrefetchWaste` event enters the same `prefetch_waste` signal path as genuine
prefetch exploitation. The evolution engine interprets exploration cost as
evidence that the global prefetch confidence floor should rise, so one key's
recovery experiment tightens every candidate's gate.

Probe cost remains real usage and must remain observable. Only its influence on
the global `PrefetchConf` control loop is incorrect.

## Root cause

Probe intent is lost at each boundary:

1. `MatrixPrefetcher.Plan` returns an ordinary `PrefetchTask`.
2. the prefetch-to-scheduler adapter reduces every task to an undifferentiated
   key;
3. the agent records a warmed result without the plan's probe identity;
4. `PrefetchHitPayload` and `PrefetchWastePayload` expose only result targets;
5. `EvolutionLoop.observe` always calls both per-key feedback and global
   `RecordSignal`.

The defect is exploration/exploitation accounting, not a bad evolution step
size. Changing global evolve coefficients would merely hide the coupling.

## Requirements

1. `PrefetchTask` gains additive `Probe bool` metadata. Both Epsilon escape and
   probation tasks set it; ordinary candidates leave it false.
2. Probe keys survive the prefetch adapter, scheduler round plan, agent gate,
   warmed-result settlement, bridge, and kernel event payload.
3. `PrefetchHitPayload` and `PrefetchWastePayload` gain additive
   `probe_targets,omitempty`; old payloads remain valid and retain existing
   behavior.
4. Probe hit/waste events continue to update the prefetcher's local per-key
   feedback path and remain in usage/event reporting.
5. Probe hit/waste events do not call the global evolve engine's
   `RecordSignal`, do not change `PrefetchConf`, and do not emit an
   `EvolutionTick` solely because of that outcome.
6. Ordinary hit/waste events continue to drive global evolution unchanged.
7. Budget suppression clears both planned IDs and probe IDs so stale probe
   metadata cannot leak into a blocked warm-up.
8. Wire changes are documented in `docs/events.md`.

## Design

### Planning metadata

`PrefetchTask.Probe` identifies work admitted only to explore an otherwise
excluded candidate. `AsLoadAwarePlanFunc` returns all task keys as `IDs` and
the subset with `Probe=true` as `ProbeIDs`. `RuleDecider` copies the subset to
`RoundPlan.PrefetchProbeIDs`.

The legacy `PrefetchPlanFunc` remains unchanged: it cannot express probe
metadata and therefore produces no probe IDs.

### Agent and event propagation

The agent's per-turn prefetch gate stores a defensive copy of planned probe
keys. When a warm result is created, those keys are copied to the result and
then to `Bridge.RecordPrefetch`. The bridge emits them as canonical,
deduplicated `probe_targets` on the existing terminal hit/waste event. This is
an additive payload field; event kinds and numeric IDs do not change.

`probe_targets` describes which scheduler candidates made the warm speculative.
It is intentionally separate from `targets`, which continues to describe the
canonical injected slice identities.

### Evolution accounting

`EvolutionLoop.observe` always performs existing local `ObserveHit` or
`ObserveWaste` feedback for event `targets`. When `probe_targets` is non-empty,
it returns immediately after local feedback. Thus exploration remains capable
of recovering/demoting local keys and remains visible as cost, but cannot
change the global engine snapshot.

An event without `probe_targets` follows the existing exploitation path,
including `RecordSignal`, tuner application, and `EvolutionTick` emission.

## Compatibility

- Go struct fields are additive.
- JSON `probe_targets` is omitted for ordinary events, so existing serialized
  payloads are byte-shape compatible.
- Consumers that do not know the field continue to decode payloads normally.
- No configuration defaults, evolve coefficients, or event kind numbers
  change.

## Acceptance criteria

- Epsilon and probation planner tests assert `Probe=true`; ordinary candidates
  assert `Probe=false`.
- Adapter and scheduler tests prove probe IDs survive and budget suppression
  clears them.
- Agent/bridge tests prove `probe_targets` reaches hit/waste payloads.
- Evolution tests prove repeated probe waste leaves `PrefetchConf` unchanged,
  emits no tick, and still invokes local waste feedback.
- The paired ordinary-waste test proves `PrefetchConf` still rises and emits a
  tick.
- `go test ./kernel/prefetch ./kernel/sched ./harness/agent ./harness/semantix`
  passes.
- `go test ./...` and race-enabled affected-package tests pass where the host
  toolchain supports the race detector.

