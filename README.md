<div align="center">

# Semantix

### Cross-session memory for AI coding agents — and a much smaller bill.

**Semantic Caching · Adaptive Scheduling · Speculative Prefetch · Cross-Session Learning**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg?style=flat-square)](./LICENSE)
[![Version](https://img.shields.io/badge/release-0.7.2-blue?style=flat-square)](https://github.com/Gnosil/semantix/releases)
[![GitHub stars](https://img.shields.io/github/stars/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/stargazers)
[![GitHub contributors](https://img.shields.io/github/contributors/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/graphs/contributors)
[![Website](https://img.shields.io/badge/website-semantix.ensureok.ai-168b6d?style=flat-square)](https://semantix.ensureok.ai)

**English** · [简体中文](./README.zh-CN.md) · [Quickstart](./docs/QUICKSTART.md) · [Technical Overview](./docs/TECHNICAL-OVERVIEW.md) · [Website](https://semantix.ensureok.ai)

</div>

<br/>

> **A coding agent loses its entire context when a session ends, and a provider's prefix cache hits only on byte-identical prompts — a single edit near the head invalidates everything after it.**
>
> Semantix addresses both: a complete coding agent with the memory kernel built in, and a standalone kernel that attaches to an agent already in use.

## Two ways to run it

**`semantix-agent` — the complete agent.** A CLI coding agent shipping with the memory kernel built in: extraction, retrieval, injection and the evolution loop are wired at boot, with provider presets for ~50 endpoints.

**The kernel — attached to an existing agent.** Harness-independent by design; the integration surface is a small set of hooks (tool registration, message interception, session export / event bypass). Three ways in:

| Path | Integration cost | Applies to |
| --- | --- | --- |
| **Agent skill** | `semantix install --target claude-code` | Claude Code |
| **Tool registration** | two tool schemas (`semantix_lookup` / `semantix_inject`) | custom / self-hosted agents |
| **Gateway** | one base URL, no code change | any OpenAI-compatible client |

Fail-open throughout: on kernel error the agent falls back to its normal execution path.

## Cross-session memory

A project's build conventions, test layout and service flags have to be re-established in every new session. Semantix extracts reusable **slices** from finished sessions — task patterns, project knowledge, tool sequences, verified results — into a local scored library, and reinjects the relevant ones when a similar task appears. Each hit carries its retrieval zone (🟢 hit · 🟡 grey · ⚪ miss) and its source session:

```text
$ semantix search --query "fix failing go test"
1. 🟢 score=4.331011 zone=hit id=619551c54af5437a scope=project from:2026-08-14-c9d4
   fix failing go test after refactor
2. 🟢 score=3.852740 zone=hit id=73b12bb117664106 scope=project from:2026-08-13-b7c2
   fix failing go test in kernel slice extractor
🎯 3/3 hits in 3 sessions
```

Hits, misses and manual corrections all feed back into slice scores and retrieval thresholds, so precision improves with use rather than volume alone. Type-aware eviction discards stale results first and retains project knowledge.

## Cache hit rate and cost

Provider prefix caches match byte-exactly: a hit requires the prompt to be identical to the previous one byte for byte, and a single differing character near the head invalidates everything after it.

The impact of that failure mode is routinely underestimated. Claude Code writes a per-request billing marker into the head of the system prompt. First-party endpoints strip it server-side; third-party endpoints do not, so there the line invalidates the cache from the first token. The spec behind the prefix-hygiene middleware attributes a **133× difference in hit rate** to that single header, and records cache spend rising **4–5×** when stripping is disabled. `semantix-gateway` strips it by default.

The design goal is to make the provider cache hittable rather than to work around it:

- **Byte-stable injection** — retrieved slices are ordered by ID rather than by score, so semantically similar requests produce byte-identical prefixes.
- **Prefix hygiene** — per-request attribution markers are stripped and the tool array is canonicalized by name, removing client enumeration order as a source of invalidation.
- **Provider awareness** — a vendor capability table, per-vendor cache lifetimes (DeepSeek's 24-hour on-disk context cache, Anthropic's 5-minute ephemeral window), budget-aware `cache_control` breakpoint placement, and per-model price tables.
- **L3 result reuse** — a verified result is returned without a model call, fail-closed.

### Per-provider adaptation

Cache behaviour is determined by the stack serving the model rather than by the model itself, and the spread is wide enough to require per-stack adaptation.

| Endpoint | Prompt-cache hit, steady turn | Where the number comes from |
| --- | --- | --- |
| DeepSeek | **99.8%** | provider-reported cache tokens |
| GLM | **97.5%** | week-long spike; ~97.6% telemetry ceiling |

Both are per-turn prompt-token hit rates — `hit / (hit + miss)` over cache token counts returned by the provider, not estimates. `semantix-agent` displays the current turn's rate alongside the session average in its status line, so the figure is verifiable on any workload.

A week-long GLM study measured the same model behind different hosts and found cache lifetimes differing several-fold: one stack held a prefix for 1–8 minutes with real expiry in `(8, 12]`; another maintained 96–98% for 120 seconds and had fallen to 28% at 301 seconds. This is why the TTL table is per-vendor rather than a single global value, and why GLM hit-rate telemetry carries a documented **~97.6%** reporting ceiling — trailing partial blocks never count as cached.

Method and raw runs: [docs/reports/glm-spike-week.md](./docs/reports/glm-spike-week.md) · [docs/reports/glm-p0-1-prefix-audit.md](./docs/reports/glm-p0-1-prefix-audit.md).

Beyond caching, the scheduler learns tool-usage patterns to parallelize eligible calls and to prefetch read-only context during model wait time.

### Measured

Real output from a small demo library (4 extracted sessions):

```text
$ semantix dashboard

  semantix dashboard — reuse snapshot
  ------------------------------------------------

  💰 Cost savings
     paid        $ 0.0060
     baseline    $ 0.0141
     saved       $ 0.0080  (56.99%)
     ██████████████░░░░░░░░░░

  🎯 Cache hit rate (L3/L2)
     4 / 5 turns  (80.00%)
     L3 1 · L2 3
     ███████████████████░░░░░

  🗂 Zone distribution (library replay)
     hit  ████ 4   grey ██████ 6   miss  0

  📦 Slice library
     10 slices · 3 cross-session sessions
```

- **79.8% cost saved** on a synthetic replay comparison — methodology in [docs/reports/m0-cost-comparison.md](./docs/reports/m0-cost-comparison.md)
- **80% cache hit rate (L3/L2)** on the demo library above
- A replay gate (`semantix verify`) enforces **≥ 70% relevance**; validating that hit rate on real user sessions is the open v1.0 gate — [#58](https://github.com/Gnosil/semantix/issues/58)

**These are replay / demo measurements, not production benchmarks.** The full evidence trail — and everything else technical — lives in [docs/TECHNICAL-OVERVIEW.md](./docs/TECHNICAL-OVERVIEW.md).

## Install

Release binaries for **macOS and Linux** (arm64 / amd64) from [Releases](https://github.com/Gnosil/semantix/releases):

```bash
tar -xzf semantix-agent-<version>-<platform>.tar.gz
cd semantix-agent-<version>-<platform>
./semantix-install.sh        # installs semantix-agent + semantix + config
```

Or build from source (Go 1.26+): `go build -o semantix ./cmd/semantix`

**Try it in 30 seconds** — extract slices from a past session, then reuse them:

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search  --query "fix failing go test" --db .semantix/project.db
semantix inject  --query "fix failing go test" --db .semantix/project.db   # L2 reuse block
semantix verify  --session <session-dir> --project demo                    # replay gate (≥70%)
semantix dashboard                                                         # one-screen snapshot
```

Full command reference, configuration and shell completion: [docs/QUICKSTART.md](./docs/QUICKSTART.md).

## Integrations

| Agent / harness | Integration path | Status |
| --- | --- | --- |
| DeepSeek-Reasonix | Built-in bundle: `[semantix] enabled=true` + `semantix_lookup` tool | ✅ shipped since v0.3.0 |
| Claude Code | Tool registration via `semantix_lookup` / `semantix_inject` schemas | ✅ documented (`agent-skill/tools/`) |
| LangChain apps | Middleware with two hooks (message rewrite + session extraction) | ✅ documented ([report](./docs/reports/langchain-middleware.md)) |
| Custom / self-hosted | Session bypass: export / event bypass / direct call | ✅ documented (`agent-skill/hooks/session-bypass.md`) |
| Any OpenAI-compatible client | `semantix-gateway` in front, custom base URL | ✅ shipped |

Cursor, Windsurf, Codex CLI, Gemini CLI, Copilot agent mode and Cline / Continue / Aider all reach the kernel through one of the paths above; none requires kernel changes. Priorities follow observed usage — concrete integration requests are tracked as [issues](https://github.com/Gnosil/semantix/issues) (template: `.github/ISSUE_TEMPLATE/integration_request.yml`).

## Documentation

| Doc | What's inside |
| --- | --- |
| [docs/TECHNICAL-OVERVIEW.md](./docs/TECHNICAL-OVERVIEW.md) | **Start here** — concepts, cache layers, scheduler, prefetch, evolution loop, architecture, module map, status, roadmap, metrics |
| [docs/QUICKSTART.md](./docs/QUICKSTART.md) | Install, 30-second demo, command reference, configuration |
| [docs/Agent-Infra-架构设计.md](./docs/Agent-Infra-架构设计.md) | Full architecture design (Chinese) |
| [docs/Agile路线图.md](./docs/Agile路线图.md) | Agile 1–3 roadmap and definition-of-done (Chinese) |
| [docs/Security-安全设计.md](./docs/Security-安全设计.md) · [SECURITY.md](./SECURITY.md) | Threat model and security mechanisms |
| [agent-skill/SKILL.md](./agent-skill/SKILL.md) | Self-serve integration for any harness |
| [semantix.ensureok.ai](https://semantix.ensureok.ai) | Website, product docs and the blog |

## Contributing

Semantix is early-stage. Contributions are welcome across semantic caching, retrieval, scheduling, speculative execution, evaluation methodology and harness adapters — see [CONTRIBUTING.md](./CONTRIBUTING.md). Branch naming `feat/<unit>`; PRs require green `go vet` and `go test -race`.

**Without writing code:** run `semantix verify` against your own sessions and post the resulting hit rate to [#58](https://github.com/Gnosil/semantix/issues/58). Aggregated community results decide the v1.0 gate.

Architectural assumptions are open to challenge; testing them is part of the work.

## License

[MIT](./LICENSE). [DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix) (MIT).

---

<div align="center">

### Semantix

**Every interaction should make the next one cheaper, faster, and smarter.**

</div>
