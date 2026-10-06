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

function Get-PackagePublisherId {
    param([string]$Publisher)
    # Windows derives the package family suffix from the first 64 bits of SHA-256(UTF-16LE publisher).
    $hash = [Security.Cryptography.SHA256]::HashData([Text.Encoding]::Unicode.GetBytes($Publisher))
    $bits = (-join ($hash[0..7] | ForEach-Object { [Convert]::ToString($_, 2).PadLeft(8, '0') })) + '0'
    $alphabet = '0123456789abcdefghjkmnpqrstvwxyz'
    -join (0..12 | ForEach-Object { $alphabet[[Convert]::ToInt32($bits.Substring($_ * 5, 5), 2)] })
}

# The identity of the MSIX package. Without a file it is the development identity, which is
# only good for a test package on this PC. For the Microsoft Store, pass a JSON file with the
# five values from Partner Center (Product management > Product identity, and the reserved
# name). That file belongs to the owner's account and is not kept in this repository.
function Get-MsixIdentity {
    param([string]$Path)
    if (-not $Path) {
        $publisher = 'CN=Burke Holland'
        return [pscustomobject]@{
            Store = $false
            IdentityName = 'BurkeHolland.ConvertMe.Development'
            Publisher = $publisher
            PublisherDisplayName = 'Burke Holland'
            DisplayName = 'Convert Me Development'
            PackageFamilyName = "BurkeHolland.ConvertMe.Development_$(Get-PackagePublisherId $publisher)"
        }
    }
    $json = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
    $values = @{}
    foreach ($field in 'identityName', 'publisher', 'publisherDisplayName', 'displayName', 'packageFamilyName') {
        $property = $json.PSObject.Properties[$field]
        $value = if ($property) { $property.Value } else { $null }
        if ($value -isnot [string] -or -not $value.Trim() -or $value -cne $value.Trim() -or $value.Length -gt 256) {
            throw "The Store identity file needs '$field', copied exactly from Partner Center."
        }
        $values[$field] = $value
    }
    if ($values.identityName -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]{2,49}$' -or $values.identityName -match '\.Development$') {
        throw "Not a Store package identity name: $($values.identityName)"
    }
    if ($values.publisher -notmatch '^CN=.+') { throw 'The Store publisher must be the complete CN=... value from Partner Center.' }
    $family = "$($values.identityName)_$(Get-PackagePublisherId $values.publisher)"
    if ($family -cne $values.packageFamilyName) {
        throw "Identity name and publisher give the package family $family, not $($values.packageFamilyName). Copy both from Partner Center exactly."
    }
    [pscustomobject]@{
        Store = $true
        IdentityName = $values.identityName
        Publisher = $values.publisher
        PublisherDisplayName = $values.publisherDisplayName
        DisplayName = $values.displayName
        PackageFamilyName = $family
    }
}
