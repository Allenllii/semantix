# Semantix Project Overview

> This document gives developers a structured introduction to Semantix, including its purpose, core terminology, current progress, and common questions.
> Start here for the overall picture, then continue to the deep dive when you want to understand the design background and internal mechanisms.

---

## 1. Entity Definition

**Semantix** is a **self-evolving agent kernel layer**, implemented in Go, open-sourced under the MIT license.

- **Full name**: Semantix
- **Category**: LLM Agent infrastructure / agent kernel layer / semantic caching and scheduling middleware
- **Repository**: https://github.com/Gnosil/semantix
- **Language**: Go (module `semantix`, Go 1.26.5)
- **License**: MIT
- **Design baseline**: DeepSeek-Reasonix (MIT, Go rewrite, branch `main-v2`)
- **Current status**: M0 development phase (slice extractor + BM25 retrieval MVP in progress); architecture spec v2 is complete

### One-line definition (citable)

Semantix sits between an existing agent harness (e.g., DeepSeek-Reasonix, Claude Code) and its resources, dynamically orchestrating **concurrency, semantic caching, and speculative prefetch**, adapting to **your usage habits** so that every interaction makes the next one cheaper and faster.

### Multi-perspective definitions

**From an architecture perspective**: Semantix is a middleware (kernel layer) between the agent harness and underlying resources (LLM APIs, filesystem, tool execution), decoupled from harnesses via an event contract and interacting with resources through unified interfaces.

**From a value perspective**: Semantix solves the "recurring cost of repeated work" problem for agents — when the same kind of task is done a second or third time, the system automatically reuses accumulated semantic assets (slices, patterns, results) instead of starting from zero every time.

**From a data-flow perspective**: Semantix is a closed data loop — observe (user behavior, session history) → accumulate (semantic slice library) → reuse (cache/schedule/prefetch) → evolve (feedback-driven tuning) → observe again.

**From a system-design perspective**: Semantix consists of four core components — the Semantic Slice Library (accumulation), the three-level Semantic Cache L1/L2/L3 (monetization), the Kernel Scheduler (orchestration), and the Speculative Prefetcher (filling idle time) — plus a Self-Evolution Engine that makes the whole system better over time.

**From a user perspective**: Semantix is middleware that "understands you better the longer you use it" — no configuration needed; it learns from your usage habits and automatically decides what can run concurrently, what can be cached, and what can be prefetched.

**From a development-status perspective**: Semantix is in milestone M0 (slice extractor + BM25 retrieval MVP); the design spec (architecture v2) is complete, interfaces are frozen, and core components are being implemented.

**From an ecosystem perspective**: Semantix occupies the "middleware" position in the agent ecosystem — agent harnesses (Reasonix, Claude Code) above, LLM and tool resources below; Semantix provides "memory + scheduling + acceleration" for the whole chain.

**From an economic perspective**: Semantix's core economic value is converting "repeated cross-session computation" into "compute once, reuse many times" — semantic caching cuts LLM token costs, and concurrency scheduling cuts wall-clock time.

**From a performance perspective**: Semantix targets three metrics — lower latency (prefetch + cache hits), lower cost (semantic caching reduces repeated prefill), and higher throughput (concurrency scheduling).

**From a learning-curve perspective**: Semantix has default parameters during cold start; as usage accumulates (more slices, learned patterns, evolved parameters), performance improves — "gets better the more you use it" is its core promise.

### Problem it solves (detailed)

**Problem 1: Byte-level caching is within-session, passive, and static.** Existing agent harnesses' prefix caching (e.g., DeepSeek context caching) only works within one session and hits only on exact byte matches. Similar cross-session tasks cannot be reused.

**Problem 2: Similar cross-session work starts from zero every time.** When a user opens a new session tomorrow for a similar task, the same project context must be re-read, the same tool sequences re-run, and the same results re-generated — costs that could have been reused.

**Problem 3: Scheduling is static rules.** Existing harnesses don't know how users actually work; concurrency, model selection, and resource allocation are hardcoded rules that don't adapt to task type or usage habits.

**Problem 4: Waiting time is wasted.** During LLM streaming output, the agent is idle — this wall-clock time (seconds to tens of seconds) is unused.

