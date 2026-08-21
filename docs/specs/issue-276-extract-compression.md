# Issue 276: deterministic extraction compression

## Status

Accepted for implementation. This spec covers rule-based compression applied
only while new Prompt and Result slices are extracted.

## Goals

- Increase useful content per injection byte without an LLM rewrite.
- Keep extraction deterministic: identical input and metadata produce identical
  content, IDs, and compression metadata.
- Preserve both ends of oversized content instead of always discarding the tail.
- Make compression observable without changing model-usage accounting.

## Rules (`rules-v1`)

Rules run in this order before the existing Prompt/Result byte limit is applied:

1. Normalize CRLF and CR line endings to LF.
2. Remove trailing spaces and tabs from every line.
3. Detect backtick and tilde code fences.
4. Remove lines made only from at least three Markdown decoration characters
   (`-`, `*`, `_`, `=`, `~`, or `#`). Content inside backtick or tilde code
   fences is exempt from rules 4-6 so code and log evidence remain exact.
5. Collapse consecutive blank lines to one blank line.
6. Collapse consecutive identical non-empty lines to one line.
7. If the result still exceeds the slice byte limit, retain a UTF-8-safe head
   and tail separated by the fixed marker `\n...[content compacted]...\n`.

The output is trimmed at its outer edges. The compressor is a pure function and
does not inspect time, environment, model output, or repository state.

## Persistence and compatibility

For compressed source text, `SliceMeta` stores:

- `compression_version`: `rules-v1`;
- `original_bytes`: source bytes before rule application;
- `stored_bytes`: bytes used to derive the slice ID and persisted content.

Compression happens before content-hash ID generation. Existing stored slices
are not migrated or rewritten; metadata fields are additive and absent on
legacy slices. Generated Context and ToolPattern slices do not claim source-text
compression metadata.

## Observability

`semantix extract` reports aggregate `raw_bytes`, `stored_bytes`, and
`compression_ratio` for the slices it successfully stores. `compression_ratio`
is the fraction saved, `(raw_bytes - stored_bytes) / raw_bytes`, from 0 to 1.
Slices without compression metadata count their current content bytes as both
raw and stored bytes.

The model-call usage log remains unchanged: extraction compression is not an
LLM request and must not be mixed into token or billing statistics.

## Non-goals

- LLM, LLMLingua, or summary-based rewriting.
- Semantic deduplication of non-consecutive text.
- Retrospective migration of existing slice stores.
- A guarantee that every transcript compresses by a fixed percentage.

## Verification

- Repeated extraction produces identical content, IDs, and compression fields.
- Output remains valid UTF-8 and within the Prompt/Result byte limits.
- A noisy transcript fixture compresses by at least 30 percent.
- Ordinary text, Context extraction, and ToolPattern extraction retain their
  existing behavior.
