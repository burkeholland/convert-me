#Requires -Version 7.2
# Builds, tests and packages Convert Me. Everything stays on this computer: the result is a
# folder and two zip files under dist\. Nothing is installed, signed, uploaded or published.
[CmdletBinding()]
param(
    [switch]$RebuildNative,
    [ValidatePattern('^\d+\.\d+\.\d+([.-][A-Za-z0-9.-]+)?$')]
    [string]$Version = '0.2.0'
)
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$tools = Use-GoToolchain
$go, $wails = $tools.Go, $tools.Wails
$npm = Find-Tool 'npm.cmd'
$exe = Join-Path $root 'build\bin\ConvertMe.exe'
$configuredVersion = (Get-Content (Join-Path $root 'wails.json') -Raw | ConvertFrom-Json).info.productVersion
if ($Version -ne $configuredVersion) { throw "Package version $Version differs from the product version $configuredVersion in wails.json." }
$sourceVersion = [regex]::Match((Get-Content (Join-Path $root 'internal\convert\types.go') -Raw), 'const Version = "([^"]+)"').Groups[1].Value
if ($Version -ne $sourceVersion) { throw "Package version $Version differs from the version $sourceVersion in internal\convert\types.go." }

Push-Location $root
try {
    $goCheck = & "$PSScriptRoot\verify-go-toolchain.ps1" -GoCommand $go
    & "$PSScriptRoot\prepare-runtime.ps1" -RebuildNative:$RebuildNative
    & "$PSScriptRoot\generate-icon.ps1"
    & "$PSScriptRoot\test-native.ps1"
    Push-Location (Join-Path $root 'frontend')
    try {
        Invoke-Checked $npm @('ci', '--no-audit', '--no-fund')
        Invoke-Checked $npm @('test')
        Invoke-Checked $npm @('run', 'build')
        Invoke-Checked $npm @('run', 'test:e2e')
    } finally { Pop-Location }
    $goSources = @(Get-ChildItem -LiteralPath $root -Filter '*.go' -File | ForEach-Object Name) + 'internal'
    $unformatted = @(& (Join-Path (Split-Path $go) 'gofmt.exe') -l @goSources)
    if ($unformatted.Count) { throw "These Go files are not formatted: $($unformatted -join ', ')" }
    Invoke-Checked $go @('test', './...')
    Invoke-Checked $go @('vet', './...')
    & "$PSScriptRoot\generate-notices.ps1"
    Invoke-Checked $wails @('build', '-clean', '-platform', 'windows/amd64', '-webview2', 'embed', '-s')
    if (-not (Test-Path $exe)) { throw 'The application was not built.' }
    $goCheck = & "$PSScriptRoot\verify-go-toolchain.ps1" -GoCommand $go -ApplicationPath $exe
    & "$PSScriptRoot\verify-runtime.ps1" -ApplicationPath $exe

    $dist = Join-Path $root 'dist'
    $name = "ConvertMe-$Version-windows-x64"
    $package = Join-Path $dist $name
    if (Test-Path $package) { Remove-Item -LiteralPath $package -Recurse -Force }
    New-Item -ItemType Directory -Force "$package\runtime\ffmpeg\bin", "$package\licenses" | Out-Null
    Copy-Item -LiteralPath $exe -Destination $package
    Copy-Item "$root\runtime\ffmpeg\bin\ffmpeg.exe", "$root\runtime\ffmpeg\bin\ffprobe.exe" "$package\runtime\ffmpeg\bin"
    & "$PSScriptRoot\verify-runtime.ps1" -RuntimePath "$package\runtime" -ApplicationPath "$package\ConvertMe.exe"
    # The README in the package may only point at files that are in the package: the
    # screenshot stays in the repository, and the license sits in the licenses folder.
    $readme = (Get-Content "$root\README.md" -Raw) -replace '(?m)^!\[[^\]]*\]\(docs/[^)]*\)\r?\n\r?\n', '' -replace '\]\(LICENSE\)', '](licenses/LICENSE)'
    Set-Content "$package\README.md" $readme -NoNewline -Encoding utf8NoBOM
    Copy-Item "$root\PRIVACY.md", "$root\THIRD-PARTY-NOTICES.md" $package
    Copy-Item "$root\LICENSE" "$package\licenses"
    Copy-Item "$root\notices" "$package\licenses" -Recurse
    foreach ($document in Get-ChildItem "$package\*.md") {
        foreach ($link in [regex]::Matches((Get-Content $document -Raw), '\]\((?!https?:|#)([^)#]+)')) {
            if (-not (Test-Path -LiteralPath (Join-Path $package $link.Groups[1].Value))) {
                throw "$($document.Name) in the package links to $($link.Groups[1].Value), which is not in the package."
            }
        }
    }

    $signature = Get-AuthenticodeSignature "$package\ConvertMe.exe"
    $goVersionText = (& $go version | Out-String).Trim()
    if ($LASTEXITCODE) { throw 'Could not record the Go toolchain version.' }
    $wailsVersionText = ((& $wails version | Out-String).Trim() -split "`r?`n")[0]
    if ($LASTEXITCODE) { throw 'Could not record the Wails version.' }
    $receipt = Get-Content (Join-Path $root 'build\native\output\build-receipt.json') -Raw | ConvertFrom-Json
    @(
        "Convert Me $Version for Windows x64"
        "Code signature: $($signature.Status)"
        "Go: $goVersionText"
        "Go version inside ConvertMe.exe: $($goCheck.Binary). Minimum in go.mod: $($goCheck.Required)"
        "Wails: $wailsVersionText"
        "Engine: FFmpeg $($receipt.sources.ffmpeg) with zlib $($receipt.sources.zlib), libwebp $($receipt.sources.libwebp) and LAME $($receipt.sources.lame)"
        "Engine compiler: $($receipt.tools.gcc)"
        'The engine source, its build recipe and the hashes of its inputs are in the native-source archive.'
        'Checks run for this build: verify-runtime.ps1, test-native.ps1 (real conversions), npm test,'
        'npm run build, npm run test:e2e, gofmt, go test ./..., go vet ./...'
        'This is a local build. It is not code signed and it was not published anywhere.'
    ) | Set-Content "$package\BUILD-INFO.txt" -Encoding utf8NoBOM

    $sourceArchive = & "$PSScriptRoot\package-native-source.ps1" -Version $Version -OutputDirectory $dist |
        Where-Object { $_ -is [IO.FileInfo] } | Select-Object -Last 1
    $zip = "$package.zip"
    if (Test-Path $zip) { Remove-Item -LiteralPath $zip -Force }
    Compress-Archive -LiteralPath $package -DestinationPath $zip -CompressionLevel Optimal
    $archives = @((Get-Item $zip), $sourceArchive)
    @($archives | ForEach-Object {
        "$((Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant())  $($_.Name)"
    }) | Set-Content "$dist\SHA256SUMS.txt" -Encoding ascii
    Write-Host "Convert Me $Version is built, tested and packaged in $dist"
    Write-Host "Code signature: $($signature.Status). Nothing was published."
} finally { Pop-Location }
