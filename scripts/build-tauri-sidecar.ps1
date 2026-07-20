param(
    [string]$Version = ""
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
$arguments = @('run', './cmd/build-tauri-sidecar')
if (-not [string]::IsNullOrWhiteSpace($Version)) {
    $arguments += @('--version', $Version)
}

Push-Location $repoRoot
try {
    & go @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Sidecar build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}
