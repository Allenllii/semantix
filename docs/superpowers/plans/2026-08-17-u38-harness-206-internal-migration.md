# U38 Harness — Adopt #206 internal/ vendor + re-port semantix integration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the flat `harness/` vendor on `harness-integration` with PR #206's `internal/`-layout vendor (public `sdk/` + retained 977-test suite), then re-port the four features that were stacked on the flat vendor — the semantix↔kernel bridge, the U39 reuse panel, U38 task-time, and issue-191 resource orchestration — onto the new layout so nothing is lost.

**Architecture:** Base the integration branch on `upstream/harness-integration` (keeps the enhanced `kernel/sched` + `kernel/event` ResourceCatalog types that orchestration needs, which live *outside* `harness/`). Swap **only** the `harness/` tree + `cmd/semantix-agent` to #206's version. The semantix integration does not exist in #206 and must be **created**, not edited: a new `harness/internal/semantix` bridge package plus injection points added to `internal/boot`, `internal/agent`, `internal/cli`, `internal/event`. Every seam is a copy-from-flat-source + import-path rewrite (`semantix/harness/<pkg>` → `semantix/harness/internal/<pkg>`), verified against the source's own unit tests.

**Tech Stack:** Go 1.26 (module `semantix`, single module), Bubble Tea v2 TUI (charmbracelet), kernel packages (`kernel/{slice,bm25,inject,usage,zone,event,sched}`) already present on the base branch.

**Spec:** `docs/specs/h2h3-resource-orchestration.md` §3 (U38 vendor); issue #189 (vendor), #190 (U39 reuse panel), #177 (task-time), #191 (orchestration). Source of truth for the port is the **flat lineage on `upstream/harness-integration`** — the four merged commits `60f25ab` (vendor), `aa10a6f` (U39), `5102353` (task-time), `f038c2f` (orchestration).

## Global Constraints

