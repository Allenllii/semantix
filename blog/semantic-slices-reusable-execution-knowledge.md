---
title: "Design Memo: Why Semantix Stores Slices Instead of Sessions"
description: "A concise design memo explaining the decision to store typed semantic slices and the tradeoffs it introduces."
updated: 2026-08-12
published: 2026-08-10
group: "Semantic Slices"
order: 104
---

# Design Memo: Why Semantix Stores Slices Instead of Sessions

**Decision:** use typed semantic slices as the reusable unit; keep raw sessions as ingest input, not as the only retrieval object.

## Context

Agent sessions are optimized for chronology. Retrieval is optimized for relevance. A single session may contain several tasks, false starts, and outputs with different safety requirements. Indexing the session as one document makes those boundaries invisible.

## Decision details

Semantix extracts Prompt, ToolPattern, and Result slices. Each slice carries scope and a content-derived ID. BM25 indexes tokenizable content; the hash embedder supplies deterministic vectors; hybrid search fuses both rankings. Injection selects and orders slices under a token budget.

```bash
semantix search --query "fix the failing Go test" --retriever bm25
semantix search --query "fix the failing Go test" --retriever vector
semantix search --query "fix the failing Go test" --retriever hybrid
```

## Consequences

The positive consequence is control: each unit can be inspected, scored, deleted, and attributed. The cost is extraction error. Turn-level boundaries may be too coarse, and a concise Result can omit conditions that made it true. This is why the roadmap treats real-session labeling as a gate and why L3 verifies dependencies before returning an old result.

## Alternatives considered

Storing only transcripts is simpler but noisy. Generating a model-written memory may be more fluent but adds cost and nondeterminism. Indexing every tool output creates volume without necessarily creating reusable knowledge. The slice design is a deliberately modest baseline between those extremes.

## Status

The mechanism exists and has unit/integration coverage. The public evidence does not yet show that this granularity is optimal across repositories. If real-session relevance misses the 70% gate twice, the M0 report calls for a stop/review decision after trying subtask-level extraction.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
