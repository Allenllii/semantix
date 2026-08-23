# Issue #296: deterministic `semantix_lookup` subprocess tests

## Status

Accepted for implementation.

## Problem

`TestSemantixLookupSubprocess` verifies the command-line arguments passed to a
fake `semantix` executable. Production lookups intentionally use a three-second
deadline and fail soft by returning an empty result. Under a repository-wide
`go test ./... -race` run, scheduler and process-start contention can consume
that deadline before the fake executable writes its output. The test then sees
an empty string and reports an argv mismatch even though argv construction is
correct.

The production deadline and fail-soft behavior are user-facing resilience
contracts and must not be relaxed to accommodate a test.

## Root cause

One deadline previously served two unrelated concerns:

1. production availability bounds for an external kernel process; and
2. a test that observes argv construction rather than latency.

When the production deadline expires, `Execute` deliberately erases the
subprocess error. Consequently, the argv test cannot distinguish scheduling
contention from incorrect argv.

## Requirements

1. A zero-value `semantixLookup` keeps the three-second production deadline.
2. Tests may inject a deadline on an individual `semantixLookup` value without
   mutating package-global state.
3. `TestSemantixLookupSubprocess` uses a one-minute deadline and continues to
   assert the exact query and limit argv.
4. A dedicated regression test proves that an injected expired deadline still
   returns an empty result and no error.
5. The test suite remains portable across Unix shell scripts and Windows batch
   shims.
6. No public API or production configuration is added.

## Design

`semantixLookup` owns an unexported `timeout time.Duration` field. `Execute`
uses that value when positive and otherwise selects the three-second default.
The argv test constructs a value with a one-minute timeout; production
registration continues to use the zero value. A timeout regression test uses
an already-expired duration and a fake executable, then checks the fail-soft
contract directly.

Instance-local injection avoids data races that a mutable package variable
would introduce during parallel or race-enabled tests.

## Acceptance criteria

- `go test ./harness/tool -run 'TestSemantixLookup(Subprocess|TimeoutFailsSoft)$' -count=20` passes.
- `go test -race ./harness/tool -run 'TestSemantixLookup(Subprocess|TimeoutFailsSoft)$' -count=20` passes on a race-capable host.
- `go test ./...` passes.
- Production code still defaults to a three-second subprocess timeout and
  returns `"", nil` for process failures and deadline expiry.
