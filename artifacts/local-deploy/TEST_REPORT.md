# Local V28 evaluation — 2026-08-22

## Deployment

- Source commit: `983fa6d`
- Binary: `semantix-agent-v28.exe`
- Model: `deepseek-v4-flash`
- Semantic reviewer: enabled with `SEMANTIX_SEMANTIC_REVIEW=1`

## Deterministic repository tasks

Five isolated Python repair tasks covered cross-file propagation, CSV edge cases, configuration precedence, path-traversal security, and multi-module validation.

Result: **5/5 tests passed** in 98,664 ms, using 264,676 input tokens, 6,959 output tokens, and $0.028201784.

All five agent processes returned exit status 1 despite producing correct, tested changes. The fixtures were untracked directories, so the runtime's final-diff readiness check could not observe a Git diff. This is a harness/runtime-exit weakness, not a task-test failure, and should be fixed before using process exit status as the sole CI signal.

## Official SWE-bench evaluation

- Instance: `django__django-13195`
- Harness: upstream `swebench.harness.run_evaluation` in Docker
- Result: **resolved (1/1)**
- Infrastructure failures: 0
- Agent duration: 537,694 ms
- Patch size: 7,788 characters
- Input/output tokens: 1,708,054 / 24,525
- Cost: $0.069163736

The generated patch matched the complete semantic fix across `HttpResponse.delete_cookie()`, message storage, session middleware, tests, documentation, and release notes. The official evaluator passed it. The CLI still returned status 1 after its successful work, so correctness and runtime-exit behavior must be scored separately.

## Interpretation

This run provides strong evidence for semantic caller propagation and security-invariant closure, plus broad small-task reliability. It does not establish a statistically stable SWE-bench pass rate: one official instance is only a smoke gate. A publishable comparison needs a frozen random subset, identical model/settings, at least three repetitions per arm, and confidence intervals over resolved rate, cost, latency, and invalid-exit rate.
