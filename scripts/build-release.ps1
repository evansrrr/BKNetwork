param(
    [string]$OutputDir = "",
    [string]$ZipName = "BKNetwork.zip"
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($OutputDir)) {
    $OutputDir = Join-Path $repoRoot 'releases\bknetwork'
}

$webSource = Join-Path $repoRoot 'web'
$webTarget = Join-Path $OutputDir 'web'
$exeTarget = Join-Path $OutputDir 'bknetwork.exe'
$zipDir = Join-Path $repoRoot 'releases'
$zipTarget = Join-Path $repoRoot ('releases\' + $ZipName)

if (Test-Path $OutputDir) {
    Remove-Item $OutputDir -Recurse -Force
}
New-Item -ItemType Directory -Path $OutputDir | Out-Null
if (-not (Test-Path $zipDir)) {
    New-Item -ItemType Directory -Path $zipDir | Out-Null
}

Copy-Item -Path $webSource -Destination $webTarget -Recurse -Force

Push-Location $repoRoot
try {
    go build -o $exeTarget ./cmd/bknetwork
} finally {
    Pop-Location
}

if (Test-Path $zipTarget) {
    Remove-Item $zipTarget -Force
}

Compress-Archive -Path (Join-Path $OutputDir '*') -DestinationPath $zipTarget -Force

Write-Host "Release staged at $OutputDir"
Write-Host "Release archive created at $zipTarget"