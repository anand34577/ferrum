<#
.SYNOPSIS
  Builds the frontend and the ferrum.exe binary for local development on
  Windows (host GOOS/GOARCH only). For cross-platform release builds, use
  scripts/build.sh (bash — works under Git Bash/WSL, and is what
  .github/workflows/release.yml runs).

.EXAMPLE
  .\scripts\build.ps1
.EXAMPLE
  .\scripts\build.ps1 -SkipFrontend   # reuse the existing web/dist
#>
[CmdletBinding()]
param(
    [switch]$SkipFrontend,
    [string]$Version,
    [string]$OutDir = "dist"
)

$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

if (-not $Version) {
    try {
        $Version = (git describe --tags --always --dirty 2>$null)
        if (-not $Version) { $Version = "dev" }
    } catch {
        $Version = "dev"
    }
}
$Commit = try { (git rev-parse --short HEAD 2>$null) } catch { "none" }
if (-not $Commit) { $Commit = "none" }
$Date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$ldflags = "-s -w -X main.version=$Version -X main.commit=$Commit -X main.date=$Date"

if (-not $SkipFrontend) {
    Write-Host "==> Building frontend (web/dist)"
    Push-Location web
    try {
        npm ci
        npm run build
    } finally {
        Pop-Location
    }
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$bin = Join-Path $OutDir "ferrum.exe"

Write-Host "==> Building ferrum.exe ($Version, $Commit, $Date)"
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags $ldflags -o $bin ./cmd/ferrum
Remove-Item Env:\CGO_ENABLED

Copy-Item README.md, LICENSE, config.example.yaml -Destination $OutDir -Force
Copy-Item scripts\windows\install-service.ps1, scripts\windows\uninstall-service.ps1 -Destination $OutDir -Force

Write-Host "==> Done: $bin"
