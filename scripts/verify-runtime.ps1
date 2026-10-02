#Requires -Version 7.2
# Checks that the conversion engine is exactly what Convert Me is allowed to ship:
# two self-contained x64 programs, LGPL only, no network code, and the formats the recipe asked for.
param(
    [string]$RuntimePath = (Join-Path (Split-Path $PSScriptRoot -Parent) 'runtime'),
    [string]$ManifestPath = (Join-Path (Split-Path $PSScriptRoot -Parent) 'assets\runtime-manifest.json'),
    [string]$ApplicationPath,
    [switch]$NoManifest
)
. (Join-Path $PSScriptRoot 'common.ps1')
if (-not ('ConvertMe.PEImports' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.IO;
using System.Text;
namespace ConvertMe {
    public static class PEImports {
        public static string[] Read(string path) {
            byte[] b = File.ReadAllBytes(path);
            if (b.Length < 128 || b[0] != 'M' || b[1] != 'Z') throw new Exception("Not a PE file: " + path);
            int pe = BitConverter.ToInt32(b, 0x3c);
            if (BitConverter.ToUInt32(b, pe) != 0x4550 || BitConverter.ToUInt16(b, pe + 4) != 0x8664)
                throw new Exception("Expected Windows x64 PE: " + path);
            int count = BitConverter.ToUInt16(b, pe + 6), opt = pe + 24;
            if (BitConverter.ToUInt16(b, opt) != 0x20b) throw new Exception("Expected PE32+: " + path);
            int sections = opt + BitConverter.ToUInt16(b, pe + 20);
            Func<uint, int> offset = rva => {
                for (int i = 0; i < count; i++) {
                    int s = sections + i * 40;
                    uint start = BitConverter.ToUInt32(b, s + 12);
                    uint size = Math.Max(BitConverter.ToUInt32(b, s + 8), BitConverter.ToUInt32(b, s + 16));
                    if (rva >= start && rva < start + size)
                        return checked((int)(rva - start + BitConverter.ToUInt32(b, s + 20)));
                }
                throw new Exception("Invalid PE RVA: " + path);
            };
            var result = new List<string>();
            Action<uint> add = rva => {
                int p = offset(rva), end = p;
                while (b[end] != 0) end++;
                result.Add(Encoding.ASCII.GetString(b, p, end - p));
            };
            uint imports = BitConverter.ToUInt32(b, opt + 120);
            if (imports != 0)
                for (int p = offset(imports); BitConverter.ToUInt32(b, p + 12) != 0; p += 20)
                    add(BitConverter.ToUInt32(b, p + 12));
            uint delay = BitConverter.ToUInt32(b, opt + 112 + 13 * 8);
            if (delay != 0)
                for (int p = offset(delay); BitConverter.ToUInt32(b, p + 4) != 0; p += 32) {
                    if ((BitConverter.ToUInt32(b, p) & 1) == 0) throw new Exception("Unsupported VA delay imports: " + path);
                    add(BitConverter.ToUInt32(b, p + 4));
                }
            return result.ToArray();
        }
    }
}
'@
}

# DLLs that are part of every supported Windows installation.
$system = @(
    'ADVAPI32.dll', 'BCRYPT.dll', 'BCRYPTPRIMITIVES.dll', 'COMBASE.dll', 'COMCTL32.dll', 'COMDLG32.dll',
    'CRYPT32.dll', 'D3D11.dll', 'DWMAPI.dll', 'DXGI.dll', 'GDI32.dll', 'IMM32.dll', 'KERNEL32.dll',
    'KERNELBASE.dll', 'MF.dll', 'MFPLAT.dll', 'MFUUID.dll', 'MSVCRT.dll', 'NTDLL.dll', 'OLE32.dll',
    'OLEAUT32.dll', 'POWRPROF.dll', 'PROPSYS.dll', 'PSAPI.dll', 'RPCRT4.dll', 'SHCORE.dll', 'SHELL32.dll',
    'SHLWAPI.dll', 'UCRTBASE.dll', 'USER32.dll', 'USERENV.dll', 'UXTHEME.dll', 'VERSION.dll', 'WINMM.dll',
    'WS2_32.dll'
)
$engineFiles = @('ffmpeg\bin\ffmpeg.exe', 'ffmpeg\bin\ffprobe.exe')
foreach ($name in $engineFiles) {
    if (-not (Test-Path (Join-Path $RuntimePath $name) -PathType Leaf)) { throw "Missing engine file: $name" }
}
foreach ($file in Get-ChildItem $RuntimePath -Recurse -File) {
    $relative = [IO.Path]::GetRelativePath($RuntimePath, $file.FullName)
    if ($relative -notin $engineFiles) { throw "Unexpected file in the runtime folder: $relative" }
}

# Every DLL the programs need must come with Windows. Nothing may need a separate install.
$nativeFiles = @(Get-ChildItem $RuntimePath -Recurse -File)
if ($ApplicationPath) { $nativeFiles += Get-Item -LiteralPath $ApplicationPath }
foreach ($file in $nativeFiles) {
    foreach ($dependency in [ConvertMe.PEImports]::Read($file.FullName)) {
        if ($dependency -match '^(MSVCP\d|VCRUNTIME\d|VCOMP\d|libgcc|libstdc\+\+|libwinpthread)') {
            throw "Forbidden separately installed runtime: $($file.Name) imports $dependency"
        }
        if ($dependency -in $system -or $dependency -match '^(api-ms-win-|ext-ms-win-).+\.dll$') { continue }
        throw "Unexpected DLL dependency: $($file.Name) imports $dependency"
    }
}

$lock = Get-Content (Join-Path $PSScriptRoot 'runtime-lock.json') -Raw | ConvertFrom-Json
$ffmpeg = Join-Path $RuntimePath 'ffmpeg\bin\ffmpeg.exe'
$probe = Join-Path $RuntimePath 'ffmpeg\bin\ffprobe.exe'

function Get-EngineText([string]$Tool, [string[]]$Arguments) {
    $text = (& $Tool @Arguments 2>&1 | Out-String)
    if ($LASTEXITCODE -ne 0) { throw "$([IO.Path]::GetFileName($Tool)) $($Arguments -join ' ') failed." }
    $text
}

# Version and license. Only the two libraries this project builds itself may be linked in.
foreach ($tool in @($ffmpeg, $probe)) {
    $name = [IO.Path]::GetFileNameWithoutExtension($tool)
    $version = Get-EngineText $tool @('-version')
    if ($version -notmatch "(?m)^$name version $([regex]::Escape($lock.ffmpeg.version)) ") {
        throw "$name is not FFmpeg $($lock.ffmpeg.version)."
    }
    foreach ($required in '--disable-network', '--disable-gpl', '--disable-nonfree', '--disable-version3', '--disable-autodetect') {
        if (-not $version.Contains($required)) { throw "$name was not configured with $required." }
    }
    if ($version -match '--enable-(gpl|nonfree|version3)') { throw "$name was built with a license option that is not allowed." }
    foreach ($library in [regex]::Matches($version, '--enable-(lib[a-z0-9_]+)') | ForEach-Object { $_.Groups[1].Value }) {
        if ($library -notin 'libwebp', 'libmp3lame') { throw "$name links an unexpected library: $library" }
    }
    $license = Get-EngineText $tool @('-hide_banner', '-L')
    if ($license -notmatch 'GNU Lesser General Public\s+License' -or $license -notmatch 'version 2\.1') {
        throw "$name does not report the LGPL 2.1 or later license."
    }
}

# Local files only. A build with any network protocol must never ship.
$protocols = @((Get-EngineText $ffmpeg @('-hide_banner', '-protocols')) -split "`r?`n" |
    Where-Object { $_ -match '^\s+\S+\s*$' } | ForEach-Object { $_.Trim() } | Sort-Object -Unique)
if (($protocols -join ',') -ne 'file,pipe') {
    throw "The engine may only read and write local files, but it was built with: $($protocols -join ', ')"
}

# The encoders and muxers must be exactly the ones the recipe asks for.
$recipe = Get-Content (Join-Path $PSScriptRoot 'configure-ffmpeg.sh') -Raw
function Get-RecipeList([string]$Name) {
    if ($recipe -notmatch "(?m)^$Name=`"([a-z0-9_,]+)`"") { throw "configure-ffmpeg.sh has no $Name list." }
    @($Matches[1] -split ',' | Sort-Object)
}
function Get-EngineList([string]$Switch, [string]$Pattern) {
    @((Get-EngineText $ffmpeg @('-hide_banner', $Switch)) -split "`r?`n" |
        ForEach-Object { if ($_ -match $Pattern) { $Matches[1] } } | Sort-Object)
}
$builtEncoders = Get-EngineList '-encoders' '^\s[VAS][A-Z.]{5}\s+([a-z0-9_]+)\s'
$wantedEncoders = Get-RecipeList 'encoders'
if (($builtEncoders -join ',') -ne ($wantedEncoders -join ',')) {
    throw "Encoders differ from the recipe. Built: $($builtEncoders -join ' '). Wanted: $($wantedEncoders -join ' ')."
}
$builtMuxers = Get-EngineList '-muxers' '^\s+E\s+([a-z0-9_]+)\s'
$wantedMuxers = Get-RecipeList 'muxers'
if (($builtMuxers -join ',') -ne ($wantedMuxers -join ',')) {
    throw "Muxers differ from the recipe. Built: $($builtMuxers -join ' '). Wanted: $($wantedMuxers -join ' ')."
}

if (-not $NoManifest) {
    $manifest = Get-Content $ManifestPath -Raw | ConvertFrom-Json
    if ($manifest.engine.ffmpeg -ne $lock.ffmpeg.version) { throw 'The manifest names a different FFmpeg version than the lock file.' }
    $seen = @{}
    foreach ($file in @($manifest.files)) {
        if ($file.path -notmatch '^runtime/[a-zA-Z0-9._/-]+$' -or $file.path.Contains('..')) {
            throw "Unsafe manifest path: $($file.path)"
        }
        $relative = $file.path.Substring(8).Replace('/', '\')
        if ($seen.ContainsKey($relative)) { throw "Duplicate manifest path: $relative" }
        $seen[$relative] = $true
        $path = Join-Path $RuntimePath $relative
        if (-not (Test-Path $path) -or (Get-Item $path).Length -ne $file.size -or
            (Get-FileHash $path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $file.sha256) {
            throw "Engine file does not match the manifest: $relative"
        }
    }
    foreach ($name in $engineFiles) {
        if (-not $seen.ContainsKey($name)) { throw "The manifest does not list $name." }
    }
    if ($ApplicationPath) {
        $binary = [Text.Encoding]::Latin1.GetString([IO.File]::ReadAllBytes($ApplicationPath))
        $embedded = [Text.Encoding]::Latin1.GetString([IO.File]::ReadAllBytes($ManifestPath))
        if (-not $binary.Contains($embedded)) { throw 'The application embeds a different runtime manifest. Build the app again before packaging.' }
    }
}
Write-Host "Verified engine: FFmpeg $($lock.ffmpeg.version), LGPL only, local files only, no extra DLLs: $RuntimePath"
