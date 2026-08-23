# Issue #328: plan approval revision feedback transport

## Status

Accepted for implementation.

## Problem

The plan approval API records three distinct decisions, but the in-process reply
channel carries only a boolean. It has no field for an inline revision note,
and both plan gates collapse the full reply before their callers can consume
it. A declined plain plan therefore ends the turn without either starting a
revision frame or leaving a model-visible explanation in history.

This issue provides the control-layer transport and consumption behavior needed
by the frontend work in #329. It does not change any frontend call site.

## Requirements

1. `approvalReply` gains bounded revision feedback.
2. `Controller.ResolvePlanDecision` and the `Approvals` port take a trailing
   `feedback string`; all existing boolean-only `Approve` APIs remain unchanged.
3. Feedback is trimmed and clipped to 4096 bytes on a valid UTF-8 boundary by
   the existing `clipUTF8` helper.
4. The plain plan-mode gate consumes the full `approvalReply` through
   `requestFreshApprovalDecision` rather than the boolean accessor.
5. `revise_plan` with non-empty feedback starts a new orchestrated synthetic
   revision frame whose user message contains the note verbatim. The revised
   plan re-enters the same approval gate.
6. Each revision frame owns its own `UserPromptSubmit`/`Stop` hook pair and
   message boundary; approved execution inside one frame retains the existing
   single hook pair.
7. A declined plan with empty feedback makes no second provider call and
   appends the exact marker
   `(The user did not approve this plan; execution was not started.)`.
8. `exit_plan` never starts a revision frame. It appends the same declined-plan
   marker when no feedback is supplied.
9. The planner approval adapter consumes the full reply too. Because its
   callback surface can start execution but cannot recursively request a new
   planner proposal, non-empty feedback is preserved as a user-role revision
   request in session history for the next planner turn. Empty denial remains
   owned by the coordinator's existing marker path, avoiding duplicates. It
   does not silently drop feedback.
10. Decision receipt schema and outcomes remain unchanged.
11. No file under `harness/cli/` changes. HTTP, ACP, bot, and serve bindings keep
    their boolean-only `Approve` behavior.

## Design

### Reply transport

`approvalReply.feedback` is private in-process state. `ResolvePlanDecision`
validates the action, clips the note, records the existing receipt, and sends
the reply. The boolean `allow` remains true only for `start_execution`, so old
control flow is preserved. The existing receipt continues to carry the exact
three-way action; feedback is not added to the receipt.

### Plain plan-mode gate

The gate calls `requestFreshApprovalDecision`, which already returns the full
reply and preserves the fresh-human posture of plan approval. On denial:

- non-empty feedback while plan mode remains enabled calls
  `runOrchestratedTurn` with a synthetic user-role prompt containing a stable
  revision marker and the clipped note;
- otherwise it appends the shared declined-plan marker and returns.

Calling `runOrchestratedTurn` rather than `runComposedSyntheticTurn` is
load-bearing: it creates a fresh message boundary, runs hooks, calls the model,
and gates the resulting revised plan again.

### Planner approval adapter

`plannerPlanApprover.RunWithPlannerApproval` also uses the full-reply accessor.
Its interface exposes only an execution callback and cannot safely nest a new
coordinator run while the current coordinator is blocked. It therefore
preserves non-empty feedback in session history as a synthetic user-role
revision request, allowing the next planner frame to consume it. Empty denial
is left to the coordinator's existing `plannerPlanNotApprovedNote` append after
the adapter returns. This explicitly removes the feedback drop without
inventing a re-entrant planner API or duplicating the established marker.

### History helpers

Control owns shared constants and two small helpers:

- `planNotApprovedNote` is byte-for-byte identical to the agent coordinator's
  established wording;
- `planRevisionMessage(feedback)` composes the bounded user-role instruction.

Messages are appended through the executor session. No event or persistence
format changes are introduced.

## Compatibility and scope

- `Controller.Approve(id string, allow, session, persist bool)` is unchanged.
- The `Approvals.ResolvePlanDecision` signature is intentionally widened for
  #329; `*Controller` is its only implementation.
- No CLI, HTTP, ACP, bot, or serve producer is rewired here.
- Existing plan receipts keep the same kind, outcome, and attachment behavior.

## Acceptance criteria

- A unit test resolves `revise_plan` with `widen the retry window` and observes
  `allow=false` plus the exact feedback on the reply channel.
- A >4 KiB multibyte note arrives as valid UTF-8 with at most 4096 bytes.
- A plain-gate integration test observes a second provider call whose opening
  user message contains the note, then observes a second plan approval request.
- Plan mode stays enabled throughout revision.
- Empty denial performs one provider call and appends `planNotApprovedNote`.
- Planner approval denial preserves non-empty feedback and leaves the existing
  empty-denial marker responsibility with the coordinator.
- Existing plan rejection and approved-plan hook invariants pass.
- `go build ./...`, `go vet ./...`, and affected control tests pass; race tests
  pass where the host toolchain supports them.
