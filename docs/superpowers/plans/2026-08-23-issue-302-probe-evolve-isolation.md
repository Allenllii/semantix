# Issue #302 Probe Evolution Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve probe cost and local learning while preventing Epsilon/probation outcomes from changing the global prefetch confidence control.

**Architecture:** Carry additive probe metadata from `PrefetchTask` through scheduler and agent settlement into existing prefetch events. Split `EvolutionLoop.observe` after local feedback: probe outcomes return before global `RecordSignal`, while ordinary outcomes retain the current path.

**Tech Stack:** Go, kernel event JSON contracts, synchronous event bus, Go testing/race detector

**Spec:** `docs/specs/issue-302-probe-evolve-isolation.md`

## Global Constraints

- Existing event kind numbers and production evolve coefficients do not change.
- `probe_targets` is additive and omitted for ordinary events.
- Probe outcomes still update local per-key feedback and cost/event reporting.
- Only probe outcomes bypass global `RecordSignal`.
- Legacy prefetch planner callbacks remain source compatible.

---

### Task 1: Mark and transport probe plans

**Files:**
- Modify: `kernel/prefetch/prefetch.go`
- Modify: `kernel/prefetch/matrix.go`
- Modify: `kernel/prefetch/escape_test.go`
- Modify: `kernel/prefetch/probation_test.go`
- Modify: `kernel/prefetch/planfunc.go`
- Modify: `kernel/prefetch/planfunc_test.go`
- Modify: `kernel/sched/sched.go`
- Modify: `kernel/sched/rule_decider.go`
- Modify: `kernel/sched/rule_decider_test.go`

**Interfaces:**
- Produces: `PrefetchTask.Probe bool`, `sched.PrefetchPlanResult.ProbeIDs []string`, `sched.RoundPlan.PrefetchProbeIDs []string`
- Consumes: existing `PrefetchTask.Key` and load-aware planning callbacks

- [ ] **Step 1: Add failing planner and adapter assertions**

Assert Epsilon/probation tasks have `Probe=true`, healthy tasks remain false,
and `AsLoadAwarePlanFunc` returns the probe subset separately.

- [ ] **Step 2: Run focused tests and confirm RED**

Run: `go test ./kernel/prefetch ./kernel/sched`

Expected: compile failures for missing `Probe`, `ProbeIDs`, and
`PrefetchProbeIDs` fields.

- [ ] **Step 3: Implement minimal additive metadata flow**

Add the three fields, mark both probe construction sites, collect probe keys in
the adapter, copy them in `RuleDecider`, and clear them with budget suppression.

- [ ] **Step 4: Run focused tests and confirm GREEN**

Run: `go test ./kernel/prefetch ./kernel/sched`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add kernel/prefetch kernel/sched
git commit -m "feat(prefetch): preserve probe identity in round plans"
```

### Task 2: Emit additive probe targets

**Files:**
- Modify: `kernel/event/event.go`
- Modify: `harness/agent/prefetch_feedback.go`
- Modify: `harness/agent/prefetch_feedback_test.go`
- Modify: `harness/agent/execute_batch.go`
- Modify: `harness/agent/agent.go`
- Modify: `harness/semantix/bridge.go`
- Modify: `harness/semantix/closed_loop_test.go`
- Modify: `docs/events.md`

**Interfaces:**
- Consumes: `sched.RoundPlan.PrefetchProbeIDs`
- Produces: `PrefetchHitPayload.ProbeTargets`, `PrefetchWastePayload.ProbeTargets`, and `Bridge.RecordPrefetch(..., probeTargets []string)`

- [ ] **Step 1: Add failing event-propagation tests**

Arm an agent with probe IDs, settle both hit and waste results, decode the
events, and assert canonical `probe_targets`. Assert ordinary payload JSON omits
the field.

- [ ] **Step 2: Run focused tests and confirm RED**

Run: `go test ./harness/agent ./harness/semantix`

Expected: compile failures for missing payload fields and updated agent method.

- [ ] **Step 3: Implement the propagation path**

Store defensive probe-ID copies in the gate and warmed result, pass them to the
bridge, canonicalize/deduplicate them, and populate the additive event fields.
Document `probe_targets` in `docs/events.md`.

- [ ] **Step 4: Run focused tests and confirm GREEN**

Run: `go test ./harness/agent ./harness/semantix`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add kernel/event harness/agent harness/semantix docs/events.md
git commit -m "feat(events): carry speculative probe targets"
```

### Task 3: Split local and global feedback accounting

**Files:**
- Modify: `harness/semantix/evolution.go`
- Modify: `harness/semantix/evolution_test.go`

**Interfaces:**
- Consumes: `PrefetchHitPayload.ProbeTargets` and `PrefetchWastePayload.ProbeTargets`
- Produces: probe-local feedback without global evolve signal

- [ ] **Step 1: Add the failing causal regression**

Emit enough probe-waste events to move `PrefetchConf` under the old path. Assert
the parameter remains at its initial value, no tuner/tick fires, and a capture
prefetcher still receives `ObserveWaste`. Pair it with ordinary waste that must
still move the parameter.

- [ ] **Step 2: Run focused test and confirm RED**

Run: `go test ./harness/semantix -run 'TestEvolutionLoop(ProbeWasteIsLocalOnly|RealWasteStillEvolves)$' -count=1`

Expected: probe test FAIL because `PrefetchConf` changes or a tick is emitted.

- [ ] **Step 3: Return before global signal for probes**

Decode `probe_targets`, retain existing local feedback, and return immediately
after it when the probe subset is non-empty.

- [ ] **Step 4: Run affected verification**

Run: `go test ./kernel/prefetch ./kernel/sched ./harness/agent ./harness/semantix`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add harness/semantix/evolution.go harness/semantix/evolution_test.go
git commit -m "fix(evolve): keep probe outcomes out of global confidence"
```

### Task 4: Repository verification and PR

**Files:**
- No production files added.

**Interfaces:**
- Consumes: completed Tasks 1–3
- Produces: verified branch and PR closing #302

- [ ] **Step 1: Run formatting and static checks**

Run: `gofmt -w <changed Go files>` and `go vet ./kernel/prefetch ./kernel/sched ./harness/agent ./harness/semantix`

Expected: exit 0.

- [ ] **Step 2: Run affected and repository tests**

Run: `go test ./kernel/prefetch ./kernel/sched ./harness/agent ./harness/semantix` and `go test ./...`

Expected: PASS, with any unrelated baseline failures reported exactly.

- [ ] **Step 3: Run race tests where supported**

Run: `go test -race ./kernel/prefetch ./kernel/sched ./harness/agent ./harness/semantix`

Expected: PASS on a CGO/race-capable host.

- [ ] **Step 4: Review, push, and create PR**

Run `git diff upstream/main...HEAD --check`, push
`codex/issue-302-probe-evolve-isolation` to `origin`, and create a PR against
`Gnosil/semantix:main` whose body contains `Closes #302` and exact validation
results.