- **Single Go module** `semantix`; no new nested `go.mod` except the standalone `harness/sdk/go/go.mod` that ships with #206. Do not add a `reasonix` module.
- **Import path prefix in the new layout:** `semantix/harness/internal/<pkg>` (confirmed `harness/internal/agent/agent.go:17`). The bridge package is placed at `harness/internal/semantix` (package name `semantix`, import `semantix/harness/internal/semantix`) so it can import the other internal packages and be imported by them.
- **Do NOT re-create three deleted files:** `harness/semantix/protocol.go` and `harness/semantix/inject.go` (deleted in `aa10a6f` — old subprocess CLI client; replaced by in-process kernel reads) and `harness/semantix/sched.go` (deleted in `f038c2f` — fork-side scheduler; replaced by `kernel/sched`).
- **Kernel is shared and already correct on the base branch** — do not modify `kernel/*`. Orchestration's kernel additions (`kernel/sched` `RoundInput`/`RoundPlan`/enhanced `Decider`, `kernel/event` `ResourceCatalog`/`ResourceModel`/`ResourceBudget`) are present on `harness-integration` and absent on `pr-206-head`; basing on `harness-integration` preserves them.
- **Verification bar (issue #189 checklist):** `go build ./...` green; `go test ./... -race` green (must not regress kernel/gateway tests); `cmd/semantix-agent` smoke (`--version`/`--help`, and a real `-p` conversation + tool call if credentials available).
- **Branding/contract strings unchanged** (reasonix.toml config names, ACP protocol id, manifest kind, machine-session v1, release asset names) — the vendor swap keeps #206's strings; do not rename them.

---

## File Structure

New/created in the `internal/` layout (the semantix integration — none of this exists in #206):

- `harness/internal/semantix/bridge.go` — the kernel-wiring bridge (`Bridge`, `Config`, `NewBridge`, `Sink`, `SetLabel`, `Enabled`, `InjectEnabled`, `Inject`, `Reuse`, `Events`, `Close`). Leaf-ish: imports `semantix/harness/internal/event` + `semantix/kernel/{bm25,event,inject,slice,usage,zone}`.
- `harness/internal/semantix/reuse.go` — `ReuseSummary` (JSON wire contract agent↔cli) + `Line()`.
- `harness/internal/semantix/sink.go` — `HarnessSink`/`NewHarnessSink` (event→kernel session JSONL mirror).
- `harness/internal/semantix/{bridge_test.go,reuse_test.go}` — ported unit tests.

Modified in #206's internal tree (add injection points):

- `harness/internal/event/event.go` — add `NoticeCodeSemantixReuse = "semantix_reuse"`.
- `harness/internal/agent/{agent.go,services.go,run_loop.go,sampling_request.go,turnruntime.go,reuse.go,taskstate.go,turn_phase.go,resources.go,tier.go,execute_batch.go}` — bridge field, per-turn gather, injection, reuse emit, task clock, resource plane.
- `harness/internal/cli/{reuse_panel.go,chat_tui.go,theme.go,resource_gauge.go}` — panel render, theme success color, gauge seam.
- `harness/internal/boot/boot.go` — construct bridge, wrap sink, SetLabel, pass agent Options.
- `harness/internal/config/config.go` — `SemantixConfig.CostInputPriceUSD`/`CostCachePriceUSD` (if not already present in #206's config).
- `go.mod` / `go.sum` — reconciled union of kernel + #206 harness deps.

---

## Task 0: Foundation branch — #206 vendor on the harness-integration kernel

**Files:**
- Branch: `hi-rebuild-206` (already exists locally: `upstream/harness-integration` with one commit removing flat `harness/` + `cmd/semantix-agent`).
- Overlay from `pr-206-head`: `harness/`, `cmd/semantix-agent/`, `go.mod`, `go.sum`.

**Interfaces:**
- Produces: a branch whose `harness/` is #206's internal/ vendor, whose `kernel/`, `gateway/`, `cmd/semantix`, docs are `harness-integration`'s (unchanged), and that builds + passes #206's vendor tests. Everything downstream is added on top of this.

- [ ] **Step 1: Confirm the starting branch state**

Run: `git -C /d/semantix log --oneline -1 hi-rebuild-206` and `git -C /d/semantix ls-files harness/ | wc -l`
Expected: HEAD is the "drop flat U38 vendor lineage" commit; `harness/` file count is 0.

- [ ] **Step 2: Overlay #206's harness tree + entry + module files**

```bash
cd /d/semantix
git checkout hi-rebuild-206
git checkout pr-206-head -- harness cmd/semantix-agent go.mod go.sum
```

- [ ] **Step 3: Reconcile modules and verify the vendor builds**

```bash
cd /d/semantix
go mod tidy
go build ./harness/... ./cmd/semantix-agent/...
```
Expected: build succeeds. If `go mod tidy` drops/edits a require the kernel needs, inspect `git diff go.mod` and restore the kernel requires; re-run.

- [ ] **Step 4: Run #206's retained vendor tests (baseline green)**

Run: `go test ./harness/internal/... 2>&1 | tail -40`
Expected: pass (this is #206's own 977-test suite; record any pre-existing failures here as the baseline — the two vendor-adaptation nits noted in #206's PR description, `eventwire` desktop contract + `TOOL_CONTRACT.md`, are known and may be absent since we did not vendor desktop).

- [ ] **Step 5: Commit the foundation**

```bash
git add -A
git commit -m "feat(harness): adopt #206 internal/ vendor on harness-integration kernel (Issue #189)

Swaps the flat harness/ vendor for #206's internal/-encapsulated vendor
(public sdk/ + retained tests) while keeping harness-integration's kernel
(enhanced kernel/sched + kernel/event ResourceCatalog needed by orchestration)."
```

---

## Task 1: Port the semantix bridge package (leaf)

**Files:**
- Create: `harness/internal/semantix/{bridge.go,reuse.go,sink.go,reuse_test.go}` (+ `bridge_test.go` if present in source)
- Source: `git show upstream/harness-integration:harness/semantix/<file>`

**Interfaces:**
- Produces (package `semantix`, import `semantix/harness/internal/semantix`):
  - `type Config struct { Enabled bool; Binary string; Inject bool; Budget int; SessionsDir, ProjectDir string; CostMissUSD, CostHitUSD float64 }`
  - `func NewBridge(cfg Config) *Bridge`
  - `func (b *Bridge) Sink(inner event.Sink) event.Sink`
  - `func (b *Bridge) SetLabel(label string)`
  - `func (b *Bridge) Enabled() bool` / `func (b *Bridge) InjectEnabled() bool`
  - `func (b *Bridge) Inject(ctx context.Context, query string) string`
  - `func (b *Bridge) Reuse(ctx context.Context, query string) ReuseSummary`
  - `func (b *Bridge) Events() kernelevent.Bus`
  - `func (b *Bridge) Close() error`
  - `type ReuseSummary struct { Hits int; SavingsUSD float64; Sources []string; TaskElapsedSeconds float64; TaskComplete bool }` (keep JSON tags verbatim — this is the agent↔cli wire contract) + `func (s ReuseSummary) Line() string`
  - `type HarnessSink struct` + `func NewHarnessSink(dir, sessionID, firstUserText string) (*HarnessSink, error)`
- Consumes: `semantix/harness/internal/event` (`event.Sink`, `event.Event`, event kinds); kernel `semantix/kernel/{bm25,event,inject,slice,usage,zone}` (already on branch).

- [ ] **Step 1: Copy the three source files + tests into the new package dir**

```bash
cd /d/semantix
mkdir -p harness/internal/semantix
for f in bridge.go reuse.go sink.go reuse_test.go; do
  git show upstream/harness-integration:harness/semantix/$f > harness/internal/semantix/$f
done
```
Note: this is the `aa10a6f`+`f038c2f` in-process version (already free of the deleted protocol.go/inject.go/sched.go). Do not fetch those deleted files.

- [ ] **Step 2: Rewrite the harness import path (event) to internal/**

In all files under `harness/internal/semantix/`, replace import `"semantix/harness/event"` → `"semantix/harness/internal/event"`. Kernel imports (`semantix/kernel/...`) are unchanged. Verify no other `semantix/harness/<pkg>` (non-internal) imports remain:
Run: `grep -rnE '"semantix/harness/(?!internal|sdk)' harness/internal/semantix/ || echo CLEAN`

- [ ] **Step 3: Build the package**

Run: `go build ./harness/internal/semantix/`
Expected: compiles. If `event.Event`/kind names differ in #206's `internal/event` vs the flat `harness/event`, reconcile field/const names against `git show pr-206-head:harness/internal/event/event.go` (the vendored event type should be identical; only the reuse Notice code is added in Task 2).

- [ ] **Step 4: Run the ported unit tests**

Run: `go test ./harness/internal/semantix/ -v 2>&1 | tail -30`
Expected: PASS (`reuse_test.go` covers `ReuseSummary.Line`/`topSources`/formatters; if it references `NoticeCodeSemantixReuse` it belongs to Task 2 — if so, land Task 2 first or skip that test until then).

- [ ] **Step 5: Commit**

```bash
git add harness/internal/semantix
git commit -m "feat(harness): port semantix↔kernel bridge into internal/ (Issue #189/#190)"
```

---

## Task 2: Add the reuse Notice code to internal/event

**Files:**
- Modify: `harness/internal/event/event.go` (add one const near the other `NoticeCode*` constants)
- Source reference: `git show upstream/harness-integration:harness/event/event.go` line ~559

**Interfaces:**
- Produces: `event.NoticeCodeSemantixReuse = "semantix_reuse"` — the shared code coupling agent emission (Task 5) to cli rendering (Task 6).

- [ ] **Step 1: Locate the NoticeCode block in #206's event.go**

Run: `grep -nE 'NoticeCode' harness/internal/event/event.go | head`
Expected: an existing set of `NoticeCode<X> = "..."` constants; note the exact grouping/const block.

- [ ] **Step 2: Add the constant**

Add alongside the existing NoticeCode constants:
```go
// NoticeCodeSemantixReuse marks the per-turn semantix reuse-panel notice
// whose Detail is a JSON semantix.ReuseSummary.
NoticeCodeSemantixReuse = "semantix_reuse"
```

- [ ] **Step 3: Build + run event tests**

Run: `go build ./harness/internal/event/ && go test ./harness/internal/event/`
Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add harness/internal/event/event.go
git commit -m "feat(harness): add NoticeCodeSemantixReuse event code (Issue #190)"
```

---

## Task 3: Agent — bridge field, Options, turn-runtime fields

**Files:**
- Modify: `harness/internal/agent/agent.go` (add `semantix *semantix.Bridge` field on `Agent`; `Semantix *semantix.Bridge` on `Options`; assign in `New`)
- Modify: `harness/internal/agent/turnruntime.go` (add `injectBlock string` and `reuse semantix.ReuseSummary` fields on `turnRuntime`)
- Source: `agent.go:315/895/1130`, `turnruntime.go:38/44` on `upstream/harness-integration`

**Interfaces:**
- Consumes: `semantix.Bridge`, `semantix.ReuseSummary` (Task 1).
- Produces: `Agent.semantix`, `Options.Semantix`, `turnRuntime.injectBlock`, `turnRuntime.reuse` — read by Tasks 4/5.

- [ ] **Step 1: Add the import + Agent/Options fields**

In `harness/internal/agent/agent.go` add import `"semantix/harness/internal/semantix"`; add field `semantix *semantix.Bridge` to the `Agent` struct; add `Semantix *semantix.Bridge` to `Options`; in `New(...)` assign `a.semantix = opts.Semantix`. (Mirror source lines 315/895/1130.)

- [ ] **Step 2: Add turnRuntime fields**

In `harness/internal/agent/turnruntime.go` add to the `turnRuntime` struct:
```go
injectBlock string                // locked L2 injection block for this turn
reuse       semantix.ReuseSummary // per-turn reuse-panel payload
```
Add the `semantix` import there too.

- [ ] **Step 3: Build (no behavior yet)**

Run: `go build ./harness/internal/agent/`
Expected: compiles (fields unused is fine for structs/fields; if Go complains about unused import, it will be used after Step 1/2 reference the types).

- [ ] **Step 4: Commit**

```bash
git add harness/internal/agent/agent.go harness/internal/agent/turnruntime.go
git commit -m "feat(harness): agent holds semantix bridge + per-turn reuse/inject state (Issue #190)"
```

---

## Task 4: Agent — per-turn inject/reuse gather + injection consumption + prefetch warm

**Files:**
- Modify: `harness/internal/agent/run_loop.go` (gather at turn start), `sampling_request.go` (consume injectBlock), `agent.go` (`startInjectWarm` + `prefetchedInject atomic.Pointer[string]`)
- Source: `run_loop.go:263-269`, `sampling_request.go:108-112`, `agent.go:1827/2916`

**Interfaces:**
- Consumes: `Agent.semantix`, `turnRuntime.injectBlock/reuse` (Task 3).
- Produces: locked injection block prepended as a system message; `prefetchedInject` warm cache.

- [ ] **Step 1: Add the per-turn gather**

At the first-user-message point in the run loop (source `run_loop.go:263`), add:
```go
if a.semantix != nil && a.semantix.Enabled() {
    state.injectBlock = a.semantix.Inject(ctx, input)
    state.reuse = a.semantix.Reuse(ctx, input)
}
```
Match `state`/`input` variable names to #206's run_loop (the vendored loop is the same upstream code; find the equivalent first-turn hook).

- [ ] **Step 2: Consume the injection block**

In `sampling_request.go` where the system prompt is assembled, insert the locked block (source `:108`):
```go
if block := a.turn.injectBlock; block != "" {
    prependSystemBlock(&msgs, block)
} else if pb := a.prefetchedInject.Load(); pb != nil && *pb != "" {
    prependSystemBlock(&msgs, *pb)
}
```
If #206 has no `prependSystemBlock`, port it from source (it inserts a system message after the system prompt, prefix-cache-stable).

- [ ] **Step 3: Add prefetch warm**

Add `prefetchedInject atomic.Pointer[string]` field to `Agent`; add `startInjectWarm(ctx)` (source `agent.go:2916`) and call it during streaming when `a.semantix.InjectEnabled()` (source `agent.go:1827`).

- [ ] **Step 4: Build + agent tests**

Run: `go build ./harness/internal/agent/ && go test ./harness/internal/agent/ -run 'Inject|Reuse|Prefetch' 2>&1 | tail -20`
Expected: build passes; ported inject tests pass (add `git show upstream/harness-integration:harness/agent/<inject_test>` if one exists).

- [ ] **Step 5: Commit**

```bash
git add harness/internal/agent/run_loop.go harness/internal/agent/sampling_request.go harness/internal/agent/agent.go
git commit -m "feat(harness): in-process L2 inject gather + prefetch warm (Issue #190)"
```

---

## Task 5: Agent — reuse emission + task clock

**Files:**
- Create/Modify: `harness/internal/agent/reuse.go` (`emitReuse`, `reuseNotice`), Modify `agent.go` (`defer a.emitReuse(state)` in Run), `taskstate.go` (task clock fields), `turn_phase.go` (`emitCompletionSummary` stamps completion)
- Test: `harness/internal/agent/reuse_test.go`
- Source: `agent.go:1318`, `reuse.go`, `taskstate.go`, `turn_phase.go` on `upstream/harness-integration` (commits `aa10a6f` + `5102353`)

**Interfaces:**
- Consumes: `turnRuntime.reuse`, `Agent.semantix`, `event.NoticeCodeSemantixReuse` (Task 2), `agentServices.sink`.
- Produces: an `event.Event{Kind: Notice, Code: NoticeCodeSemantixReuse, Text: reuse.Line(), Detail: json(reuse)}` per turn — consumed by cli (Task 6).

- [ ] **Step 1: Port reuse.go (emitReuse/reuseNotice)**

```bash
git show upstream/harness-integration:harness/agent/reuse.go > harness/internal/agent/reuse.go
git show upstream/harness-integration:harness/agent/reuse_test.go > harness/internal/agent/reuse_test.go
```
Rewrite any `semantix/harness/<pkg>` imports → `.../internal/<pkg>`. `emitReuse` stamps `TaskElapsedSeconds`/`TaskComplete` from `a.task`, then emits the Notice via `a.svc.sink`.

- [ ] **Step 2: Add the task clock**

In `taskstate.go` add `taskStartedAt time.Time` and `taskCompletedAt *time.Time` to `taskRuntime`; stamp `taskStartedAt = time.Now()` in `restartLedger()`. In `turn_phase.go` add/extend `emitCompletionSummary` to stamp `taskCompletedAt` on `VerdictComplete`. (Source `5102353`.)

- [ ] **Step 3: Wire the deferred emit**

In `agent.go` Run, add `defer a.emitReuse(state)` (source `:1318`).

- [ ] **Step 4: Build + reuse tests**

Run: `go build ./harness/internal/agent/ && go test ./harness/internal/agent/ -run 'Reuse|Task' -v 2>&1 | tail -30`
Expected: PASS (reuse_test covers `reuseNotice` + duration formatting).

- [ ] **Step 5: Commit**

```bash
git add harness/internal/agent/reuse.go harness/internal/agent/reuse_test.go harness/internal/agent/taskstate.go harness/internal/agent/turn_phase.go harness/internal/agent/agent.go
git commit -m "feat(harness): per-turn reuse notice + task completion clock (Issue #190/#177)"
```

---

## Task 6: CLI — reuse panel render + theme + resource-gauge seam

**Files:**
- Create: `harness/internal/cli/reuse_panel.go`, `harness/internal/cli/resource_gauge.go`
- Modify: `harness/internal/cli/chat_tui.go` (consume the reuse Notice; add `gauge resourceGauge` field), `harness/internal/cli/theme.go` (add `success *cliColor` to `cliThemeStyle` + `semantix` theme entry)
- Test: `harness/internal/cli/{reuse_panel_test.go,theme_test.go}`
- Source: `cli/reuse_panel.go`, `cli/theme.go`, `cli/resource_gauge.go`, `cli/chat_tui.go:4604` on `upstream/harness-integration`

**Interfaces:**
- Consumes: `event.NoticeCodeSemantixReuse` (Task 2), `semantix.ReuseSummary` JSON (Task 1).
- Produces: green `📦 N slices · 💰 $X · 🗂 … · ✅/⏳ …` panel line under the TUI; `resourceGauge` interface + `nilGauge` (U40/U41 mount point).

- [ ] **Step 1: Port reuse_panel.go + resource_gauge.go + tests**

```bash
for f in reuse_panel.go reuse_panel_test.go resource_gauge.go theme_test.go; do
  git show upstream/harness-integration:harness/cli/$f > harness/internal/cli/$f
done
```
Rewrite imports to internal/. `reusePanelLines(detail, width)` json-unmarshals `Detail` into `semantix.ReuseSummary` and renders.

- [ ] **Step 2: Add theme success color + semantix theme**

In `theme.go` add `success *cliColor` field to `cliThemeStyle` + apply logic, and the `semantix` dark theme entry (accent/success `#2F967F`). (Source `aa10a6f` theme.go +12.)

- [ ] **Step 3: Wire chat_tui consumption + gauge field**

In `chat_tui.go`: add `gauge resourceGauge` field initialized to `nilGauge{}` in `newChatTUI`; in the event dispatch (`update`, #206 `chat_tui.go:1056`) add a branch matching `e.Code == event.NoticeCodeSemantixReuse` → append `reusePanelLines(e.Detail, m.width)` to the transcript (mirror source `chat_tui.go:4604`).

- [ ] **Step 4: Build + cli tests**

Run: `go build ./harness/internal/cli/ && go test ./harness/internal/cli/ -run 'Reuse|Theme' -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add harness/internal/cli/reuse_panel.go harness/internal/cli/reuse_panel_test.go harness/internal/cli/resource_gauge.go harness/internal/cli/theme.go harness/internal/cli/theme_test.go harness/internal/cli/chat_tui.go
git commit -m "feat(harness): reuse panel + semantix theme + resource-gauge seam (Issue #190)"
```

---

## Task 7: Agent — resource orchestration plane (issue #191)

**Files:**
- Create: `harness/internal/agent/resources.go` (`resourceCatalogState`), `harness/internal/agent/tier.go` (`TierResolver`, `applyScheduledTier`), `harness/internal/agent/scheduler_integration_test.go`
- Modify: `harness/internal/agent/agent.go` (`sched sched.Decider`, `resources *resourceCatalogState`, Options `{Decider, TierResolver, KernelEvents, ResourceModels, ResourceBudget}`, defaults `sched.NewRuleDecider`), `execute_batch.go` (`decideRound(ctx, ...)` + `observeSched` durationMs + suspend/parallel/budget), `run_loop.go` (`applyScheduledTier`), `services.go` (`tierResolver`), `sampling_request.go`
- Source: commit `f038c2f` on `upstream/harness-integration`

**Interfaces:**
- Consumes: `kernel/sched` (`Decider`, `NewRuleDecider`, `Config`, `RoundInput`, `RoundPlan`), `kernel/event` (`Bus`, `ResourceModel`, `ResourceBudget`, `ResourceCatalog*`) — **present on the base branch** (Task 0), `Bridge.Events()` (Task 1).
- Produces: scheduled-round decisions gating `executeBatch`; tier swaps; resource catalog emitted to the kernel bus.

- [ ] **Step 1: Port resources.go + tier.go + integration test**

```bash
for f in resources.go tier.go scheduler_integration_test.go; do
  git show upstream/harness-integration:harness/agent/$f > harness/internal/agent/$f
done
```
Rewrite imports to internal/ (kernel imports unchanged).

- [ ] **Step 2: Retype the scheduler + add Options + defaults**

In `agent.go`: change `Agent.sched` to `sched.Decider`; add `resources *resourceCatalogState`; add Options `Decider sched.Decider`, `TierResolver TierResolver`, `KernelEvents kernelevent.Bus`, `ResourceModels []kernelevent.ResourceModel`, `ResourceBudget ...`; in `New` default `sched.NewRuleDecider(sched.Config{})` and `newResourceCatalogState(...)`. (Source `f038c2f` agent.go +26.)

- [ ] **Step 3: Port execute_batch + run_loop + services deltas**

Apply the `f038c2f` changes to `execute_batch.go` (`decideRound(ctx, sched.RoundInput{...SuspendedTools, Budget})`, honor `plan.SuspendTools`/`MaxParallel`/`BudgetAction`, `observeSched` durationMs), `run_loop.go` (`a.applyScheduledTier(batch.Tier, batch.BudgetAction)`), `services.go` (`agentServices.tierResolver`), `sampling_request.go` (+4). Diff-guided: `git show f038c2f -- harness/agent/execute_batch.go` etc., re-applied at the internal/ paths.

- [ ] **Step 4: Build + orchestration tests**

Run: `go build ./harness/internal/agent/ && go test ./harness/internal/agent/ -run 'Sched|Resource|Batch|Tier' -v 2>&1 | tail -40`
Expected: PASS (`scheduler_integration_test.go`).

- [ ] **Step 5: Commit**

```bash
git add harness/internal/agent
git commit -m "feat(harness): resource orchestration plane on kernel/sched (Issue #191)"
```

---

## Task 8: Boot + config wiring — construct and inject the bridge

**Files:**
- Modify: `harness/internal/boot/boot.go` (construct `NewBridge`, wrap `sink`, `SetLabel`, pass agent Options + `resourceModels` + `tierResolver`)
- Modify: `harness/internal/config/config.go` (`SemantixConfig.CostInputPriceUSD`/`CostCachePriceUSD` if absent)
- Source: `boot.go:298-307 / 1735-1738 / 1955`, `config.go` on `upstream/harness-integration`

**Interfaces:**
- Consumes: everything above (`semantix.NewBridge`, agent `Options.{Semantix,Decider,TierResolver,KernelEvents,ResourceModels}`).
- Produces: a fully wired runtime — the bridge mirrors events, injects L2, drives the reuse panel and orchestration.

- [ ] **Step 1: Add config fields (if missing)**

Check `grep -n 'CostInputPriceUSD\|CostCachePriceUSD\|SemantixConfig' harness/internal/config/config.go`. If absent, add `CostInputPriceUSD float64` (`toml:"cost_input_price_usd"`) and `CostCachePriceUSD float64` (`toml:"cost_cache_price_usd"`) to `SemantixConfig`. If #206 has no `SemantixConfig` at all, port the struct from `git show upstream/harness-integration:harness/config/config.go` (the `[semantix]` section).

- [ ] **Step 2: Construct + wrap the bridge in build()**

In `boot.go` `build()` (find the sink/controller assembly), add (source `:298`):
```go
semantixBridge := semantix.NewBridge(semantix.Config{
    Enabled: cfg.Semantix.Enabled, Binary: cfg.Semantix.Binary, Inject: cfg.Semantix.Inject,
    Budget: cfg.Semantix.Budget, SessionsDir: cfg.Semantix.SessionsDir, ProjectDir: projectDir,
    CostMissUSD: cfg.Semantix.CostInputPriceUSD, CostHitUSD: cfg.Semantix.CostCachePriceUSD,
})
sink = semantixBridge.Sink(sink)
defer semantixBridge.Close()
```

- [ ] **Step 3: Build resourceModels + tierResolver, pass into agent Options**

Add `resourceModels` from `cfg.ResolveModel("flash"/"pro")` and the `tierResolver` closure (source `f038c2f` boot.go +35), then extend the `agent.New(... agent.Options{...})` call (source `:1735`) with:
```go
Semantix: semantixBridge, TierResolver: tierResolver,
KernelEvents: semantixBridge.Events(), ResourceModels: resourceModels,
```

- [ ] **Step 4: SetLabel after controller creation**

After `control.New(...)` (source `:1955`) add `semantixBridge.SetLabel(ctrl.Label())`.

- [ ] **Step 5: Build boot + full harness**

Run: `go build ./harness/... ./cmd/semantix-agent/...`
Expected: compiles.

- [ ] **Step 6: Commit**

```bash
git add harness/internal/boot/boot.go harness/internal/config/config.go
git commit -m "feat(harness): wire semantix bridge + resource orchestration in boot (Issue #189/#190/#191)"
```

---

## Task 9: Full verification — build, race tests, smoke

**Files:** none (verification only)

- [ ] **Step 1: Whole-repo build**

Run: `go build ./...`
Expected: green.

- [ ] **Step 2: Vet the new integration**

Run: `go vet ./harness/internal/semantix/ ./harness/internal/agent/ ./harness/internal/cli/ ./harness/internal/boot/`
Expected: zero warnings.

- [ ] **Step 3: Full race test suite**

Run: `go test ./... -race 2>&1 | tee /tmp/u38-test.log | tail -40`
Expected: kernel/gateway packages unchanged-green; harness internal packages green; new semantix integration tests green. Triage any FAIL against the Task 0 baseline — a failure present at baseline is pre-existing (record it), a new failure must be fixed before proceeding.

- [ ] **Step 4: cmd/semantix-agent smoke**

Run: `go run ./cmd/semantix-agent --version` and `go run ./cmd/semantix-agent --help`
Expected: exit 0, version/help print. If DeepSeek credentials are configured, also run a real `-p "say hi"` conversation and a `run` tool-call, confirming a session JSONL lands under `.semantix/sessions/` and the reuse panel renders on a second identical query (L3 hit).

- [ ] **Step 5: Commit any fixes, then tag the verification**

```bash
git add -A && git commit -m "test(harness): green build + race + smoke after #206 migration (Issue #189)" || echo "nothing to commit"
```

---

## Task 10: Attribution, migration notes, and PR

**Files:**
- Modify: `harness/ATTRIBUTION.md` (keep #206's MIT + decouple commit; add a note that the semantix integration was re-ported from the flat lineage)
- Create: PR description with the moved/trimmed/re-ported manifest
- Base branch: `harness-integration`

- [ ] **Step 1: Update ATTRIBUTION + write the migration manifest**

Ensure `harness/ATTRIBUTION.md` states: upstream MIT baseline + #206 decouple commit `bf0d8594`, and that U38/U39/#177/#191 semantix integration was re-ported onto the internal/ layout from `upstream/harness-integration` commits `aa10a6f`/`5102353`/`f038c2f`.

- [ ] **Step 2: Push the branch**

```bash
git push origin hi-rebuild-206:feat/u38-206-internal-migration
```

- [ ] **Step 3: Open the PR into harness-integration**

```bash
gh pr create --base harness-integration --head feat/u38-206-internal-migration \
  --title "feat(harness): adopt #206 internal/ vendor + re-port semantix integration (Issue #189)" \
  --body "<manifest: swapped flat→internal vendor; re-ported semantix bridge, U39 reuse panel, task-time, issue-191 orchestration; kept harness-integration kernel; supersedes #206>"
```
Note in the PR that this **supersedes #206** (adopts its vendor) and preserves U39/#177/#191, so #206 can be closed once this merges.

---

## Self-Review

**Spec coverage:** vendor swap (Task 0) → #189; bridge (Task 1) + inject/reuse (Tasks 4–5) + panel (Task 6) → #190; task clock (Task 5) → #177; resource orchestration (Task 7) + boot wiring (Task 8) → #191; verification (Task 9) → #189 checklist; attribution/PR (Task 10) → #189 deliverables. The two #206 vendor-adaptation nits (eventwire desktop contract, TOOL_CONTRACT.md) are captured as Task 0 baseline items.

**Placeholder scan:** the bulk-copy steps intentionally reference exact source paths (`git show upstream/harness-integration:harness/<pkg>/<file>`) rather than inlining 100–200-line vendor files — the source is the authoritative, tested original; inlining it would risk transcription drift. The small, hand-written seams (event const, Options fields, run_loop gather, boot call sites) are given verbatim.

**Type consistency:** `semantix.Bridge`/`semantix.ReuseSummary` (Task 1) are referenced identically in agent (Tasks 3–5) and cli (Task 6); `sched.Decider`/`RoundInput`/`RoundPlan` (Task 7) come from the base-branch kernel (verified present); `event.NoticeCodeSemantixReuse` (Task 2) is the single coupling constant used by both agent emit (Task 5) and cli render (Task 6).

**Known risk (flagged, not hidden):** the copy+import-rewrite method assumes #206's vendored `internal/{event,agent,cli,boot}` are the *same upstream code* as the flat lineage (so the seams graft cleanly). Where #206's vendor has drifted from the flat lineage (different function names at the hook points, refactored run loop), the hand-wired steps (Tasks 4/7/8) need local reconciliation against #206's actual code — the per-task `go build` + ported unit test is the guard that catches drift immediately.
