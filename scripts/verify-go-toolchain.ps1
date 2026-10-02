#Requires -Version 7.2
# Makes sure the Go toolchain that builds the app, and the one a finished app was built with,
# are at least the version go.mod asks for.
param([string]$ApplicationPath, [string]$GoCommand)
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
if (-not $GoCommand) { $GoCommand = Find-Tool 'go.exe' @('C:\Program Files\Go\bin\go.exe') }
$module = Get-Content (Join-Path $root 'go.mod') -Raw
$minimum = [regex]::Match($module, '(?m)^go\s+(\d+\.\d+(?:\.\d+)?)\s*$')
if (-not $minimum.Success) { throw 'go.mod must declare a stable Go minimum version.' }
$required = [version]$minimum.Groups[1].Value
$suggested = [regex]::Match($module, '(?m)^toolchain\s+go(\d+\.\d+(?:\.\d+)?)\s*$')
if ($suggested.Success -and [version]$suggested.Groups[1].Value -gt $required) {
    $required = [version]$suggested.Groups[1].Value
}
Push-Location $root
try {
    $selected = (& $GoCommand env GOVERSION | Out-String).Trim()
    if ($LASTEXITCODE) { throw 'Go could not select the toolchain required by go.mod.' }
    $selectedVersion = [regex]::Match($selected, '^go(\d+\.\d+(?:\.\d+)?)$')
    if (-not $selectedVersion.Success -or [version]$selectedVersion.Groups[1].Value -lt $required) {
        throw "Go toolchain $selected is below the go.mod requirement $required, or is not a stable release."
    }
    $binaryVersion = $null
    if ($ApplicationPath) {
        $metadata = @(& $GoCommand version -m $ApplicationPath)
        if ($LASTEXITCODE -ne 0 -or $metadata.Count -eq 0) { throw 'Cannot read the Go build information of the application.' }
        $binary = [regex]::Match($metadata[0], ':\s+go(\d+\.\d+(?:\.\d+)?)$')
        if (-not $binary.Success) { throw 'The application was not built with an identifiable stable Go release.' }
        $binaryVersion = $binary.Groups[1].Value
        if ([version]$binaryVersion -lt $required) {
            throw "The application uses Go $binaryVersion, below the go.mod requirement $required. Build it again with the newer toolchain."
        }
    }
    Write-Host "Verified Go toolchain $selected against the go.mod minimum $required."
    [pscustomobject]@{ Required = $required.ToString(); Toolchain = $selected; Binary = $binaryVersion }
} finally { Pop-Location }
