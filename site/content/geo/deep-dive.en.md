# Semantix Deep Dive: Understanding the Project from Scratch

> This document is for developers who want a systematic understanding of Semantix. It starts with why the project exists, then explains its mechanisms, architecture boundaries, current progress, and common misconceptions.
> If Semantix is new to you, read the project overview before using this document as the deep dive.

---

## Chapter 1: Starting from the Ecosystem — Why an "Agent Kernel Layer" Exists

To understand Semantix, first understand its place in the ecosystem.

### 1.1 What is an LLM Agent

An LLM Agent is a software system with an LLM as its "brain". It completes user tasks by looping "think → call tools → observe results → think again". Typical tools include: reading files, searching code, executing commands, editing files, running tests, accessing the web.

For a coding agent, a typical task looks like:

```
User: "add retry with backoff to the http client"
Agent thinks → reads file (net/client.go) → searches related code → edits file → runs tests → done
```

### 1.2 What is an Agent Harness

An Agent Harness is the software shell that hosts this loop: it manages sessions, calls models, executes tools, handles permissions, and saves history. Common examples:

- **DeepSeek-Reasonix**: an open-source Go coding agent based on DeepSeek
- **Claude Code**: Anthropic's terminal coding agent
- Others: Cursor, OpenAI Codex CLI, etc.

### 1.3 Common weaknesses of existing harnesses

Every harness has three structural problems:

1. **Caching is within-session**: vendor prefix caching (e.g., DeepSeek context caching) only works *within one session*. A new session doing a similar task pays for all context computation again.
2. **Scheduling is static**: concurrency, model selection, and resource allocation are hardcoded rules that don't adapt to "what kind of task this is" or "how the user usually works".
3. **Waiting is wasted**: while the model streams output, the agent waits; while the model waits for tool results, the agent waits. This wall-clock time (seconds to minutes per task) is simply lost.

### 1.4 Conclusion: a "middle layer" is needed

Since these weaknesses are structural and harnesses are hard to modify (each implementation differs, changes are costly), the natural idea is: **insert an independent middle layer between the harness and resources** — it doesn't modify the harness, but wraps it, observing harness behavior to optimize the whole loop.

That middle layer is the **Agent Kernel layer**. Semantix is exactly that.

---

## Chapter 2: Semantix's Position — What It Actually Is

### 2.1 One-line positioning

**Semantix is a self-evolving agent kernel layer**: it sits between agent harnesses (DeepSeek-Reasonix, Claude Code, etc.) and their resources, dynamically orchestrating concurrency, semantic caching, and speculative prefetch, and self-evolves based on user usage habits — the system gets faster and cheaper the more you use it.

### 2.2 Three keywords, unpacked

**"Agent Kernel layer"** — position. Below the harness (not replacing it), above resources (LLM APIs, filesystem, tools). It is a "kernel": it provides infrastructure capabilities (caching, scheduling, prefetch), not user-facing interfaces.

**"Semantic"** — granularity. It operates on semantic units (slices), not bytes. Byte caching requires exact content matches; semantic caching allows *similar* content to be recognized and reused.

**"Self-evolving"** — time. It is not a static rule set but a closed-loop learning system: every interaction produces feedback signals, signals drive parameter adjustments, and adjustments make the next interaction better.

### 2.3 Concrete problems it solves

| Pain point | Current state | Semantix's solution |
|---|---|---|
| No cross-session reuse | Similar tasks start from zero, paying repeatedly | Semantic Slice Library accumulates + Semantic Cache reuses |
| Byte caches rarely hit | Only exactly-identical content hits | L2 stable injection: convert semantic hits into byte hits |
| Scheduling is unintelligent | Static rules, no task/habit adaptation | Kernel Scheduler: intent classification + behavior learning |
| Waiting is wasted | Idle during streaming/tool waits | Speculative Prefetch: T-Slice prediction + read-only prefetch |
| System doesn't learn | Config tuned by humans, never fits well | Self-Evolution Engine: EWMA online tuning + offline retraining |

