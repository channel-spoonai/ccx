# ccx installer for Windows (PowerShell 5.1+ / 7+)
#
# Usage:
#   irm https://raw.githubusercontent.com/channel-spoonai/ccx/main/install.ps1 | iex
#
# 특정 버전 또는 설치 경로 지정:
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

# 기본 경로
if (-not $BinDir) {
    $BinDir = Join-Path $env:LOCALAPPDATA 'Programs\ccx'
}

# 아키텍처 감지
$archRaw = $env:PROCESSOR_ARCHITECTURE
switch ($archRaw) {
    'AMD64' { $Arch = 'amd64' }
    'ARM64' { $Arch = 'arm64' }
    'x86'   { $Arch = 'amd64' }  # 32비트 호스트에서도 64비트 받게
    default { throw "지원하지 않는 아키텍처: $archRaw" }
}

# arm64 Windows 빌드는 goreleaser 설정에서 제외했으므로 amd64로 fallback (x64 emulation)
if ($Arch -eq 'arm64') {
    Warn "Windows arm64 빌드가 없어 amd64 바이너리로 설치합니다 (x64 에뮬레이션)."
    $Arch = 'amd64'
}

# TLS 1.2 강제 (PowerShell 5.1)
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

# 최신 버전 조회
if (-not $Version) {
    Info '최신 릴리즈 조회 중...'
    $headers = @{ 'User-Agent' = 'ccx-installer' }
    $latest = Invoke-RestMethod -Headers $headers -Uri "https://api.github.com/repos/$Repo/releases/latest"
    $Version = $latest.tag_name
    if (-not $Version) { throw "최신 릴리즈를 가져올 수 없습니다." }
}

$VerNum = $Version.TrimStart('v')
$Archive = "ccx-$VerNum-windows-$Arch.zip"
$Url = "https://github.com/$Repo/releases/download/$Version/$Archive"

# 임시 작업 디렉터리
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("ccx-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null

try {
    $zipPath = Join-Path $tmp $Archive
    Info "다운로드: $Url"
    try {
        Invoke-WebRequest -Uri $Url -OutFile $zipPath -UseBasicParsing
    } catch {
        throw "다운로드 실패. 버전/아키텍처를 확인하세요: $Url`n$($_.Exception.Message)"
    }

    Info '압축 해제'
    Expand-Archive -Path $zipPath -DestinationPath $tmp -Force

    # 바이너리 검색
    $exe = Get-ChildItem -Path $tmp -Recurse -Filter 'ccx.exe' | Select-Object -First 1
    if (-not $exe) { throw "아카이브에서 ccx.exe를 찾을 수 없습니다." }

    # 설치
    if (-not (Test-Path $BinDir)) {
        New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
    }
    $target = Join-Path $BinDir 'ccx.exe'

    # 실행 중일 가능성 대비
    try {
        Copy-Item -Path $exe.FullName -Destination $target -Force
    } catch {
        throw "복사 실패 (ccx가 실행 중일 수 있습니다): $($_.Exception.Message)"
    }

    Ok "ccx $Version 설치 완료: $target"

    # 사용자 PATH에 추가
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $pathEntries = @()
    if ($userPath) { $pathEntries = $userPath -split ';' | Where-Object { $_ -ne '' } }

    if ($pathEntries -notcontains $BinDir) {
        $newPath = ($pathEntries + $BinDir) -join ';'
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        Ok "PATH에 추가됨: $BinDir"
        Warn '새 터미널을 열어야 PATH 변경이 반영됩니다.'
    } else {
        Info "이미 PATH에 있음: $BinDir"
    }
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

Write-Host ''
Write-Host '설정 파일 위치 (우선순위):' -ForegroundColor White
Write-Host '  1. $env:CCX_CONFIG'
Write-Host '  2. %APPDATA%\ccx\ccx.config.json'
Write-Host ''
Write-Host "예제 설정: https://github.com/$Repo/blob/main/ccx.config.example.json"
Write-Host ''
Write-Host '사용법:' -ForegroundColor White
Write-Host '  ccx                                    # 프로파일 선택 메뉴'
Write-Host "  ccx -xSet 'GLM Coding Plan' -p '안녕'  # 프로파일 직접 지정"
