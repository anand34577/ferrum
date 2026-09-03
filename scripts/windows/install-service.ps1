#requires -RunAsAdministrator
<#
.SYNOPSIS
  Installs ferrum as a Windows service.

.DESCRIPTION
  Copies ferrum.exe into Program Files, seeds a config file under
  ProgramData\Ferrum from config.example.yaml (an existing config is left
  untouched), and registers + starts a "Ferrum" Windows service pointed at
  it. ferrum.exe detects it is running under the Service Control Manager
  (cmd\ferrum\service_windows.go) and manages its own start/stop lifecycle —
  no wrapper (NSSM etc.) needed.

  Run this from an extracted release archive (expects .\ferrum.exe and
  ..\config.example.yaml next to it, i.e. the layout scripts/build.sh
  produces), or pass -BinPath explicitly.

.PARAMETER BinPath
  Path to the ferrum.exe to install. Defaults to ferrum.exe next to this
  script.

.PARAMETER InstallDir
  Where to copy the binary. Defaults to "$env:ProgramFiles\Ferrum".

.PARAMETER DataDir
  Config + database directory. Defaults to "$env:ProgramData\Ferrum".

.EXAMPLE
  .\install-service.ps1
#>
[CmdletBinding()]
param(
    [string]$BinPath = (Join-Path $PSScriptRoot "ferrum.exe"),
    [string]$InstallDir = (Join-Path $env:ProgramFiles "Ferrum"),
    [string]$DataDir = (Join-Path $env:ProgramData "Ferrum")
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $BinPath)) {
    throw "ferrum.exe not found at '$BinPath'. Pass -BinPath, or run this from an extracted release archive."
}

$configExample = Join-Path (Split-Path $PSScriptRoot -Parent) "config.example.yaml"
if (-not (Test-Path $configExample)) {
    $configExample = Join-Path $PSScriptRoot "config.example.yaml"
}

Write-Host "==> Installing binary to $InstallDir"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item -Path $BinPath -Destination (Join-Path $InstallDir "ferrum.exe") -Force

Write-Host "==> Preparing data directory $DataDir"
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null

$configPath = Join-Path $DataDir "config.yaml"
if (-not (Test-Path $configPath)) {
    if (Test-Path $configExample) {
        $content = Get-Content $configExample -Raw
        $dbPath = (Join-Path $DataDir "ferrum.db") -replace '\\', '/'
        $content = $content -replace '(?m)^(\s*path:\s*).*$', "`$1$dbPath"
        Set-Content -Path $configPath -Value $content -Encoding utf8
        Write-Host "    wrote $configPath (edit before exposing this beyond localhost)"
    } else {
        Write-Warning "config.example.yaml not found — create $configPath manually before starting the service."
    }
} else {
    Write-Host "    $configPath already exists, leaving it alone"
}

$logPath = Join-Path $DataDir "ferrum.log"
$exePath = Join-Path $InstallDir "ferrum.exe"
$binaryPathName = '"' + $exePath + '" -config "' + $configPath + '" -log-file "' + $logPath + '"'

$existing = Get-Service -Name Ferrum -ErrorAction SilentlyContinue
if ($existing) {
    Write-Host "==> Service 'Ferrum' already exists — stopping to reconfigure"
    Stop-Service -Name Ferrum -Force -ErrorAction SilentlyContinue
    & sc.exe config Ferrum binPath= $binaryPathName start= auto | Out-Null
} else {
    Write-Host "==> Registering service 'Ferrum'"
    & sc.exe create Ferrum binPath= $binaryPathName start= auto DisplayName= "Ferrum" | Out-Null
    & sc.exe description Ferrum "Ferrum - Proxmox VE fleet control" | Out-Null
}

# SIGTERM-equivalent graceful stop happens inside ferrum.exe (service_windows.go);
# give Windows a bit more than its own 15s wait before declaring it hung.
& sc.exe failure Ferrum reset= 86400 actions= restart/5000 | Out-Null

Write-Host "==> Starting service"
Start-Service -Name Ferrum
Get-Service -Name Ferrum | Format-Table -AutoSize

Write-Host ""
Write-Host "Config: $configPath"
Write-Host "Data:   $DataDir"
Write-Host "Logs:   $logPath"
