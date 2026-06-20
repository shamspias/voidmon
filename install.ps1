<#
  ░▒▓█ VOIDMON INSTALLER (Windows) █▓▒░
  Usage:
    irm https://raw.githubusercontent.com/shamspias/voidmon/main/install.ps1 | iex
#>

#Requires -Version 5.0
$ErrorActionPreference = "Stop"

$Repo       = "shamspias/voidmon"
$InstallDir = Join-Path $env:LOCALAPPDATA "voidmon"

# ─── Detect architecture ─────────────────────
$arch = $env:PROCESSOR_ARCHITECTURE
switch ($arch) {
    "AMD64" { $goarch = "amd64" }
    "ARM64" { $goarch = "arm64" }
    "x86"   { Write-Host "  x  32-bit Windows is not supported."; exit 1 }
    default { Write-Host "  x  Unsupported architecture: $arch"; exit 1 }
}

Write-Host ""
Write-Host "  ::  VOIDMON INSTALLER"
Write-Host "  -----------------------------"
Write-Host "  OS:   windows"
Write-Host "  Arch: $goarch"
Write-Host ""

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$target = Join-Path $InstallDir "void.exe"

# ─── Find latest release ─────────────────────
$tag = $null
try {
    $rel = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" `
        -Headers @{ "User-Agent" = "voidmon-installer" }
    $tag = $rel.tag_name
} catch {
    $tag = $null
}

if ($tag) {
    $asset = "void-windows-$goarch.zip"
    $url   = "https://github.com/$Repo/releases/download/$tag/$asset"
    $tmp   = Join-Path $env:TEMP $asset

    Write-Host "  ->  Latest version: $tag"
    Write-Host "  ->  Downloading $asset ..."
    try {
        Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing
        Write-Host "  ->  Extracting ..."
        Expand-Archive -Path $tmp -DestinationPath $InstallDir -Force
        Remove-Item $tmp -Force
    } catch {
        Write-Host "  x  Download failed: $($_.Exception.Message)"
        Write-Host "  ->  Falling back to building from source ..."
        $tag = $null
    }
}

if (-not $tag) {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        Write-Host "  x  No release available and Go is not installed."
        Write-Host "  ->  Install Go (https://go.dev/dl/) or download a binary from:"
        Write-Host "      https://github.com/$Repo/releases"
        exit 1
    }
    $src = Join-Path $env:TEMP "voidmon-src"
    if (Test-Path $src) { Remove-Item $src -Recurse -Force }
    Write-Host "  ->  Cloning repository ..."
    git clone --depth 1 "https://github.com/$Repo.git" $src 2>$null
    Push-Location $src
    Write-Host "  ->  Building ..."
    go build -ldflags "-s -w" -o $target .
    Pop-Location
    Remove-Item $src -Recurse -Force
}

# ─── Add to user PATH ────────────────────────
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
    Write-Host "  ->  Added $InstallDir to your user PATH."
    Write-Host "      (open a new terminal for it to take effect)"
}

Write-Host ""
Write-Host "  ok  voidmon installed -> $target"
Write-Host "  ->  Run: void"
Write-Host ""
