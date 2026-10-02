$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Invoke-Checked {
    param([string]$Command, [string[]]$Arguments = @())
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Command failed with exit code $LASTEXITCODE."
    }
}

function Find-Tool {
    param([string]$Name, [string[]]$Fallbacks = @())
    $command = Get-Command $Name -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($command) { return $command.Source }
    foreach ($candidate in $Fallbacks) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) { return $candidate }
    }
    throw "Required build tool not found: $Name. See README.md for build prerequisites."
}

function Get-VerifiedAsset {
    param($Asset, [string]$Destination)
    if ((Test-Path -LiteralPath $Destination) -and
        (Get-FileHash -LiteralPath $Destination -Algorithm SHA256).Hash.ToLowerInvariant() -eq $Asset.sha256) {
        return $Destination
    }
    New-Item -ItemType Directory -Force (Split-Path $Destination -Parent) | Out-Null
    $partial = "$Destination.partial"
    try {
        Invoke-Checked 'curl.exe' @('--fail', '--location', '--retry', '3',
            '--connect-timeout', '30', '--max-time', '1800', '--silent', '--show-error',
            '--output', $partial, $Asset.url)
        if ((Get-FileHash -LiteralPath $partial -Algorithm SHA256).Hash.ToLowerInvariant() -ne $Asset.sha256) {
            throw "SHA-256 mismatch: $Destination"
        }
        Move-Item -LiteralPath $partial -Destination $Destination -Force
    } finally {
        if (Test-Path -LiteralPath $partial) { Remove-Item -LiteralPath $partial -Force }
    }
    return $Destination
}

# The Go toolchain and Wails CLI are not always on PATH. Wails shells out to "go", so both
# folders are added for this process only. Nothing outside the process is changed.
function Use-GoToolchain {
    $go = Find-Tool 'go.exe' @('C:\Program Files\Go\bin\go.exe')
    $wails = Find-Tool 'wails.exe' @((Join-Path $HOME 'go\bin\wails.exe'))
    foreach ($folder in @((Split-Path $go), (Split-Path $wails))) {
        if (($env:PATH -split ';') -notcontains $folder) { $env:PATH = "$folder;$env:PATH" }
    }
    [pscustomobject]@{ Go = $go; Wails = $wails }
}
