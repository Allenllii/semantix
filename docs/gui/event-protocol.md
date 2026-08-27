// Protocol: Semantix workspace frontend events (v1)
// Issue #407 · Epic #403
//
// Endpoint: `GET /workspace/events` (SSE). The legacy `GET /events` raw-wire
// stream is unchanged and remains the contract for the console/index.html and
// desktop clients.

## Envelope

Every frame carries the full SSE form:

	id: <seq>
	event: <type>
	data: {"v":1,"seq":<seq>,"type":"<type>","task_id":"<id>","time_ms":<ms>,
	       "data":<untouched eventwire frame>}

- `v` — protocol version, currently 1. Bump on breaking shape changes.
- `seq` — broadcaster-lifetime monotonic counter assigned when an event is
  published, shared by all connections. Delivery order equals seq order on a
  single connection. A reconnect sends `Last-Event-ID`; the server replays the
  retained suffix from its bounded 256-frame log before live events. A response
  with `X-Semantix-Replay: gap` means the requested prefix has expired; the
  client must refresh `/history` and continue consuming the stream. The counter
  resets when the server/broadcaster restarts.
- `task_id` — basename of the active session transcript, snapshotted when the
  connection subscribes. A connection therefore belongs to exactly one task;
  clients reconnect to re-snapshot after switching tasks.
- `data` — the untouched eventwire JSON (`eventwire.ToWire`). The envelope
  NEVER synthesizes tool results, diffs, or cache numbers.

## Types

| type                 | source wire kinds                                     | notes |
|----------------------|-------------------------------------------------------|-------|
| user_message         | steer                                                 | mid-turn steer confirmations |
| assistant_message    | text, message, reasoning                               | deltas + final assembly ride `data.kind` |
| tool_start           | tool_dispatch, tool_progress                           | progress chunks keep flowing under this type |
| tool_result          | tool_result                                            | only real tool outputs |
| permission_request   | approval_request, ask_request                          | blocks until /approve or /answer |
| cache_status         | usage, compaction_started, compaction_done             | session hit/miss tokens live in data.Usage |
| task_status          | turn_started, turn_phase, completion_summary, retrying, guardian_assessment, extension_status | turn lifecycle phases |
| error                | turn_done(err≠"")                                      | payload-dependent promotion from task_status |
| plan                 | —                                                      | **reserved**: no producer yet; never emitted in v1 |
| diff                 | —                                                      | **reserved**: no producer yet; never emitted in v1 |
| unknown              | any kind added after this protocol version             | forward-compat wrapper |

Filtered host-internal kinds that are never forwarded:
`stream_attempt`, `extension_surface`, `mcp_surface_ready`,
`workspace_changed`, `context_maintenance`.

## Client rules

1. Treat unrecognized `event:` names (and unrecognized fields inside `data`)
   as no-op — new backend kinds must not crash old pages.
2. Validate ordering via `seq`; treat gaps or `X-Semantix-Replay: gap` as a
   signal to refresh derived state, not to abort the stream.
3. Attribute frames per `task_id`; discard stale-task frames after switching.
4. Never trust synthesized content — everything authoritative lives inside
   `data`.

## Server rules (tested)

1. One wire kind maps to exactly one canonical type; completeness tests cover
   every kind eventwire knows, including skipped ones.
2. Frames are emitted in delivery order with strictly increasing `seq`.
3. Reserved types (`plan`, `diff`) are asserted absent from streams.
4. On reconnect, preserve the last received SSE `id` as `Last-Event-ID` (the
   browser `EventSource` does this automatically). Initial connections replay
   pending prompts; reconnects rely on the event log so approval cards are not
   duplicated.
