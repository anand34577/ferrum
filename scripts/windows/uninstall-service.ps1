#requires -RunAsAdministrator
<#
.SYNOPSIS
  Stops and removes the "Ferrum" Windows service.

.DESCRIPTION
  Configuration and data under ProgramData\Ferrum are kept by default —
  pass -Purge to also delete them and the installed binary.

.EXAMPLE
  .\uninstall-service.ps1
.EXAMPLE
  .\uninstall-service.ps1 -Purge
#>
[CmdletBinding()]
param(
    [switch]$Purge,
    [string]$InstallDir = (Join-Path $env:ProgramFiles "Ferrum"),
    [string]$DataDir = (Join-Path $env:ProgramData "Ferrum")
)

$ErrorActionPreference = "Stop"

$existing = Get-Service -Name Ferrum -ErrorAction SilentlyContinue
if ($existing) {
    Write-Host "==> Stopping service 'Ferrum'"
    Stop-Service -Name Ferrum -Force -ErrorAction SilentlyContinue
    & sc.exe delete Ferrum | Out-Null
} else {
    Write-Host "==> No 'Ferrum' service registered"
}

if ($Purge) {
    Write-Host "==> Removing $InstallDir and $DataDir"
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $InstallDir
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $DataDir
} else {
    Write-Host "==> Keeping $InstallDir and $DataDir (pass -Purge to remove them)"
}

Write-Host "==> Done."
