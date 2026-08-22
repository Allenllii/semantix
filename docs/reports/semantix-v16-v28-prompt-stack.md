# Semantix V16–V28 prompt stack

This branch ports the proven local prompt stack onto the current Semantix architecture:

- **V16:** compact default executor prompt with Occam's razor, semantic-caller closure, workspace preservation, and evidence-based completion.
- **V24.1:** the independent reviewer receives the actual `git diff HEAD`, including staged changes.
- **V25:** one isolated, bounded repair worker handles a concrete reviewer failure before one final executor retry.
- **V27:** retained as architecture, not prompt bulk: stable host constraints remain programmatic; only active roles and real runtime capability/context data are injected. Template placeholders and the full role catalog are not sent to the model.
- **V28:** semantic review is opt-in (`SEMANTIX_SEMANTIC_REVIEW=1`) and conditionally routed only for interface, caller propagation, configuration, protocol, authentication/authorization, permission, path-traversal, and related boundary changes.

## Recorded SWE-bench evidence

| Variant | Resolved | Mean duration | Input tokens | Output tokens | Mean cost |
|---|---:|---:|---:|---:|---:|
| V16 repeated Django | 2/3 (66.7%) | 175,017 ms | 1,158,393 | 18,947 | $0.0465046 |
| V24.1 actual-diff reviewer | 3/3 (100%) | 329,582 ms | 2,937,492 | 35,445 | $0.104333736 |
| V28 mixed set | 5/5 (100%) | 298,715 ms | 1,608,391 | 36,471 | $0.083972725 |
| V16 fresh paired short set | 5/5 (100%) | 84,252 ms | 238,330 | 6,541 | $0.025545656 |
| V28 fresh paired short set | 5/5 (100%) | 102,420 ms | 308,195 | 7,916 | $0.031164424 |

The paired short-set result shows equal correctness but V28 used 29.31% more input tokens, so the earlier unconditional installation gate failed. This port therefore keeps V16 as the compact default and makes V28 review both opt-in and conditional. The repository tests validate routing, budgets, staged-diff capture, and prompt invariants; they do not relabel the historical benchmark as a new run.