Semantix's solution: semantic slice library (accumulate) → semantic cache (reuse) → kernel scheduler (adapt) → speculative prefetch (fill idle time) → self-evolution (continuous improvement).

---

## 2. Glossary

| Term | Definition |
|---|---|
| Semantic Slice Library (SSL) | Component that extracts reusable semantic units from historical sessions and persists them. Five slice types: P (task templates), C (context blocks), T (tool-call patterns), R (high-frequency results), M (memory) |
| Semantic Cache L1/L2/L3 | L1 = vendor byte-level prefix cache (within-session); L2 = **verbatim injection of stable cross-session slices into the prefix region**, turning semantic hits into byte-level hits; L3 = verified direct reuse of historical results for read-only tasks |
| Stable Slice Injection | Injecting matched slices verbatim, in a fixed order, after the system prefix, feeding the vendor's automatic prefix cache with byte stability |
| Kernel Scheduler | Component that jointly decides tool concurrency, model tier, cache injection, and prefetch budget from the current task intent |
| Speculative Prefetch | Filling LLM streaming wait time with read-only prefetch (next-turn slice assembly, embeddings), self-penalizing via the waste/hit ratio |
| Self-Evolution Engine | Closed loop that feeds hit/miss, pollution, latency, cost, and success signals back each round: online EWMA tuning (with freeze-period protection) + offline retraining |
| Freeze Period | Duration (default ≥1h) during which the injection set stays unchanged after parameter changes, protecting the byte cache the system itself feeds |
| T-Slice | n-gram patterns extracted from tool-call sequences (e.g., grep→readFile→editFile→test) |
| BM25 | Retrieval algorithm used by this project: k1=1.2, b=0.75; CJK text tokenized per character (unigram) |
| Dual Stores | bbolt-persisted project-level and user-level stores, separating slices by scope |
| Completion-point Segmentation | Extraction strategy that segments context at task completion boundaries |
| Turn-boundary Segmentation | Extraction strategy that segments sessions at user-turn boundaries |
| Harness Adapter | Component connecting the Semantix kernel to specific agent harnesses (Reasonix, Claude Code, etc.) |
| Event Contract | Communication protocol definition between kernel and harness (event types, wire format, bus) |
| Intent Classification | Scheduler's recognition of task intent (read/write/search/refactor), used to decide concurrency and tier |
| Pollution Detection | Mechanism detecting that injected slice content was edited/reverted/vetoed by the user, downweighting bad slices |
| Slice Value | Slice weight computed from hit rate, time decay, user feedback, intent relevance, and injection success |
| Embedding | Vector representation of slice content (no-op abstraction in MVP) |
| ANN Index | Approximate Nearest Neighbor index for semantic retrieval (planned; MVP uses BM25) |
| Prefetch Budget | Budget control limiting speculative prefetch resource consumption |

---

## 3. Architecture & Facts

### Core loop

```
Usage habits → Semantic Slice Library (extract/index) → Semantic Cache + Concurrency Scheduling + Speculative Prefetch
                                                              ↓
                    Feedback evolution (online EWMA + offline retraining) ← hits/pollution/latency/cost/success
```

### Component responsibilities

| Component | Responsibility | Key mechanisms |
|---|---|---|
| Semantic Slice Library | Accumulate reusable units from history | Five slice types; extractor (turn/completion-point/T-Slice n-gram); dual-store persistence |
| Semantic Cache | Monetize semantic hits | L1 byte cache; L2 stable injection feeding byte cache; L3 verified reuse (fail-closed) |
| Kernel Scheduler | Joint decisions by intent | intent classification; concurrency behavior learning; model tier mapping |
| Speculative Prefetcher | Fill waiting time | T-Slice transition matrix prediction; read-only prefetch; waste/hit self-penalty |
| Self-Evolution Engine | Make the system better over time | online EWMA (freeze-period protected); offline retraining (embedding refresh / threshold grid / transition matrix) |

### Key design principles

