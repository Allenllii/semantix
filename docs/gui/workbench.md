# Workspace context workbench

The right-hand context area of `/workspace` is a single, collapsible panel with
four tabbed views. The tabs are a presentation layer over the existing
`GET /workspace/events` stream; they do not introduce a second workspace or
history data model.

| View | Source of truth | What it shows |
| --- | --- | --- |
| 文件 | existing workspace shell + the latest `tool.fileDiff` | project tree, current file, and the latest file preview |
| Diff | `tool.fileDiff` on tool events | each structured file diff received during the current turn |
| 终端 | `tool.args`, `tool.output`, `tool.err`, `tool.execution` | shell command, live/result output, execution state, and exit code |
| Review | `tool.fileDiff` and `approval_request` events | changed files, server-reported mutation risk, and pending permission decisions |

Review decisions call the existing `POST /approve` endpoint with one-shot
`allow`/`deny` values. The browser never enables a session or persistent grant,
and it cannot bypass the controller's permission policy. If no matching event
has arrived, the panel shows an explicit empty state instead of sample data.

The tab controller uses ARIA `tab`/`tabpanel` relationships and roving
`tabindex`; switching views only changes the panel's visibility, so collapsing
the right rail keeps the center work area layout unchanged.
