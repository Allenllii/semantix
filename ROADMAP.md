# Roadmap

> Source of truth: [`docs/Agile路线图.md`](docs/Agile路线图.md) (Chinese). This file is the English summary kept in sync at each release. Last updated: 2026-08-29 (v0.7.2).

## Shipped

| Milestone | Goal | Status |
| --- | --- | --- |
| **Agile 1** (M0 + M1 + CLI v2) | First downloadable branded agent: kernel + agent bundle, install-and-run, visible reuse metrics | ✅ v0.4.1 (CLI v2 complete: command tree, config, `--json` envelope, completion, doctor, install, gc, serve) |
| **Agile 2** — self-evolving loop | Kernel orchestrates the harness: resource scheduling, model tiers, speculative prefetch, evolution loop with causal before/after curves | ✅ v0.6.0 (2026-08-21) |
| **v0.7.x** — GLM adaptation + gateway economics | Zhipu GLM-5.x adaptation (prefix hygiene, hit telemetry → tier mapping, quality gates), gateway billing, config protection | ✅ v0.7.2 |

## In progress

- **Coding-agent desktop GUI (v0.8.0)** — epic [#403](https://github.com/Gnosil/semantix/issues/403), GUI-1…GUI-12 tracked as [#404](https://github.com/Gnosil/semantix/issues/404)–[#415](https://github.com/Gnosil/semantix/issues/415): workspace shell, session sidebar, conversation/tool cards, diff review, composer, observability status bar, history, desktop shell, release checks.
- **Credibility gate [#58](https://github.com/Gnosil/semantix/issues/58)** — real-data hit-rate ≥ 70% over a 30-day window (`semantix verify` replay). This is the single remaining Agile 1 gate for v1.0. Community-run `verify` results count: post yours to the issue.
- **Memory-arm benchmark rerun** — kernel-on SWE-bench arms after the four memory-kernel fixes (R1–R4, [#425](https://github.com/Gnosil/semantix/issues/425)–[#428](https://github.com/Gnosil/semantix/issues/428)) landed; see [`docs/reports/swebench-harness-comparison.md`](docs/reports/swebench-harness-comparison.md).

## Next

- **Agile 3 — multi-harness ecosystem**: ≥3 external harnesses formally integrated (serve/watch already shipped in CLI v2). Integration paths: [agent skill](agent-skill/SKILL.md), tool registration, OpenAI-compatible gateway.
- **Gateway line (GW2–GW7)**: streaming memory side-writes, deployment artifacts + healthz, full-chain acceptance (second-hit + cost saving ≥ 30%), Anthropic adaptation, config reconciliation.
- **v1.0**: single-binary bundle install-and-run + hit-rate gate passed + reuse visualization in TUI/desktop.

## Principles

- Every behavior change lands as **spec → implementation → acceptance report** (`docs/specs/`, `docs/reports/`).
- The event wire contract (`docs/events.md`) is append-only.
- Fail-open everywhere: a broken kernel never blocks the agent's normal path.
