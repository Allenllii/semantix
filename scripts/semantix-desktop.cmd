@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0semantix-desktop.ps1" %*
endlocal
