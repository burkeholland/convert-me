#Requires -Version 7.2
# Builds the conversion engine (ffmpeg.exe and ffprobe.exe) from pinned, unmodified source.
# Everything happens under build\native. Nothing is installed on the system.
param([switch]$Rebuild)
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$native = Join-Path $root 'build\native'
$output = Join-Path $native 'output'
$prefix = Join-Path $native 'prefix'
$lock = Get-Content (Join-Path $PSScriptRoot 'runtime-lock.json') -Raw | ConvertFrom-Json
New-Item -ItemType Directory -Force $native, $output | Out-Null
# The third-party sources contain stray Go files. Go skips a folder that is its own module,
# so "go build ./..." and "go vet ./..." in the project never wander in here.
Set-Content (Join-Path $native 'go.mod') "module convertme.invalid/native-sources`n" -Encoding utf8NoBOM -NoNewline

$recipe = @('runtime-lock.json', 'configure-ffmpeg.sh', 'pkg-config-shim.sh', 'build-native.ps1')
$key = ($recipe | ForEach-Object { (Get-FileHash (Join-Path $PSScriptRoot $_) -Algorithm SHA256).Hash }) -join ':'
$receipt = Join-Path $output 'build-receipt.json'
if (-not $Rebuild -and (Test-Path $receipt)) {
    $saved = Get-Content $receipt -Raw | ConvertFrom-Json
    $valid = $saved.inputKey -eq $key
    foreach ($file in $saved.files) {
        $path = Join-Path $output $file.name
        $valid = $valid -and (Test-Path $path) -and
            ((Get-FileHash $path -Algorithm SHA256).Hash.ToLowerInvariant() -eq $file.sha256)
    }
    if ($valid) { Write-Host 'Verified cached native build.'; return }
}

$gcc = Find-Tool 'gcc.exe' @('C:\Strawberry\c\bin\gcc.exe')
$make = Find-Tool 'mingw32-make.exe' @('C:\Strawberry\c\bin\mingw32-make.exe')
$cmake = Find-Tool 'cmake.exe' @('C:\Strawberry\c\bin\cmake.exe')
$nasm = Find-Tool 'nasm.exe' @('C:\Strawberry\c\bin\nasm.exe')
# Git Bash only. bash.exe in System32 is the WSL launcher and must not be used.
$bash = @('C:\Program Files\Git\bin\bash.exe') + @(Get-Command bash.exe -All -ErrorAction SilentlyContinue |
    ForEach-Object Source | Where-Object { $_ -notmatch '\\(System32|WindowsApps)\\' }) |
    Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } | Select-Object -First 1
