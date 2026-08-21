# U43 Prefetch/Evolve Closed Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire prefetch hit/waste feedback through online evolution into next-round scheduler and prefetch decisions, persist searchable evidence, and publish the 20-session evolution report.

**Architecture:** `harness/semantix` remains the composition root. Kernel packages expose narrow, race-safe tuning methods; the harness owns prefetch-result lifecycle and emits exactly one terminal event per warmed result; an evolution loop subscribes to the shared bus, applies immutable parameter snapshots, and emits full `EvolutionTick` snapshots.

**Tech Stack:** Go 1.26, kernel synchronous event bus, JSONL, Go race detector, Markdown/Mermaid reports.

**Spec:** https://github.com/Gnosil/semantix/issues/194#issuecomment-5327218370

## Global Constraints

- Base branch is `upstream/harness-integration`.
- Preserve existing event Kinds and payloads; no offline retraining.
- Parameters affect only RuleDecider behavior threshold and prefetch confidence.
- A warmed result settles exactly once as Hit or Waste.
- Dynamic parameters take effect on the next round, not midway through a round.
- Full repository must pass `go build ./...` and `go test -race ./...`.

---

### Task 1: Race-safe dynamic tuning

**Files:**
- Modify: `kernel/sched/rule_decider.go`
- Test: `kernel/sched/rule_decider_test.go`
- Modify: `kernel/prefetch/matrix.go`
- Test: `kernel/prefetch/matrix_test.go`

**Interfaces:**
- Produces: `(*sched.RuleDecider).ApplyEvolution(float64) error`
- Produces: `(*prefetch.MatrixPrefetcher).ApplyEvolution(float64) error`

- [ ] Add tests showing the same observed history/transition produces a different next-round decision after tuning.
- [ ] Run targeted tests and confirm missing-method compilation failures.
- [ ] Implement validation, clamping, and locked config replacement.
- [ ] Run targeted tests and race tests.

### Task 2: Exactly-once warmed-result lifecycle

**Files:**
- Modify: `harness/semantix/bridge.go`
- Modify: `harness/agent/agent.go`
- Modify: `harness/agent/sampling_request.go`
- Modify: `harness/agent/run_loop.go`
- Test: `harness/agent/prefetch_feedback_test.go`

**Interfaces:**
- Produces: `semantix.InjectResult{Text string, Targets []string}`
- Produces: Agent helpers `storePrefetch`, `takePrefetch`, and `wastePrefetch`.

- [ ] Add tests for consume-hit, synchronous-block waste, replacement waste, expiry waste, and duplicate settlement prevention.
- [ ] Run tests and confirm failures caused by missing lifecycle methods.
- [ ] Return canonical slice IDs with injected text and replace the string pointer with an identified result.
- [ ] Emit existing Hit/Waste payloads through the shared kernel bus and clear with atomic swap/CAS semantics.
- [ ] Run agent and semantix tests under `-race`.

### Task 3: Evolution loop and tick persistence

**Files:**
- Create: `harness/semantix/evolution.go`
- Test: `harness/semantix/evolution_test.go`
- Modify: `harness/semantix/bridge.go`
- Modify: `harness/semantix/sink.go`
- Modify: `harness/boot/boot.go`

**Interfaces:**
- Consumes: dynamic tuning methods from Task 1 and shared `event.Bus`.
- Produces: `EvolutionLoop` subscription with monotonic epoch and full JSON parameter snapshots.

- [ ] Add an integration test that emits feedback, observes one real parameter change, verifies both consumers change on the next decision, and sees one tick.
- [ ] Confirm RED.
- [ ] Implement bus subscription, before/after parameter comparison, atomic publication, fail-open consumer application, and tick emission.
- [ ] Subscribe the JSONL sink to kernel-native events so C4 events share the real session file and flush on close.
- [ ] Run focused and race tests.

### Task 4: Searchable event projection

**Files:**
- Modify: `kernel/ingest/jsonl.go` or the existing JSONL extractor selected by tests.
- Test: matching `kernel/ingest/*_test.go`
- Test: `cmd/semantix/search_test.go` or existing CLI E2E suite.

**Interfaces:**
- Consumes: existing event JSONL wire format.
- Produces: deterministic textual slices for prefetch hit, prefetch waste, and evolution tick.

- [ ] Add an E2E test writing all three events and asserting each query finds its session.
- [ ] Confirm RED.
- [ ] Implement the smallest stable text projection without changing Kind values.
- [ ] Run ingest and CLI search tests.

### Task 5: Evolution curve evidence

**Files:**
- Create: `scripts/evolution-curve/main.go`
- Create: `docs/reports/data/agile2-evolution-curve/sessions.jsonl`
- Create: `docs/reports/data/agile2-evolution-curve/summary.csv`
- Create: `docs/reports/agile2-evolution-curve.md`

**Interfaces:**
- Consumes: the production bus/evolution/prefetch path.
- Produces: at least 20 independent session records and a reproducible report.

- [ ] Add a deterministic runner using the production closed-loop components and fixed task-family sequence.
- [ ] Run it for 20+ sessions and preserve raw JSONL plus CSV.
- [ ] Compute hit rate, cost, first-five/last-five aggregates, slopes, parameter snapshots, and task success.
- [ ] Render Markdown tables and Mermaid trend charts, and mark each DoD gate pass/fail from observed data.

### Task 6: Verification and delivery evidence

**Files:**
- Create: `artifacts/issue-194/DIFF_FILE.patch`
- Create: `artifacts/issue-194/VERIFICATION.txt`
- Create: `artifacts/issue-194/ROLLBACK.sh`
- Create: `artifacts/issue-194/MODIFIED_FILE`

- [ ] Run targeted packages, `go build ./...`, and `go test -race ./...` and capture literal outputs/statuses.
- [ ] Save the branch diff, a manifest copy, verification commands/results, and executable rollback script.
- [ ] Test rollback on a separate copy/worktree and prove baseline behavior is restored while leaving this worktree modified.
- [ ] Reopen all four artifacts, review `git diff --check`, and update Issue #194 with DoD evidence links/results.