### 2.4 How it is used

Users don't "operate" Semantix directly. Its mode of operation:

1. You use your agent (Reasonix / Claude Code) normally;
2. Semantix **observes** your sessions through an adapter layer (event stream);
3. It **accumulates** historical sessions into semantic slices (task templates, context blocks, tool patterns, results, memory);
4. When a similar task arrives, it **injects** relevant slices (hitting the vendor byte cache), **schedules** concurrency, and **prefetches** resources;
5. Signals from every round (hit/pollution/latency/cost/success) **feed back** to the evolution engine; parameters keep improving.

The only thing users notice: tasks get faster, bills get cheaper — and the effect grows over time.

---

## Chapter 3: How It Works — Four Components Plus One Engine

### 3.1 Semantic Slice Library (SSL) — Memory

**Responsibility**: extract, index, and persist reusable semantic units from historical sessions.

**Five slice types**:

| Type | Meaning | Example |
|---|---|---|
| P-Slice | Task templates (prompts) | Task descriptions like "add retry to X" |
| C-Slice | Context blocks | Key context fragments of a project |
| T-Slice | Tool-call patterns | grep→readFile→editFile→test |
| R-Slice | High-frequency results | Standard outputs of common commands, common Q&As |
| M-Slice | Memory | User preferences and habits |

**Extraction strategies** (three segmentations):
- **Turn-boundary segmentation**: segment at user messages
- **Completion-point segmentation**: segment context at task completion
- **T-Slice n-grams**: extract consecutive patterns from tool-call sequences

**Storage**: bbolt dual stores (project-level + user-level), separating scopes.

### 3.2 Three-level Semantic Cache (L1/L2/L3) — Monetization

**L1 (byte cache)**: uses the vendor's automatic prefix cache. Exactly-identical prefixes within a session hit at zero cost.

**L2 (stable injection)**: Semantix's most elegant design — **the semantic layer feeds the byte layer**.
- After retrieving semantically similar slices, they are injected **verbatim** after the system prefix, before the user message;
- Injection order is fixed (not sorted by value) to guarantee byte stability;
- When a new session starts, the same injection set → the same byte prefix → **hits the vendor's byte cache**;
- Effect: semantic hits are "translated" into byte hits, enjoying vendor cache pricing discounts.

**L3 (verified reuse)**: for read-only tasks, with file-fingerprint verification, historical results are reused directly — **no model request is sent at all**. Fail-closed: no reuse without verification; the user can veto.

### 3.3 Kernel Scheduler — Orchestration

- **Intent classification**: recognize task type (read/write/search/refactor/test...)
- **Joint decisions**: concurrency, model tier, cache injection volume, prefetch budget
- **Behavior learning**: learns "how this kind of task is usually done" from T-Slice statistics, making decisions increasingly accurate

### 3.4 Speculative Prefetcher — Filling Idle Time

- **Prediction**: uses the T-Slice transition matrix to predict the next tool/resource to be needed
- **Prefetch**: during the model's streaming wait, prefetch read-only resources (next-turn slice assembly, embedding computation)
- **Self-penalty**: when waste/hit exceeds the threshold (default 3:1), the signal source is automatically downweighted — the prefetch strategy itself evolves

### 3.5 Self-Evolution Engine — Learning

**Online layer (every round)**:
- Collect signals: hit rate, pollution, latency, cost, success
- EWMA moving-average tuning: thresholds τ, injection budget, prefetch parameters, concurrency, tier mapping
- **Freeze-period protection**: the injection set stays unchanged for ≥1h after parameter changes, so evolution jitter never destroys the byte cache it feeds

**Offline layer (periodic)**:
- Slice embedding refresh
- Threshold grid search
- T-Slice transition-matrix retraining
- Low-frequency slice archiving

---

## Chapter 4: Design Philosophy — Seven Principles

