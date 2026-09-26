#!/usr/bin/env bash
# ROLLBACK for the S7-S9 closure batch (f5be6877..47987b66), 2026-09-24.
# Restores a Git working copy that has the batch applied back to the
# original base bytes: every file modified by the batch is checked out at
# f5be6877, every file added by the batch is removed. Requires the copy to
# share this repository's object store (a worktree/clone of semantix).
# Usage: bash ROLLBACK.sh <target-dir>
set -euo pipefail
TARGET="${1:?usage: ROLLBACK.sh <target-dir>}"
BASE=f5be6877
MODIFIED="cmd/semantix/lookup.go cmd/semantix/lookup_stats_test.go docs/reports/issue-447-memory-flow-repair.md docs/specs/issue-447-memory-flow-repair.md gateway/pipeline.go gateway/stats_test.go harness/agent/memory_flow_e2e_test.go harness/agent/negative_transfer_fuse_test.go harness/agent/progress_guard.go harness/agent/sampling_request.go harness/agent/turnruntime.go harness/event/event.go harness/semantix/admission_defaults_test.go harness/semantix/bridge.go harness/semantix/query.go harness/semantix/reuse_test.go kernel/event/event.go kernel/inject/inject.go kernel/slice/distill.go kernel/slice/extractor.go kernel/slice/slice.go"
ADDED="harness/agent/memory_delivery_test.go harness/semantix/delivery_accounting_test.go kernel/inject/historical_observation_test.go kernel/slice/task_body.go kernel/slice/task_body_boundaries_test.go kernel/slice/task_body_test.go"
cd "$TARGET"
git checkout "$BASE" -- $MODIFIED
for f in $ADDED; do rm -f "$f"; done
echo "RESTORED: status below lists exactly the batch files rolled back to base $BASE"
git status --short
