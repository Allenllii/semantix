$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$launcher = Get-Content -LiteralPath (Join-Path $PSScriptRoot "semantix-desktop.ps1") -Raw
$doc = Get-Content -LiteralPath (Join-Path $root "docs\gui\desktop-shell.md") -Raw
foreach ($needle in @("--port-file", "-WorkingDirectory", "-WindowStyle Hidden", "-Stop", "semantix-agent web")) {
  if ($launcher -notmatch [regex]::Escape($needle)) { throw "launcher missing $needle" }
}
foreach ($needle in @("Lifecycle contract", "Wails v2", "Closing the browser window")) {
  if ($doc -notmatch [regex]::Escape($needle)) { throw "desktop doc missing $needle" }
}
Write-Output "desktop launcher contract passed"
