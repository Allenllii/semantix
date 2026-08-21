# Spec: load-aware prefetch gating (Issue 271)

The prefetch planner currently considers transition confidence, hit/waste
feedback, task count, and cost. This addendum adds system load and useful wait
window as independent gates without changing the frozen `Prefetcher.Plan`
interface.

## Contract evolution

`Prefetcher.Plan([]string)` remains source compatible. Implementations may also
implement the optional `LoadAwarePrefetcher` interface:

```go
type LoadHint struct {
    ConcurrencyUsed  int
    ConcurrencyLimit int
    WaitWindow       time.Duration
    TaskEstimate     time.Duration
}

type LoadAwarePrefetcher interface {
    PlanWithLoad([]string, LoadHint) (PlanDecision, error)
}
```

A zero or incomplete hint means "unknown", not "busy": the implementation
falls back to the existing `Plan` behavior. This preserves cold-start and old
adapter behavior.

## Gate order

The load-aware path evaluates these rules before candidate ranking:

1. when both slot values are known and `used / limit >= 0.8`, return no tasks
   with reason `load_saturated`;
2. when both durations are known and `WaitWindow < TaskEstimate`, return no
   tasks with reason `window_too_short`;
3. otherwise run the existing planner and return `candidate` or
   `no_candidate`.

Budget remains authoritative. `halt_prefetch` and `hard_stop` clear any
candidate and expose `budget:<action>` as the round reason. A skipped task was
never executed, so it must not emit `PrefetchWaste`; load skip counters and the
persisted `RoundPlan.PrefetchReason` are the observability surface.

## Harness signals

- slot use is the largest scheduled parallel group, capped by the scheduler's
  configured slot limit;
- wait window is the preceding provider stream duration;
- task estimate is the preceding injection warm duration.

The first stream has no history and keeps the legacy allow behavior. Explicit
load/window/budget skip reasons block the next real injection warm;
`no_candidate`, planner errors, and legacy callbacks remain fail-open so this
change does not redefine candidate policy. Measurements are process-local and
do not add a kernel dependency on the harness.

## Acceptance

- high slot occupancy and short windows independently produce an empty plan;
- zero hints retain the existing plan;
- normal candidates still reach `RoundPlan.PrefetchIDs`;
- the actual harness warm is not started after a skip decision;
- load-aware calls expose reason counts without creating false waste events;
- `go test -race ./kernel/prefetch ./kernel/sched` and focused harness tests
  pass.

## Non-goals

This change does not predict provider-wide fleet load, train a learned gate,
cancel an already-running warm, change the 80% default dynamically, or treat a
skip as hit/waste feedback.
