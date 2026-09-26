# SDD ledger — plan: closure/PLAN.md
Spec: docs/specs/issue-447-memory-flow-repair.md §9.10
User requested continuous execution with SPEC-first local commits and rollback-before-adjustment.
S0–S6 already complete; do not repeat.
- S7 applicability: complete (2dc964ce, Kernel/Bridge/CLI full package checks GREEN)
- S8 delivered accounting: complete (79cd80dd, negative/HTTP/full project/race/vet GREEN)
- S9 task-body sharing and full integration: pending
- Fresh review / final rollback / artifacts: pending
Ruling: deterministic observations and arbitrary claims have different validity, not type-wide freshness removal. Keep source Deps as audit and strict snapshot checks for unclassified/Result.
Ruling: use existing Injected/event at accepted provider boundary; old persisted assembly counts remain versioned historical data.

S7 first edit command failed decoding UTF-8 before any write; reran same edit with Python -X utf8. Original native Windows test execution denied; Linux same assertions used, original output retained.

S9 extra boundary RED: 4 real failures (Unicode casefold shifted byte indices; tag-name prefix stripped unrelated content). Saved attempt1 patch/files; restored 2 tracked files to HEAD and removed 3 owned additions before adjusting. Reuse the same parser with byte-preserving ASCII tag fold and exact tag-name boundary; no new wrapper format.

S8 contract: exact Git-archive-plus-owned-overlay full suite, affected race and whole-repo vet exit0; event/CLI/Gateway/Agent production calls verified by loopback HTTP. Foreign invoice close edits excluded. Local commit only.

Baseline runner first invocation had a CRLF shell flag (-v\r); output retained as CRLF-BASELINE.txt, not behavioral RED. Corrected runner line endings only, no source change.

2026-09-24 ZCode takeover completion: codex quota exhausted mid-S9. Landed the corrected TaskBody
attempt (47987b66) + round-2 leftovers (204e8d9a test cleanup, 5c8f4b7f docs) + spec status
(e7baff6f). Closure RED/GREEN/ROLLBACK-RED and attribution evidence in this directory
(RED.txt/GREEN.txt/ROLLBACK-RED.txt/VERIFICATION.txt/BASE-ENVPACKAGES.txt/AGENT-SERIAL.txt);
S9 boundary tests green on the landed attempt. Linux race still open. No push.
