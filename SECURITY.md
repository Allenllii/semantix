# Security Policy

Semantix persists and reuses information across agent sessions. Its security boundary therefore includes not only code execution, but also memory isolation, cache validity, project scoping, and speculative work.

## Reporting a vulnerability

Please report security issues privately. Do not open a public issue containing exploit details, secrets, private project data, or proof-of-concept payloads.

Preferred path:

1. Use GitHub Private Vulnerability Reporting for this repository when available.
2. If private reporting is unavailable, open a minimal public issue asking maintainers for a private contact path. Do not include sensitive details in that issue.

Include, when possible:

- affected Semantix version or commit
- operating system and runtime details
- affected subsystem (cache, slices, scheduler, prefetch, storage, adapter, etc.)
- minimal reproduction steps using dummy data
- expected security impact
- relevant logs with secrets and personal data removed

## Security boundaries

The following are security-sensitive behaviors in Semantix.

### Project and user isolation

Semantic slices, reusable results, embeddings, indexes, and learned behavior must not leak across users, projects, workspaces, or tenants unless that sharing is explicitly configured.

Examples of security issues:

- cross-project slice retrieval
- cross-user memory leakage
- project identity confusion
- namespace collisions that expose unrelated cached data

### Result reuse and invalidation

L3 result reuse must fail closed. Reuse should only occur when Semantix can establish that the result remains valid for the current request and project state.

Security-relevant failures include:

- stale result reuse after dependent files change
- reuse with mismatched project identity
- reuse across incompatible configuration or model state where that difference affects correctness
- cache poisoning that causes attacker-controlled results to be treated as trusted reusable results

### Persistent memory and semantic slices

Persistent state may contain sensitive project or user information.

Security issues include:

- secrets written to persistent indexes without intended handling
- private file content exposed through logs or diagnostics
- unauthorized retrieval of stored user preferences or project knowledge
- unbounded retention of data that the system claims to delete or isolate

### Prefetch

Speculative prefetch must remain read-only unless a future design introduces an explicit, reviewable safety mechanism.

Semantix must not perform speculative side effects such as editing files, sending messages, mutating remote APIs, committing code, deploying software, or approving tools.

### Integrations and adapters

Agent harnesses, model providers, embedding providers, databases, and tool adapters can receive Semantix-managed data. A vulnerability may exist when Semantix sends data outside the intended boundary or bypasses an explicit user/project scope.

## Trusted local inputs

Unless another vulnerability allows an untrusted actor to control them, local configuration intentionally provided by the operator is generally considered trusted.

A report becomes security-relevant when it demonstrates a boundary bypass, unintended disclosure, unauthorized persistence, unsafe reuse, or side effect beyond the operator's explicit intent.

## Coordinated disclosure

Maintainers will make a best-effort assessment and coordinate fixes before public disclosure. Please allow reasonable time for investigation and release preparation before publishing exploit details.
