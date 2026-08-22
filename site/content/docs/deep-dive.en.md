# Understanding Semantix from Scratch

Semantix starts from a constraint: model context is finite, tool execution has a cost, and not every part of a past session deserves to be used again.

## A transcript is not a memory system

Saving a conversation solves persistence. A new task still needs scope, retrieval, freshness, authority, and feedback. Replaying every transcript increases token use, noise, and prompt-injection exposure.

Semantix therefore extracts bounded semantic slices before retrieval and reuse.

## Scope comes before similarity

Session facts, project conventions, and user preferences have different lifetimes. The `session`, `project`, and `user` scopes reduce cross-project contamination and control recall. They are not a substitute for access control or data redaction.

## Retrieval does not approve execution

BM25 finds exact terms, vector retrieval finds paraphrases, and hybrid fusion reduces their individual blind spots. All three rank relevance. They do not prove that files, dependencies, or user intent are unchanged.

Semantix retains uncertainty through hit, grey, and miss zones. L3 candidates additionally pass dependency fingerprints, rule gates, and an optional judge. Uncertain candidates fall back to normal execution.

## Three cache levels, three different objects

- L1 reuses provider computation for a stable prompt prefix.
- L2 injects relevant historical context while the model and tools still run.
- L3 reuses a verified result and may skip repeated work.

The shared word “cache” does not make these paths equivalent. Risk and proof requirements increase from L1 to L3.

## Scheduling, prefetch, and evolution

The scheduler expresses round plans without replacing the harness permission model. Prefetch uses learned transition patterns for restricted read-only work and records both hits and waste.

Evolution currently means bounded parameter updates, such as retrieval thresholds and injection budgets. It does not mean unrestricted self-modification, and repeated use does not guarantee better outcomes.

## Failure behavior

Semantix is an optimization layer. When memory loading, judging, or prefetch cannot establish a safe path, the host continues the original task. Fail-open means abandoning the optimization, not bypassing a failed safety check.

## How to evaluate the project

Separate specifications, executable tests, synthetic replay, and production evidence. Semantix provides the first three in the repository. Generalized performance and cost claims require real sessions, human relevance checks, task outcomes, and billing comparison in the target environment.
