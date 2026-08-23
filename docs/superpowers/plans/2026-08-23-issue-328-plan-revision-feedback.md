# Issue #328 Plan Revision Feedback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Carry bounded inline revision notes through plan approval and ensure neither control-layer plan gate discards them.

**Architecture:** Widen the internal plan decision reply, consume the existing full-reply accessor, and re-enter the ordinary orchestrator for plain-plan revisions. Preserve planner-gate notes in session history because its callback interface cannot safely start a nested planner run.

**Tech Stack:** Go, controller approval manager, agent session history, synchronous control events

**Spec:** `docs/specs/issue-328-plan-revision-feedback.md`

## Global Constraints

- `Controller.Approve` and boolean-only external bindings remain unchanged.
- Feedback is trimmed and UTF-8 clipped to exactly 4096 bytes maximum.
- Receipt schema and three-way receipt outcomes remain unchanged.
- No file under `harness/cli/` changes.
- Revised plain plans must pass through `runOrchestratedTurn` and a second gate.

---

### Task 1: Add bounded plan feedback transport

**Files:**
- Modify: `harness/control/controller.go`
- Modify: `harness/control/port.go`
- Modify: `harness/control/controller_test.go`

**Interfaces:**
- Produces: `ResolvePlanDecision(id string, action PlanDecisionAction, feedback string) error`
- Produces: `approvalReply.feedback string`
- Consumes: `clipUTF8(string, int) string`

- [ ] **Step 1: Add failing transport and clipping assertions**

Update the existing three-way decision test to pass feedback and assert the
reply field. Add a multibyte note longer than 4096 bytes and assert the received
value is valid UTF-8 and bounded.

- [ ] **Step 2: Run RED**

Run: `go test ./harness/control -run 'TestResolvePlanDecision' -count=1`

Expected: compile failure because the method and reply field do not accept
feedback.

- [ ] **Step 3: Implement the transport**

Add the reply field, widen the controller and port signatures, clip feedback,
and include it in the buffered reply. Do not change `Approve`.

- [ ] **Step 4: Run GREEN**

Run: `go test ./harness/control -run 'TestResolvePlanDecision' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add harness/control/controller.go harness/control/port.go harness/control/controller_test.go
git commit -m "feat(control): transport bounded plan revision feedback"
```

### Task 2: Consume feedback in the plain plan gate

**Files:**
- Modify: `harness/control/turn_orchestrator.go`
- Modify: `harness/control/auto_plan_e2e_test.go`
- Modify: `harness/control/turn_orchestrator_test.go`

**Interfaces:**
- Consumes: `requestFreshApprovalDecision(...) (approvalReply, error)`
- Produces: `planNotApprovedNote`, `planRevisionMessage(string) string`

- [ ] **Step 1: Add failing causal integration tests**

Script a plan, a revised plan, and approval events. Resolve the first gate with
revision feedback, assert a second user-role model frame and second approval,
then exit. Add the empty-feedback marker assertion.

- [ ] **Step 2: Run RED**

Run: `go test ./harness/control -run 'TestPlanGate(RevisionFeedbackReentersGate|EmptyRevisionPersistsMarker)$' -count=1`

Expected: FAIL because the first denial ends after one provider call and no
marker is appended.

- [ ] **Step 3: Implement full-reply gate handling**

Replace the boolean request with `requestFreshApprovalDecision`. Re-enter
`runOrchestratedTurn` for non-empty revision feedback while plan mode is on;
otherwise append the shared marker and return.

- [ ] **Step 4: Run GREEN and invariants**

Run: `go test ./harness/control -run 'Test(PlanGate|TurnOrchestratorApprovedPlanSharesOneStopHook)' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add harness/control/turn_orchestrator.go harness/control/auto_plan_e2e_test.go harness/control/turn_orchestrator_test.go
git commit -m "feat(control): re-enter plan gate with revision feedback"
```

### Task 3: Preserve feedback in the planner approval adapter

**Files:**
- Modify: `harness/control/controller.go`
- Modify: `harness/control/approval_e2e_test.go`

**Interfaces:**
- Consumes: `approvalReply.feedback`
- Produces: persisted planner revision request or shared declined marker

- [ ] **Step 1: Add failing planner denial tests**

Resolve a planner approval with non-empty feedback and assert a user-role
history message contains it. Resolve another with empty feedback and assert the
shared declined marker.

- [ ] **Step 2: Run RED**

Run: `go test ./harness/control -run 'TestPlannerPlanApproval(PreservesRevisionFeedback|PersistsEmptyDenialMarker)$' -count=1`

Expected: FAIL because the adapter discards the full reply.

- [ ] **Step 3: Implement planner history preservation**

Use `requestFreshApprovalDecision`; on denial append a user revision request
when feedback is non-empty, otherwise append the shared assistant marker.

- [ ] **Step 4: Run GREEN**

Run: `go test ./harness/control -run 'TestPlannerPlanApproval' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add harness/control/controller.go harness/control/approval_e2e_test.go
git commit -m "feat(control): preserve planner gate revision notes"
```

### Task 4: Verify and create PR

**Files:**
- No production files added.

**Interfaces:**
- Consumes: Tasks 1–3
- Produces: verified PR closing #328

- [ ] **Step 1: Format and run affected tests**

Run `gofmt` on changed Go files and `go test ./harness/control/...`.

Expected: PASS.

- [ ] **Step 2: Build and vet**

Run: `go build ./...` and `go vet ./...`.

Expected: exit 0, with unrelated baseline failures reported exactly.

- [ ] **Step 3: Run race verification where supported**

Run: `go test ./harness/control/... -race`.

Expected: PASS on a CGO/race-capable host.

- [ ] **Step 4: Review and publish**

Run `git diff upstream/main...HEAD --check`, push
`codex/issue-328-plan-revision-feedback`, and create a PR against
`Gnosil/semantix:main` with `Closes #328`, scope boundaries, hook behavior, and
exact validation evidence.

