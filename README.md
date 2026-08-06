# Semantix

A **self-evolving agent kernel** that sits between any agent harness (e.g. DeepSeek-Reasonix, Claude Code) and its resources — dynamically orchestrating concurrency, semantic caching, and speculative prefetch based on **your usage habits**, so every interaction makes the next one cheaper and faster.

> 中文导读：Semantix 是一个**自进化的 Agent Kernel 层**。它架在现有 agent harness 与资源之间，通过「用户使用习惯 → 语义切片库 → 语义缓存/并发调度/投机预取 → 反馈进化」的闭环，让系统越用越好：能并发的并发、能语义缓存的缓存、能预取的就预取，最终让"一个问题进来 → 经过最优的 loop engineering 与 LLM 交互 → 得到优质结果"。

## Why Semantix

Existing agent harnesses optimize for **byte-level prefix caching within one session** — passive, session-bound, and static in scheduling. Semantix adds:

| Capability | What it does |
|---|---|
| **Semantic Slice Library (SSL)** | Extracts reusable semantic units (task templates, context blocks, tool-call patterns, high-frequency results) from your historical sessions, vector-indexed and persisted |
| **Semantic Cache (L1/L2/L3)** | Reuses similar work across sessions; **stable slice injection into the prefix region feeds the vendor's byte cache** — cross-session semantic hits become byte-level cache hits |
| **Kernel Scheduler** | Joint decisions on tool concurrency, model tier, cache injection, and prefetch — driven by the current task *intent*, learned from your behavior patterns |
| **Speculative Prefetch** | Fills LLM wait time with read-only prefetch (next-turn slice assembly, embeddings), self-penalizing waste |
| **Self-Evolution** | Every round feeds back hit/miss, pollution, latency, cost, success — online EWMA tuning (with freeze-period protection) + offline retraining |

## Documents

| File | Content |
|---|---|
| [`docs/Agent-Infra-架构设计.md`](docs/Agent-Infra-架构设计.md) | Full architecture design (Chinese): problem definition, layered architecture, component design, rationale, roadmap P0-P5, risks, success metrics |
| [`docs/总体架构-流程树.md`](docs/总体架构-流程树.md) | End-to-end flow tree (Chinese): "a question comes in → loop engineering + LLM interaction → high-quality result", structured for generating tree flowcharts (incl. ready-to-use mermaid source) |

## Roadmap

| Phase | Deliverable |
|---|---|
| P0 | Observability layer (harness adapter + event stream + baseline metrics) |
| P1 | Semantic Slice Library (extractor + embedder + ANN index, dual project/user stores) |
| P2 | Semantic cache (L2 stable injection + L3 verified reuse + pollution detection) |
| P3 | Scheduler (intent classification + concurrency behavior learning + tier) |
| P4 | Prefetcher (T-Slice transition matrix + path patterns + budget control) |
| P5 | Evolution loop (online EWMA + offline retraining + ablation) |

## Status

Design phase — architecture spec v2 (post-adversarial-review) is complete. Implementation starts once the naming & first target harness are locked in.

## License

MIT

*Design baseline: [DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix) (MIT, Go rewrite, branch `main-v2`). All file:line references in the docs point to that branch.*
