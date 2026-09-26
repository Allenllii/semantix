# Remaining memory-flow repair implementation plan
Spec: docs/specs/issue-447-memory-flow-repair.md §9.10. Execute inline, TDD, local commits; fresh whole-branch reviewer at end.
Global constraints: same Kernel/BM25/L3/security/roles; no network model calls; no old campaign or DB writes; preserve external dirty files. Test on exact-source isolated Linux copy if Windows cleanup obstructs verification.
Review focus: stale arbitrary Result; spoofed/unknown provenance; intercepted/unsent payload; cancelled/retried/incremental delivery; unchanged source audit and missing persistence.

## S7 Historical observation applicability
- [ ] Add kernel/inject/historical_observation_test.go: real transcript Extract+Distill → reopen store → four template cards admit on new commit with changed/missing source files. Assert source/commit/Deps intact and explicit historical label. Same Result/legacy/import/bad-path/unknown provenance remain rejected.
- [ ] RED: `go test ./kernel/inject -run TestHistorical -count=1 -v`.
- [ ] Implement SliceMeta.Historical, producer-only stamping in observedSlice, reset marker on generic newSlice; Injector applies historical semantics only to proven Context/Memory observations. No hash/extra configuration. Keep L3 and all snapshot checks.
- [ ] GREEN: same command; `go test ./kernel/... ./harness/semantix ./cmd/semantix -count=1`; save logs and commit owned paths.

## S8 Delivery accounting
- [ ] Add Agent and Bridge negatives for assembled/prefetched but unsent; loop guard no persisted rejection before real delivery. CLI output does not imply provider delivery; Gateway upstream error no Injected.
- [ ] RED: run named new tests before production changes.
- [ ] Move shared Bridge record call into explicit synchronous RecordInjectionDelivery, emitting existing KernelCache persistence failure diagnosis. In Agent.streamProviderRequest check successful nonnil stream and final user block, record once per turn; gate loop-guard feedback on delivered flag. Gateway moves stats below successful forward (2xx). CLI removes assembly-as-delivery events/stats.
- [ ] Add HTTP recorder with OpenAI, Anthropic and Responses stateful (initial/full, incremental, reset, expired ID full replay), failed send/cancel/filter/budget cases, retry at-most-once check. Expected JSON user-role block not privileged instruction; absent payload never grows Injected.
- [ ] GREEN: affected packages plus full Linux project/race/vet; commit owned paths.

## S9 Source task-body sharing
- [ ] Add a Distill test with actual testbed prefix and distinctive issue title; RED observes boilerplate currently used for outcome.
- [ ] Move existing retrievalTaskBody and required tag helpers to kernel/slice.TaskBody; Bridge calls it; Distill uses for title and classification. Preserve ordinary multi-line requirements; not provider input mutation.
- [ ] GREEN: query regressions + slice/distill tests + project suite; local commit.

## Completion
- [ ] Fresh read-only review against Spec; fix findings with RED/rollback/GREEN as required.
- [ ] Exact Git source archive in an independent directory: GREEN, then rollback to original files and observer-only RED, then restore originals without observer files. Main copy remains modified.
- [ ] Generate/reopen MODIFIED_FILE.tar.gz, DIFF_FILE.patch, VERIFICATION.txt, executable ROLLBACK.sh; raw commands/input/output/exit preserved.
- [ ] Persist memory + existing PAUSED automation readback; final report distinguishes software closure from unmeasured model benefits and legacy DB migration.