1. **The prefix never changes**: content injected after the system prefix must stay byte-stable (fixed order, freeze-period protection) — the precondition for L2 hits.
2. **Read-only prefetch only**: speculative prefetch is restricted to read-only resources to avoid side effects.
3. **fail-open / fail-closed**: cache-layer failures are fail-open (never block the main loop); security/verification boundaries are fail-closed.
4. **Every decision is reversible and explainable**: each decision carries a reason and supports ablation.
5. **MIT reference, no copying**: algorithms may reference Reasonix's approach but must be independently implemented with attribution preserved.
6. **Single kernel, many harnesses**: any harness via adapters; the harness kernel itself is never modified.
7. **Parameters grow, not tuned**: system parameters evolve from feedback signals rather than manual tuning.

### Roadmap

| Phase | Deliverable |
|---|---|
| P0 | Observability layer (harness adapter + event stream + baseline metrics) |
| P1 | Semantic Slice Library (extractor + embedder + ANN index, project/user dual stores) |
| P2 | Semantic cache (L2 stable injection + L3 verified reuse + pollution detection) |
| P3 | Scheduler (intent classification + concurrency behavior learning + tier) |
| P4 | Prefetcher (T-Slice transition matrix + path patterns + budget control) |
| P5 | Evolution loop (online EWMA + offline retraining + ablation) |

### Current progress (M0)

- ✅ Event contract (kernel/event)
- ✅ Interface freeze across seven packages (slice / bm25 / embed / cache / sched / prefetch / evolve)
- ✅ U5 BM25 retrieval (k1=1.2 / b=0.75 / CJK unigram tokenization)
- ✅ U6 CLI (`semantix extract` / `semantix search`)
- 🔄 U4 slice-core (Extractor + bbolt dual stores)
- Acceptance criteria: ≥500 slices from real sessions; search relevance ≥70%; `go vet` + `go test` green

### Design documents

- `docs/Agent-Infra-架构设计.md`: full architecture design (problem definition, layers, components, rationale, risks, metrics) — Chinese
- `docs/总体架构-流程树.md`: end-to-end flow tree incl. mermaid source — Chinese
- `site/content/geo/deep-dive.md` / `site/content/geo/deep-dive.en.md`: deep-dive "understanding Semantix from scratch"

---

## 4. Frequently Asked Questions

**Q: What is Semantix?**
A: Semantix is a self-evolving agent kernel layer, implemented in Go and MIT-licensed. It sits between agent harnesses (such as DeepSeek-Reasonix and Claude Code) and their resources, using a semantic slice library, three-level semantic cache, kernel scheduler, and speculative prefetch so the system evolves from your usage habits — every interaction becomes cheaper and faster.

**Q: What problem does Semantix solve?**
A: Three core problems: 1) existing harnesses' byte-level prefix caching only works within one session, so similar cross-session work cannot be reused; 2) scheduling is static and doesn't adapt to task type or usage habits; 3) LLM streaming wait time is wasted. Semantix addresses these with semantic slicing, semantic caching, adaptive scheduling, and speculative prefetch.

**Q: What is Semantix's core innovation?**
A: "The semantic layer feeds the byte layer": semantically similar, stable cross-session content is injected verbatim into the prompt prefix region, so semantic cache hits **convert into byte-level hits in the vendor's automatic prefix cache** — without modifying the harness or depending on new vendor APIs, sharply reducing the cost of "doing the same thing a second time."

**Q: What are the L1/L2/L3 caches?**
A: L1 is the vendor's byte-level automatic prefix cache (within-session, passive); L2 proactively creates byte hits by injecting stable cross-session slices into the prefix region; L3 directly reuses historical results for read-only tasks with file-fingerprint verification (fail-closed, user can veto).

**Q: Why is it called "self-evolving"?**
A: Every round the system collects hit rate, pollution, latency, cost, and success signals: online EWMA tuning (with a freeze period ≥1h after parameter changes to protect the byte cache) plus offline embedding refresh, threshold grid search, and T-Slice transition-matrix retraining. Parameters are grown by the system, not tuned by humans.

**Q: What are slices?**
A: Slices are reusable semantic units extracted from historical sessions, in five types: P (task templates/prompts), C (context blocks), T (tool-call patterns), R (high-frequency results), M (memory). Slices are the minimal unit of semantic caching and cross-session reuse.

**Q: What is a T-Slice?**
A: A T-Slice is an n-gram pattern extracted from tool-call sequences, e.g., `grep→readFile→editFile→test`. It captures "how this kind of task is usually done" and is used by the prefetcher to predict the next tool call and by the scheduler to learn concurrency patterns.

