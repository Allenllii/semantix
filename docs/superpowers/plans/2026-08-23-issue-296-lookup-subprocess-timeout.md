# Issue #296 Lookup Subprocess Timeout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the argv subprocess test independent of the production timeout while preserving and directly testing fail-soft timeout behavior.

**Architecture:** Keep timeout injection instance-local on `semantixLookup`; its zero value retains the production default. Add a focused regression test that forces deadline expiry through the real subprocess boundary and observes the existing soft-degrade result.

**Tech Stack:** Go, `testing`, `os/exec`, race detector

**Spec:** `docs/specs/issue-296-lookup-subprocess-timeout.md`

## Global Constraints

- The zero-value production timeout remains exactly three seconds.
- Timeout injection is unexported and instance-local; no mutable package global is allowed.
- Subprocess failures and deadline expiry continue to return `"", nil`.
- Tests support Unix and Windows command shims.

---

### Task 1: Timeout fail-soft regression

**Files:**
- Modify: `harness/tool/semantix_test.go`

**Interfaces:**
- Consumes: `semantixLookup{timeout time.Duration}.Execute(context.Context, json.RawMessage) (string, error)`
- Produces: `TestSemantixLookupTimeoutFailsSoft`

- [ ] **Step 1: Write the failing test**

Add a fake executable with the existing platform-specific script convention,
execute `semantixLookup{timeout: time.Nanosecond}`, and require `out == ""` and
`err == nil`. Before the production branch is correct, the test must fail by
returning the fake executable's argv output.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./harness/tool -run '^TestSemantixLookupTimeoutFailsSoft$' -count=1`

Expected: FAIL because the temporary red-phase assertion expects a non-empty
result while deadline expiry produces an empty result.

- [ ] **Step 3: Complete the regression assertion**

Replace the temporary red-phase assertion with explicit checks that execution
returns no error and empty output. Keep setup local to the test and use no
mutable shared state.

- [ ] **Step 4: Run focused verification**

Run: `go test ./harness/tool -run 'TestSemantixLookup(Subprocess|TimeoutFailsSoft)$' -count=20`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add harness/tool/semantix_test.go
git commit -m "test(harness): cover lookup timeout soft degradation"
```

### Task 2: Repository verification and issue linkage

**Files:**
- Modify: `docs/specs/issue-296-lookup-subprocess-timeout.md` only if observed commands require acceptance-result corrections

**Interfaces:**
- Consumes: the regression tests from Task 1
- Produces: passing package and repository verification plus a PR that closes issue #296

- [ ] **Step 1: Run race-focused repetitions**

Run: `go test -race ./harness/tool -run 'TestSemantixLookup(Subprocess|TimeoutFailsSoft)$' -count=20`

Expected: PASS on a race-capable host.

- [ ] **Step 2: Run the repository suite**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 3: Review the branch**

Run: `git diff upstream/main...HEAD --check` and `git status --short`.

Expected: no whitespace errors and a clean worktree.

- [ ] **Step 4: Push and open the PR**

```bash
git push -u origin codex/issue-296-lookup-subprocess-timeout
gh pr create --repo Gnosil/semantix --base main --head Allenllii:codex/issue-296-lookup-subprocess-timeout --title "test(harness): close lookup subprocess timeout coverage gap" --body-file PR_BODY.md
```

The PR body must include `Closes #296`, the root cause, the preserved
production behavior, and exact verification commands.