if (-not $bash) { throw 'Git for Windows Bash is required to configure FFmpeg and LAME.' }
$gitUtilities = Join-Path (Split-Path (Split-Path $bash)) 'usr\bin'
# GNU make accepts the forward-slash shell path, including its spaces.
$sh = (Join-Path (Split-Path $bash) 'sh.exe').Replace('\', '/')
# Windows' own tar. Git Bash also has a tar, and that one reads "X:\..." as a remote host.
$tar = Join-Path $env:SystemRoot 'System32\tar.exe'
$jobs = [Math]::Max(2, [Environment]::ProcessorCount)

function Get-FirstLine([string]$Tool, [string[]]$Arguments) {
    $lines = & $Tool @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Cannot read the version of $Tool." }
    ($lines | Select-Object -First 1).ToString().Trim()
}
$gccVersion = Get-FirstLine $gcc @('--version')
$cmakeVersion = Get-FirstLine $cmake @('--version')
$nasmVersion = Get-FirstLine $nasm @('-v')
$makeVersion = Get-FirstLine $make @('--version')

foreach ($asset in @($lock.zlib, $lock.libwebp, $lock.lame, $lock.ffmpeg)) {
    $archive = Get-VerifiedAsset $asset (Join-Path $native "downloads\$($asset.archive)")
    $source = Join-Path $native $asset.directory
    if ($Rebuild -and (Test-Path $source)) { Remove-Item -LiteralPath $source -Recurse -Force }
    if (-not (Test-Path $source)) { Invoke-Checked $tar @('-xf', $archive, '-C', $native) }
}
$webpBuild = Join-Path $native 'libwebp-build'
if ($Rebuild) {
    foreach ($stale in @($prefix, $webpBuild)) {
        if (Test-Path $stale) { Remove-Item -LiteralPath $stale -Recurse -Force }
    }
}
New-Item -ItemType Directory -Force "$prefix\include\lame", "$prefix\lib" | Out-Null

$oldPath = $env:PATH
try {
    # A short PATH keeps the build from picking up unrelated tools or libraries.
    $toolFolders = @((Split-Path $gcc), (Split-Path $make), (Split-Path $nasm), (Split-Path $cmake), $gitUtilities,
        "$env:SystemRoot\System32", $env:SystemRoot) | Select-Object -Unique
    $env:PATH = $toolFolders -join ';'

    # zlib: PNG, TIFF deflate and compressed container headers.
    Push-Location (Join-Path $native $lock.zlib.directory)
    try {
        Invoke-Checked $make @('-f', 'win32/Makefile.gcc', "-j$jobs", "SHELL=$sh", 'libz.a')
        Copy-Item 'libz.a' "$prefix\lib" -Force
        Copy-Item 'zlib.h', 'zconf.h' "$prefix\include" -Force
    } finally { Pop-Location }

    # libwebp: WebP encoding. Static, no threads, no command-line tools.
    Invoke-Checked $cmake @('-S', (Join-Path $native $lock.libwebp.directory), '-B', $webpBuild,
        '-G', 'MinGW Makefiles', "-DCMAKE_MAKE_PROGRAM=$($make.Replace('\', '/'))",
        "-DCMAKE_C_COMPILER=$($gcc.Replace('\', '/'))", '-DCMAKE_BUILD_TYPE=Release',
        '-DBUILD_SHARED_LIBS=OFF', "-DCMAKE_INSTALL_PREFIX=$($prefix.Replace('\', '/'))",
        '-DWEBP_USE_THREAD=OFF', '-DWEBP_BUILD_LIBWEBPMUX=ON',
        '-DWEBP_BUILD_ANIM_UTILS=OFF', '-DWEBP_BUILD_CWEBP=OFF', '-DWEBP_BUILD_DWEBP=OFF',
        '-DWEBP_BUILD_GIF2WEBP=OFF', '-DWEBP_BUILD_IMG2WEBP=OFF', '-DWEBP_BUILD_VWEBP=OFF',
        '-DWEBP_BUILD_WEBPINFO=OFF', '-DWEBP_BUILD_WEBPMUX=OFF', '-DWEBP_BUILD_EXTRAS=OFF')
    Invoke-Checked $cmake @('--build', $webpBuild, '--parallel', "$jobs")
    Invoke-Checked $cmake @('--install', $webpBuild)

    # LAME: MP3 encoding. Only the library is built, and it is copied instead of installed.
    $lameSource = Join-Path $native $lock.lame.directory
    $lameConfigure = './configure --build=x86_64-w64-mingw32 --host=x86_64-w64-mingw32 --disable-shared --enable-static ' +
        '--disable-frontend --disable-decoder --disable-gtktest --disable-dependency-tracking CFLAGS=-O2'
    $lameKey = "$lameConfigure|$($lock.lame.sha256)|$gccVersion"
    $lameReceipt = Join-Path $native 'lame-configure-key.txt'
    if (-not (Test-Path (Join-Path $lameSource 'config.h')) -or -not (Test-Path $lameReceipt) -or
        (Get-Content $lameReceipt -Raw).Trim() -ne $lameKey) {
        # A changed recipe starts from a clean copy of the source.
        if (Test-Path $lameSource) { Remove-Item -LiteralPath $lameSource -Recurse -Force }
        Invoke-Checked $tar @('-xf', (Join-Path $native "downloads\$($lock.lame.archive)"), '-C', $native)
        Push-Location $lameSource
        try { Invoke-Checked $bash @('-c', $lameConfigure) } finally { Pop-Location }
        $lameKey | Set-Content $lameReceipt -Encoding utf8NoBOM
    }
    Push-Location $lameSource
    try {
        # LAME's makefiles hard-code "make" for sub-folders, so the real make is named explicitly.
        # They also paste $(SHELL) into commands unquoted, so the shell path must not contain
        # spaces. The 8.3 short name of the Git folder (C:\PROGRA~1\...) provides that.
        $shortShell = (New-Object -ComObject Scripting.FileSystemObject).GetFile((Join-Path $gitUtilities 'sh.exe')).ShortPath
        if ($shortShell -match '\s') {
            throw "LAME needs a shell path without spaces, but '$shortShell' has one. Install Git for Windows in a folder without spaces or enable 8.3 short names."
        }
        Invoke-Checked $make @('-C', 'libmp3lame', "-j$jobs", "SHELL=$($shortShell.Replace('\', '/'))",
            "MAKE=$($make.Replace('\', '/'))")
        Copy-Item 'libmp3lame\.libs\libmp3lame.a' "$prefix\lib" -Force
        Copy-Item 'include\lame.h' "$prefix\include\lame" -Force
    } finally { Pop-Location }

    # FFmpeg: configured once per recipe, then built.
    Push-Location (Join-Path $native $lock.ffmpeg.directory)
    try {
        $configureKey = ((Get-FileHash (Join-Path $PSScriptRoot 'configure-ffmpeg.sh') -Algorithm SHA256).Hash) + ':' +
            ((Get-FileHash (Join-Path $PSScriptRoot 'pkg-config-shim.sh') -Algorithm SHA256).Hash) + ':' +
            ((Get-FileHash (Join-Path $PSScriptRoot 'runtime-lock.json') -Algorithm SHA256).Hash) + ':' + $gccVersion
        $configureReceipt = Join-Path $native 'ffmpeg-configure-key.txt'
        if (-not (Test-Path 'ffbuild\config.mak') -or -not (Test-Path $configureReceipt) -or
            (Get-Content $configureReceipt -Raw).Trim() -ne $configureKey) {
            Invoke-Checked $bash @((Join-Path $PSScriptRoot 'configure-ffmpeg.sh'))
            $configureKey | Set-Content $configureReceipt -Encoding utf8NoBOM
        }
        # V=1 is not about a noisy log. In brief mode FFmpeg wraps every command in a small
        # shell script, and Git Bash started by the native make cuts a command line at about
        # 8000 characters, which silently drops objects from libavcodec. With V=1 the long
        # archive command has no shell syntax, so make starts the archiver directly.
        Invoke-Checked $make @("-j$jobs", "SHELL=$sh", 'V=1')
    } finally { Pop-Location }
} finally { $env:PATH = $oldPath }

Copy-Item (Join-Path $native "$($lock.ffmpeg.directory)\ffmpeg.exe") $output -Force
Copy-Item (Join-Path $native "$($lock.ffmpeg.directory)\ffprobe.exe") $output -Force
$files = Get-ChildItem $output -Filter '*.exe' | Sort-Object Name | ForEach-Object {
    @{ name = $_.Name; sha256 = (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant(); size = $_.Length }
}
@{
    inputKey = $key; files = @($files)
    sources = @{
        ffmpeg = $lock.ffmpeg.version; zlib = $lock.zlib.version
        libwebp = $lock.libwebp.version; lame = $lock.lame.version
    }
    tools = @{ gcc = $gccVersion; make = $makeVersion; cmake = $cmakeVersion; nasm = $nasmVersion }
} | ConvertTo-Json -Depth 5 | Set-Content $receipt -Encoding utf8NoBOM
Write-Host "Native binaries staged at $output. The active runtime was not modified."
