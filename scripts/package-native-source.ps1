#Requires -Version 7.2
# Packs everything needed to rebuild the conversion engine: the four unmodified source
# archives, the exact build recipe, the build receipt and the license texts.
# The LGPL asks that this goes wherever the app goes, so build.ps1 always makes both.
param(
    [ValidatePattern('^\d+\.\d+\.\d+([.-][A-Za-z0-9.-]+)?$')]
    [string]$Version = '0.1.0',
    [string]$OutputDirectory = (Join-Path (Split-Path $PSScriptRoot -Parent) 'build\source-package-validation')
)
. (Join-Path $PSScriptRoot 'common.ps1')
Add-Type -AssemblyName System.IO.Compression.FileSystem
$root = Split-Path $PSScriptRoot -Parent
$lock = Get-Content "$PSScriptRoot\runtime-lock.json" -Raw | ConvertFrom-Json
$recipe = @('runtime-lock.json', 'configure-ffmpeg.sh', 'pkg-config-shim.sh', 'build-native.ps1')
$receiptPath = Join-Path $root 'build\native\output\build-receipt.json'
if (-not (Test-Path $receiptPath)) { throw 'The engine has not been built yet. Run scripts\prepare-runtime.ps1 first.' }
$receipt = Get-Content $receiptPath -Raw | ConvertFrom-Json
$inputs = $recipe | ForEach-Object { (Get-FileHash (Join-Path $PSScriptRoot $_) -Algorithm SHA256).Hash }
if ($receipt.inputKey -ne ($inputs -join ':')) { throw 'The build recipe changed after the engine was built. Build the engine again first.' }
foreach ($file in $receipt.files) {
    if ((Get-FileHash "$root\build\native\output\$($file.name)" -Algorithm SHA256).Hash.ToLowerInvariant() -ne $file.sha256) {
        throw "The engine file differs from its build receipt: $($file.name)"
    }
}

$name = "ConvertMe-$Version-native-source"
$source = Join-Path $OutputDirectory $name
if (Test-Path $source) { Remove-Item -LiteralPath $source -Recurse -Force }
New-Item -ItemType Directory -Force "$source\build\native\downloads", "$source\scripts" | Out-Null
foreach ($asset in @($lock.ffmpeg, $lock.zlib, $lock.libwebp, $lock.lame)) {
    $archive = Get-VerifiedAsset $asset "$root\build\native\downloads\$($asset.archive)"
    Copy-Item $archive "$source\build\native\downloads"
}
Copy-Item "$PSScriptRoot\common.ps1" "$source\scripts"
foreach ($file in $recipe) { Copy-Item (Join-Path $PSScriptRoot $file) "$source\scripts" }
Copy-Item $receiptPath "$source\build-receipt.json"
Copy-Item "$root\notices" "$source\notices" -Recurse
Copy-Item "$root\THIRD-PARTY-NOTICES.md", "$root\LICENSE" $source
@(
    'Convert Me: source of the conversion engine'
    ''
    "This archive belongs to Convert Me $Version for Windows x64."
    'It holds everything needed to rebuild runtime\ffmpeg\bin\ffmpeg.exe and ffprobe.exe.'
    ''
    'What is inside'
    "- build\native\downloads: the exact, unmodified source archives of FFmpeg $($lock.ffmpeg.version),"
    "  zlib $($lock.zlib.version), libwebp $($lock.libwebp.version) and LAME $($lock.lame.version)."
    '  Their download addresses and SHA-256 hashes are in scripts\runtime-lock.json.'
    '- scripts\configure-ffmpeg.sh: the exact FFmpeg configuration. LGPL only, no network code.'
    '- scripts\build-native.ps1: builds the three libraries and then FFmpeg, all static.'
    '- build-receipt.json: compiler versions and the hashes of the binaries that were shipped.'
    '- notices and THIRD-PARTY-NOTICES.md: the license of every component.'
    '- SHA256SUMS.txt: a hash for every file in this archive.'
    ''
    'How to rebuild on Windows x64'
    'You need PowerShell 7.2 or later, Git for Windows (for its Bash), and a MinGW-w64 GCC'
    'toolchain with mingw32-make, CMake and NASM. The shipped binaries were built with the'
    'toolchain that comes with Strawberry Perl: GCC 13.2.0 (x86_64, UCRT, posix threads, SEH).'
    ''
    'From this folder run:   .\scripts\build-native.ps1 -Rebuild'
    'The result is in:       build\native\output'
    ''
    'To change the engine, extract a source archive under build\native, edit it, and run the'
    'script without -Rebuild. Delete build\native\output\build-receipt.json first, because an'
    'old receipt makes the script reuse the previous binaries.'
    ''
    'Using your own build with the app'
    'The app checks the two engine files against hashes that are compiled into it. To use a'
    'modified engine, build the app from its source after running scripts\prepare-runtime.ps1,'
    'which writes new hashes. You are free to do this: FFmpeg and LAME are under the LGPL, and'
    'nothing in the license of Convert Me restricts modifying or replacing them.'
    ''
    'The same bytes are not guaranteed with a different compiler version.'
) | Set-Content "$source\README.txt" -Encoding utf8NoBOM

$sourceFiles = @(Get-ChildItem $source -File -Recurse | Sort-Object FullName)
$checksums = @{}
@($sourceFiles | ForEach-Object {
    $relative = [IO.Path]::GetRelativePath($source, $_.FullName).Replace('\', '/')
    $hash = (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    $checksums[$relative] = $hash
    "$hash  $relative"
}) | Set-Content "$source\SHA256SUMS.txt" -Encoding ascii
$zip = "$source.zip"
if (Test-Path $zip) { Remove-Item -LiteralPath $zip -Force }
Compress-Archive -LiteralPath $source -DestinationPath $zip -CompressionLevel Optimal

# Read the finished archive back and compare every file with its hash.
$archive = [IO.Compression.ZipFile]::OpenRead($zip)
try {
    foreach ($entry in $archive.Entries) {
        $relative = $entry.FullName.Replace('\', '/').Substring($name.Length + 1)
        if (-not $checksums.ContainsKey($relative)) { continue }
        $stream = $entry.Open()
        $sha = [Security.Cryptography.SHA256]::Create()
        try { $actual = [Convert]::ToHexString($sha.ComputeHash($stream)).ToLowerInvariant() }
        finally { $stream.Dispose(); $sha.Dispose() }
        if ($actual -ne $checksums[$relative]) { throw "A file in the source archive failed its hash check: $relative" }
        $checksums.Remove($relative)
    }
    if ($checksums.Count) { throw 'The source archive is missing files that are in its checksum list.' }
} finally { $archive.Dispose() }
Write-Host "Engine source archive verified: $zip"
Get-Item -LiteralPath $zip