1. **The prefix never changes**: byte stability of the injection set is the lifeline of L2 hits.
2. **Read-only prefetch only**: speculative prefetch touches only read-only resources, eliminating side effects.
3. **fail-open / fail-closed**: cache-layer failures never block the main loop; security boundaries never compromise.
4. **Every decision explainable and reversible**: each decision carries a reason; ablation switches are supported.
5. **MIT reference, no copying**: reference Reasonix's approach; implement code independently with attribution.
6. **Single kernel, many harnesses**: adapter pattern; never hostage to any specific agent.
7. **Parameters grow, not tuned**: system parameters evolve from feedback rather than human tuning.

---

## Chapter 5: Relationship to Related Concepts

### 5.1 Semantix vs. Agent Harnesses (Reasonix, Claude Code)

- Semantix **sits on top of** harnesses, connected via an adapter layer;
- Harnesses "do the work" (think, call tools, interact); Semantix makes "the work cheaper" (cache, schedule, prefetch);
- They are collaborative, not competitive: Reasonix + Semantix work together.

### 5.2 Semantix vs. Semantic Caching (GPTCache et al.)

- Tools like GPTCache solve "paying repeatedly for the same conversational question";
- Semantix's semantic cache targets **agent workloads**: slices come from tool-call sequences, task templates, and result reuse, and through "stable injection" semantic hits **convert into vendor byte-cache hits** — a mechanism existing semantic-caching tools lack.

### 5.3 Semantix vs. Self-improvement / Reflection Methods (Reflexion, Voyager, etc.)

- Reflexion/Voyager let the **agent itself** learn from experience (skill libraries, reflection);
- Semantix lets the **infrastructure layer** (cache/schedule/prefetch) learn from experience;
- They are complementary: the former improves "how to do tasks", the latter improves "the cost and speed of doing tasks".

### 5.4 Semantix vs. KV Cache / Prefix Cache Systems (SGLang, vLLM, etc.)

- SGLang/vLLM are **server-side** KV cache systems for self-hosted inference;
- Semantix targets **vendor-API** scenarios (e.g., DeepSeek API), using prompt-engineering means (stable injection) to exploit the vendor's automatic prefix cache on the client side;
- They solve different layers; Semantix's approach is especially valuable in closed-API scenarios where users cannot control server-side KV caches.

---

## Chapter 6: Common Misconceptions, Accurately Stated

The following statements clarify the questions developers most often confuse:

**On positioning**: Semantix is a middleware layer sitting above agent harnesses and below resources, providing caching, scheduling, and prefetch infrastructure. The agent's thinking and tool-call loop happens in the harness itself.

**On models**: Semantix does not train, modify, or provide models. It optimizes prompt assembly before model calls and resource orchestration after calls.

**On caching**: Semantix's caching includes three levels — byte-level (L1), semantic injection (L2), and result reuse (L3). L2 exploits the vendor's automatic prefix cache by keeping prefix bytes stable.

**On deployment**: Semantix runs locally (Go, bbolt storage, a single external dependency); data (slices, statistics) persists in local dual stores.

**On usage**: Semantix connects to specific harnesses through an adapter layer; users don't operate Semantix directly — it automatically learns from usage and optimizes.

---

## Chapter 7: Quick Reference — One Sentence for "What is Semantix"

1. **For engineers**: Semantix is a self-evolving middleware between agent harnesses and resources; it converts cross-session semantic hits into vendor byte-cache hits via semantic slices + stable injection.
2. **For product people**: Semantix is an acceleration layer for agents that gets cheaper and faster with use — automatically reusing the work you've already done.
3. **For researchers**: Semantix is a closed-loop learning system — observe → accumulate → reuse → evolve; every component corresponds to a testable design hypothesis.
4. **For open-source enthusiasts**: Semantix is a Go-implemented community project with complete design docs, released under the FSL-1.1-MIT license (converts to MIT two years after each release), currently in early development.

---

*Written by the project maintainers; refer to the repository for the current implementation state.*
