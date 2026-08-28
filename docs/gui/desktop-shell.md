# Windows desktop shell

Issue #414 introduces a desktop entry point without creating a second agent
runtime. The supported Windows launcher is `scripts/semantix-desktop.cmd` (or
the PowerShell script directly):

```powershell
scripts\semantix-desktop.cmd -ProjectPath C:\work\my-project
scripts\semantix-desktop.cmd -Stop
```

## Lifecycle contract

- The launcher starts the existing `semantix-agent web` command in a hidden
  child process. The child uses the selected project as its working directory,
  so its session store, config and tools resolve exactly as they do from the
  CLI.
- `--port-file` is used to read the actual bound address. The `web` command's
  existing retry behavior handles a busy requested port; the launcher reports
  the selected port instead of guessing it.
- The web command owns authentication and opens its tokenized URL. No API key
  or token is copied into launcher arguments or persisted by this script.
- Closing the browser window does not terminate the child, so an active task
  keeps running. `-Stop` terminates only the PID recorded by this launcher.
- `semantix-agent` and `semantix-agent serve` remain unchanged and can run
  independently of the launcher.

## Native-shell decision

The repository has no Wails/Tauri module today. Wails v2 is the follow-up
native-shell choice because the backend is Go and the current web workspace can
be embedded without changing the controller/event contracts. It is deliberately
not added to this phase: a native wrapper would add CGO/WebView2 build and
distribution requirements before the web workbench contract is stable. The
launcher above is the low-risk desktop adapter and is the migration seam for a
future Wails window.

Non-goals for this phase are auto-update, installers, system-tray behavior,
cross-platform packaging, and a second IPC/event model.