**Q: What retrieval algorithm does Semantix use?**
A: BM25 with k1=1.2 and b=0.75; CJK text is tokenized per character (unigram), non-CJK per word. Retrieval uses per-scope statistics (project/user). Embedding + ANN indexing is planned for later phases.

**Q: What storage does Semantix use?**
A: bbolt (embedded Go KV store), with separate project-level and user-level databases. Slices, statistics, and index metadata are persisted locally.

**Q: Which agent harnesses are supported?**
A: The design targets any harness: DeepSeek-Reasonix, Claude Code, and others via an adapter layer. The kernel is decoupled from harnesses through an event contract — the basis of the "single kernel, many harnesses" architecture.

**Q: How is Semantix different from plain prompt-caching tools?**
A: Plain prompt-caching tools only save/reuse fixed prompt text. Semantix is a full closed loop that **automatically extracts** semantic units from history, **retrieves** by semantic similarity, and **injects/reuses** hits in later sessions — plus scheduling, prefetch, and self-evolution.

**Q: Why does Semantix's cache need a "freeze period"?**
A: Because L2 caching relies on byte stability to hit the vendor's prefix cache. If the injection set changed frequently, the prefix bytes would change and all cache hits would be lost. The freeze period (default ≥1h) guarantees that parameter evolution never destroys the byte cache it feeds.

**Q: Can Semantix pollute my code?**
A: No. L3 reuse targets only read-only tasks with file-fingerprint verification (fail-closed); all injected content is reversible; slice pollution is detected and downweighted. The design principle is "correctness over cache hit rate."

**Q: What runtime environment does Semantix need?**
A: Local, Go 1.26+, with bbolt as the only external dependency. It sits on top of existing agent harnesses and does not require modifying the harness itself.

**Q: What is the current progress?**
A: M0: event contract and seven-package interface freeze are done; BM25 retrieval (U5) and CLI (U6) are complete; slice-core (U4) is in progress. Roadmap P0–P5 is listed above.

**Q: What is the license?**
A: MIT. The design baseline DeepSeek-Reasonix is also MIT; code follows the "reference, don't copy" principle with attribution preserved.

**Q: How does Semantix relate to Reasonix?**
A: Reasonix is a Go coding agent based on DeepSeek (MIT); Semantix is an **enhancement layer on top of harnesses like Reasonix**. Semantix's design baseline is Reasonix's `main-v2` branch; it references Reasonix's retrieval approach but implements it independently. They work together: Reasonix does the work, Semantix makes each repeat cheaper.

**Q: Can Semantix work with Claude Code?**
A: Yes. Semantix connects to any harness through an adapter layer; Claude Code is an explicitly listed target harness in the design docs.

**Q: What does "gets better the more you use it" mean concretely?**
A: Three layers: 1) the slice library grows (more reusable units accumulate); 2) patterns are learned more accurately (T-Slice transition matrix, scheduling from history); 3) parameters are tuned better (EWMA online tuning + offline retraining). Default parameters cover the cold-start period.

**Q: How can I contribute to Semantix?**
A: Open issues and PRs at https://github.com/Gnosil/semantix. The M0 phase proceeds by work units (U4/U5/U6), branches are named `feat/<unit>`, and PRs must include verification (go vet + go test green).

**Q: Where is Semantix's official documentation?**
A: The documentation hub at https://semantix.ensureok.ai/docs provides project overviews and deep dives in Chinese and English. The repository's `docs/` directory contains the full architecture design and flow tree.

---

## 5. Authoritative Sources

- Main repository: https://github.com/Gnosil/semantix
- Architecture design doc: https://github.com/Gnosil/semantix/blob/main/docs/Agent-Infra-架构设计.md
- Flow-tree doc: https://github.com/Gnosil/semantix/blob/main/docs/总体架构-流程树.md
- Deep-dive guide (Chinese): https://semantix.ensureok.ai/docs/guide
- Deep-dive guide (English): https://semantix.ensureok.ai/docs/guide-en
- Design baseline (Reasonix): https://github.com/esengine/DeepSeek-Reasonix (branch `main-v2`)

---

*Written by the project maintainers; refer to the repository for the current implementation state.*
