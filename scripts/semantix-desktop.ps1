[CmdletBinding()]
param(
  [string]$ProjectPath = (Get-Location).Path,
  [string]$Address = "127.0.0.1:8787",
  [string]$AgentPath = "semantix-agent",
  [switch]$Stop
)

$ErrorActionPreference = "Stop"
$stateRoot = Join-Path ([System.IO.Path]::GetTempPath()) "semantix-desktop"
$portFile = Join-Path $stateRoot "address.txt"
$pidFile = Join-Path $stateRoot "agent.pid"
$stdoutFile = Join-Path $stateRoot "agent.stdout.log"
$stderrFile = Join-Path $stateRoot "agent.stderr.log"

New-Item -ItemType Directory -Force -Path $stateRoot | Out-Null

if ($Stop) {
  if (-not (Test-Path -LiteralPath $pidFile)) {
    Write-Output "No Semantix desktop process is recorded."
    exit 0
  }
  $pid = [int](Get-Content -LiteralPath $pidFile -Raw).Trim()
  $process = Get-Process -Id $pid -ErrorAction SilentlyContinue
  if ($null -ne $process) {
    if ($process.ProcessName -ne "semantix-agent") {
      throw "Refusing to stop PID $pid because it is not semantix-agent. Remove $pidFile after checking it."
    }
    Stop-Process -Id $pid -Force
    Write-Output "Stopped semantix-agent (PID $pid)."
  } else {
    Write-Output "Semantix desktop process $pid is no longer running."
  }
  Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath $portFile -Force -ErrorAction SilentlyContinue
  exit 0
}

$project = (Resolve-Path -LiteralPath $ProjectPath).Path
Remove-Item -LiteralPath $portFile -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $stdoutFile, $stderrFile -Force -ErrorAction SilentlyContinue

$arguments = @("web", "--addr", $Address, "--port-file", $portFile, "--open")
$process = Start-Process -FilePath $AgentPath -ArgumentList $arguments -WorkingDirectory $project -WindowStyle Hidden -PassThru -RedirectStandardOutput $stdoutFile -RedirectStandardError $stderrFile
$process.Id | Set-Content -LiteralPath $pidFile -Encoding ascii

$deadline = (Get-Date).AddSeconds(15)
$actualAddress = $null
while ((Get-Date) -lt $deadline) {
  if (Test-Path -LiteralPath $portFile) {
    $actualAddress = (Get-Content -LiteralPath $portFile -Raw).Trim()
    if ($actualAddress) { break }
  }
  if ($process.HasExited) {
    $errorText = if (Test-Path -LiteralPath $stderrFile) { Get-Content -LiteralPath $stderrFile -Raw } else { "" }
    throw "semantix-agent web exited with code $($process.ExitCode). $errorText"
  }
  Start-Sleep -Milliseconds 150
}

if (-not $actualAddress) {
  throw "Timed out waiting for semantix-agent web to bind. See $stderrFile"
}

Write-Output "Semantix desktop workspace is running at http://$actualAddress/"
Write-Output "Project: $project"
Write-Output "PID: $($process.Id) (browser-window close does not stop the task; run with -Stop to stop it)"
