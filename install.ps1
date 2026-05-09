# ccx installer for Windows (PowerShell 5.1+ / 7+)
#
# Usage:
#   irm https://raw.githubusercontent.com/channel-spoonai/ccx/main/install.ps1 | iex
#
# Pin a version or install path:
#   $env:CCX_VERSION = "v0.1.0"
#   $env:CCX_BIN_DIR = "C:\Tools\ccx"
#   irm https://raw.githubusercontent.com/channel-spoonai/ccx/main/install.ps1 | iex

[CmdletBinding()]
param(
    [string]$Version = $env:CCX_VERSION,
    [string]$BinDir  = $env:CCX_BIN_DIR
)

$ErrorActionPreference = 'Stop'
$Repo = 'channel-spoonai/ccx'

function Info($msg) { Write-Host "→ $msg" -ForegroundColor Cyan }
function Ok($msg)   { Write-Host "✓ $msg"  -ForegroundColor Green }
function Warn($msg) { Write-Host "⚠ $msg"  -ForegroundColor Yellow }

# Default install path
if (-not $BinDir) {
    $BinDir = Join-Path $env:LOCALAPPDATA 'Programs\ccx'
}

# Detect architecture
$archRaw = $env:PROCESSOR_ARCHITECTURE
switch ($archRaw) {
    'AMD64' { $Arch = 'amd64' }
    'ARM64' { $Arch = 'arm64' }
    'x86'   { $Arch = 'amd64' }  # 32비트 호스트에서도 64비트 받게
    default { throw "unsupported architecture: $archRaw" }
}

# arm64 Windows 빌드는 goreleaser 설정에서 제외했으므로 amd64로 fallback (x64 emulation)
if ($Arch -eq 'arm64') {
    Warn "No native Windows arm64 build available; installing amd64 binary (x64 emulation)."
    $Arch = 'amd64'
}

# Force TLS 1.2 (PowerShell 5.1)
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

# Resolve latest version
if (-not $Version) {
    Info 'fetching latest release...'
    $headers = @{ 'User-Agent' = 'ccx-installer' }
    $latest = Invoke-RestMethod -Headers $headers -Uri "https://api.github.com/repos/$Repo/releases/latest"
    $Version = $latest.tag_name
    if (-not $Version) { throw "could not resolve latest release." }
}

$VerNum = $Version.TrimStart('v')
$Archive = "ccx-$VerNum-windows-$Arch.zip"
$Url = "https://github.com/$Repo/releases/download/$Version/$Archive"

# Temporary working directory
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("ccx-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null

try {
    $zipPath = Join-Path $tmp $Archive
    Info "downloading: $Url"
    try {
        Invoke-WebRequest -Uri $Url -OutFile $zipPath -UseBasicParsing
    } catch {
        throw "download failed. check version/arch: $Url`n$($_.Exception.Message)"
    }

    Info 'extracting'
    Expand-Archive -Path $zipPath -DestinationPath $tmp -Force

    # Locate the binary
    $exe = Get-ChildItem -Path $tmp -Recurse -Filter 'ccx.exe' | Select-Object -First 1
    if (-not $exe) { throw "could not find ccx.exe in archive." }

    # Install
    if (-not (Test-Path $BinDir)) {
        New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
    }
    $target = Join-Path $BinDir 'ccx.exe'

    # ccx may be running — handle copy failure
    try {
        Copy-Item -Path $exe.FullName -Destination $target -Force
    } catch {
        throw "copy failed (ccx may be running): $($_.Exception.Message)"
    }

    Ok "installed ccx $Version at: $target"

    # Add to user PATH
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $pathEntries = @()
    if ($userPath) { $pathEntries = $userPath -split ';' | Where-Object { $_ -ne '' } }

    if ($pathEntries -notcontains $BinDir) {
        $newPath = ($pathEntries + $BinDir) -join ';'
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        Ok "added to PATH: $BinDir"
        Warn 'open a new terminal for the PATH change to take effect.'
    } else {
        Info "already on PATH: $BinDir"
    }
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

Write-Host ''
Write-Host 'Config file lookup (priority order):' -ForegroundColor White
Write-Host '  1. $env:CCX_CONFIG'
Write-Host '  2. %APPDATA%\ccx\ccx.config.json'
Write-Host ''
Write-Host "Example config: https://github.com/$Repo/blob/main/ccx.config.example.json"
Write-Host ''
Write-Host 'Usage:' -ForegroundColor White
Write-Host '  ccx                                    # interactive profile menu'
Write-Host "  ccx -xSet 'GLM Coding Plan' -p 'hi'    # run with a specific profile"
