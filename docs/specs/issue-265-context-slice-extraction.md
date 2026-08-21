# Spec: deterministic project Context slice extraction (Issue 265)

## Goal

Generate one project-scoped Context slice from repeated, structured tool-call
observations in a session transcript. The MVP is deterministic and rule based;
it does not call an LLM or infer facts from tool output.

## Input contract

The extractor reads `tool_calls[].arguments` from the existing session JSONL.
It observes only:

- path fields: `path`, `paths`, `file`, `files`, `file_path`, `file_paths`,
  `source_path`, `destination_path`, `directory`, `directories`, `dir`, `dirs`,
  `cwd`, and `workdir`;
- `command` from shell-like tools (`bash`, `shell`, `exec_command`, or
  `run_command`).

Unknown fields, tool output, file contents, URLs, home paths, absolute paths,
and paths that escape through `..` are ignored. This keeps secrets and
machine-specific workspace roots out of the reusable project summary.

## Aggregation and wire text

Paths, their immediate parent directories, and command heads are counted over
the whole session. An observation must occur at least twice to be included.
Each category keeps at most five entries, ordered by count descending and then
lexicographically ascending.

The emitted text is a fixed English block with only non-empty sections:

```text
Project context observed from repeated tool calls:
Frequent paths:
- kernel/slice/extractor.go (3)
Frequent directories:
- kernel/slice (4)
Common command heads:
- go test (2)
```

The Context slice uses the existing content-derived ID. Identical transcript
bytes therefore produce identical Context content and ID. `CreatedAt` is not
part of this byte-stability guarantee.

## Scope

Context summaries are project knowledge and may only be stored with
`slice.Project` scope. Extracting into session or user scope skips Context
slices while preserving the existing Prompt, ToolPattern, and Result behavior.

## Acceptance

- repeated extraction yields byte-identical Context content and the same ID;
- frequency, Top-N, and tie ordering are deterministic;
- sensitive/absolute paths and unrelated argument values do not enter output;
- repeated shell commands produce stable command heads;
- no Context slice is emitted when every observation occurs only once;
- ingest and CLI extraction do not store Context slices outside project scope;
- existing extraction and ingest tests remain green.

## Non-goals

This increment does not inspect tool output, summarize source code, aggregate
across sessions, extract Memory slices, call an LLM, add configuration knobs,
or claim a retrieval-quality improvement without a separate replay evaluation.
