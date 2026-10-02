#Requires -Version 7.2
# Builds the conversion engine if needed, checks it, and puts it where the app looks for it
# (runtime\ffmpeg\bin). It also writes the manifest that the app embeds and verifies at startup.
param([switch]$RebuildNative, [switch]$StageOnly)
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$lock = Get-Content (Join-Path $PSScriptRoot 'runtime-lock.json') -Raw | ConvertFrom-Json
& (Join-Path $PSScriptRoot 'build-native.ps1') -Rebuild:$RebuildNative

$stage = Join-Path $root 'build\runtime-staging'
if (Test-Path $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
New-Item -ItemType Directory -Force "$stage\ffmpeg\bin" | Out-Null
Copy-Item "$root\build\native\output\ffmpeg.exe", "$root\build\native\output\ffprobe.exe" "$stage\ffmpeg\bin"
& (Join-Path $PSScriptRoot 'verify-runtime.ps1') -RuntimePath $stage -NoManifest
if ($StageOnly) {
    Write-Host "Verified engine staged at $stage. The active runtime and manifest were not changed."
    return
}

# Close Convert Me before running this script: Windows will not replace a program that is running.
$runtime = Join-Path $root 'runtime'
$backup = Join-Path $root 'build\runtime-previous'
if (Test-Path $backup) { Remove-Item -LiteralPath $backup -Recurse -Force }
if (Test-Path $runtime) { Move-Item -LiteralPath $runtime -Destination $backup }
try {
    Move-Item -LiteralPath $stage -Destination $runtime
} catch {
    if (Test-Path $backup) { Move-Item -LiteralPath $backup -Destination $runtime }
    throw
}

$files = @(Get-ChildItem $runtime -File -Recurse | Sort-Object FullName | ForEach-Object {
    [ordered]@{
        path = [IO.Path]::GetRelativePath($root, $_.FullName).Replace('\', '/')
        sha256 = (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        size = $_.Length
    }
})
$manifest = [ordered]@{
    engine = [ordered]@{
        ffmpeg = $lock.ffmpeg.version; zlib = $lock.zlib.version
        libwebp = $lock.libwebp.version; lame = $lock.lame.version
    }
    files = $files
}
New-Item -ItemType Directory -Force (Join-Path $root 'assets') | Out-Null
$manifest | ConvertTo-Json -Depth 4 | Set-Content -Encoding utf8NoBOM (Join-Path $root 'assets\runtime-manifest.json')
& (Join-Path $PSScriptRoot 'verify-runtime.ps1')
Write-Host "Runtime ready: $($files.Count) verified engine files."
