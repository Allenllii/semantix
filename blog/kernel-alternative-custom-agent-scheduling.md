---
title: "Control-Loop Walkthrough: What the Kernel Does Before a Tool Call"
description: "A control-loop walkthrough separating shipped retrieval behavior from scheduling and prefetch direction."
updated: 2026-08-12
published: 2026-08-10
group: "Scheduling & Harness"
order: 304
---

# Control-Loop Walkthrough: What the Kernel Does Before a Tool Call

Start with the shipped path, not the roadmap diagram.

## Today's path

Historical sessions are captured as JSONL. The extractor creates typed slices. Search ranks them with BM25, deterministic hash vectors, or hybrid fusion. Injection selects relevant slices and emits a stable marked block for the harness.

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

The CLI also exposes verification and usage reporting. L3 can reuse verified read-only results when dependency checks pass.

## The broader control-loop direction

The architecture describes intent-aware scheduling, concurrency, speculative prefetch, and an evolution loop that adjusts policy from outcomes. Those ideas explain why the project calls itself a kernel, but they should not be presented as equally mature shipped capabilities.

## The key interface question

A harness integration needs explicit answers: when are events flushed, which user text forms the retrieval query, where is the reuse block inserted, how is it removed, and which errors are non-blocking? If those contracts remain implicit, the kernel becomes difficult to debug regardless of retrieval quality.

## Practical conclusion

Use Semantix today as a testable retrieval-and-reuse side layer. Evaluate scheduling and prefetch as roadmap capabilities against their own code and tests. This distinction makes the current CLI useful without requiring a reader to accept every architectural ambition as complete.

## What the test surface says today

I checked the scheduler and prefetch packages directly on 2026-08-12 using Go 1.26.5 on Windows/amd64:

```bash
go test -count=1 ./kernel/sched ./kernel/prefetch ./kernel/evolve
```

The result matters because it separates interfaces from exercised behavior:

```text
    semantix/kernel/sched     [no test files]
    semantix/kernel/prefetch  [no test files]
ok  semantix/kernel/evolve
```

My reading is deliberately conservative. The adaptation package has executable tests; scheduler and prefetch compile, but their packages do not yet contain tests. That is not evidence of a production scheduling loop. Before calling this a kernel alternative, I would add decision-table tests, cancellation tests, a disabled-kernel baseline, and a trace showing which decision changed a real tool run.

## What changed my recommendation

Before running those packages, I treated scheduling, prefetch, and evolution as one maturity claim because they sit next to one another in the architecture. The output forced a narrower conclusion. Two packages compile without package tests, while `evolve` has executable coverage. A green build therefore says less than the diagram suggests.

If I were integrating Semantix into a harness today, I would enable extraction, retrieval, and marked-block injection first. I would leave scheduler-driven concurrency and speculative prefetch behind an explicit feature flag. The acceptance condition for enabling them would not be “the package builds.” I would require a trace that records the scheduling decision, the no-scheduler baseline, cancellation behavior, latency, and the downstream task result.

There is also an adverse case worth preserving: a speculative fetch can finish successfully and still be the wrong work. If its result consumes context or delays the tool that the user actually needs, a technically successful prefetch is a product failure. That is why usefulness, rejection rate, and cancellation cost belong beside execution time in the evaluation.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
