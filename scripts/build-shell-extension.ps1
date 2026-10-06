#Requires -Version 7.2
# Builds and tests ConvertMeCommand.dll, the File Explorer command of the packaged version,
# and probe.exe, the small program that tests it. Needs the Microsoft C++ build tools
# (Visual Studio 2022 or its Build Tools, with "Desktop development with C++").
# Nothing is installed and nothing is registered. The output goes to build\shellext\x64.
[CmdletBinding()]
param([string]$Version)
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
if (-not $Version) { $Version = (Get-Content (Join-Path $root 'wails.json') -Raw | ConvertFrom-Json).info.productVersion }
if ($Version -notmatch '^(\d+)\.(\d+)\.(\d+)') { throw "The version $Version does not start with three numbers." }
$numbers = "$($Matches[1]),$($Matches[2]),$($Matches[3]),0"
$source = Join-Path $root 'shellext'
$out = Join-Path $root 'build\shellext\x64'

$vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
if (-not (Test-Path -LiteralPath $vswhere)) { throw 'The Microsoft C++ build tools were not found. Install Visual Studio Build Tools 2022 with "Desktop development with C++".' }
$studio = & $vswhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
$environment = if ($studio) { Join-Path $studio 'VC\Auxiliary\Build\vcvars64.bat' }
if (-not $environment -or -not (Test-Path -LiteralPath $environment)) { throw 'The Microsoft C++ compiler for x64 was not found. Add "Desktop development with C++" to Visual Studio.' }

if (Test-Path -LiteralPath $out) { Remove-Item -LiteralPath $out -Recurse -Force }
New-Item -ItemType Directory -Force $out | Out-Null
@("#define VERSION_NUMBERS $numbers", "#define VERSION_TEXT `"$Version`"") | Set-Content (Join-Path $out 'version.h') -Encoding ascii

# The compiler only works inside the environment that vcvars64.bat sets up, so every step
# runs in one command file. Warnings are errors. The C++ runtime is linked in, so the DLL
# needs nothing that is not part of Windows. /Brepro leaves the time of the build out, so
# the same source and the same tools always give the same file.
$compile = '/nologo /std:c++17 /permissive- /utf-8 /W4 /WX /O1 /MT /EHsc /GS /guard:cf /sdl /Brepro /DUNICODE /D_UNICODE'
$link = '/NOLOGO /INCREMENTAL:NO /DEBUG:NONE /OPT:REF /OPT:ICF /guard:cf /DYNAMICBASE /NXCOMPAT /HIGHENTROPYVA /CETCOMPAT /Brepro'
@(
    '@echo off'
    # vcvars64.bat looks for vswhere.exe on PATH.
    "set `"PATH=%PATH%;$(Split-Path $vswhere)`""
    "call `"$environment`" >nul || exit /b 1"
    "cd /d `"$out`" || exit /b 1"
    "rc /nologo /I `"$out`" /fo ConvertMeCommand.res `"$source\ConvertMeCommand.rc`" || exit /b 1"
    "cl $compile /I `"$source`" `"$source\ConvertMeCommand.cpp`" ConvertMeCommand.res /Fe:ConvertMeCommand.dll /link /DLL /DEF:`"$source\ConvertMeCommand.def`" $link kernel32.lib user32.lib ole32.lib shell32.lib shlwapi.lib || exit /b 1"
    "cl $compile /I `"$source`" `"$source\probe.cpp`" /Fe:probe.exe /link $link kernel32.lib ole32.lib shell32.lib || exit /b 1"
    'dumpbin /nologo /dependents ConvertMeCommand.dll > dependents.txt || exit /b 1'
    'dumpbin /nologo /exports ConvertMeCommand.dll > exports.txt || exit /b 1'
) | Set-Content (Join-Path $out 'build.cmd') -Encoding ascii
& $env:ComSpec /d /c "`"$out\build.cmd`""
if ($LASTEXITCODE -ne 0) { throw 'The File Explorer command could not be built.' }
$dll = Join-Path $out 'ConvertMeCommand.dll'
$probe = Join-Path $out 'probe.exe'

# The DLL may only need Windows itself, and must offer exactly the two COM entry points.
$allowed = 'KERNEL32.dll', 'USER32.dll', 'ole32.dll', 'SHELL32.dll', 'SHLWAPI.dll'
$needed = @(Get-Content (Join-Path $out 'dependents.txt') | ForEach-Object { $_.Trim() } | Where-Object { $_ -match '^\S+\.dll$' })
$extra = @($needed | Where-Object { $_ -notin $allowed })
if (-not $needed.Count -or $extra.Count) { throw "ConvertMeCommand.dll needs unexpected libraries: $($extra -join ', ')" }
$exports = @(Get-Content (Join-Path $out 'exports.txt') | ForEach-Object { if ($_ -match '^\s+\d+\s+[0-9A-F]+\s+[0-9A-F]+\s+(\S+)') { $Matches[1] } } | Sort-Object)
if (($exports -join ',') -ne 'DllCanUnloadNow,DllGetClassObject') { throw "ConvertMeCommand.dll exports unexpected names: $($exports -join ', ')" }

# Test 1: what the command says about itself.
$stage = Join-Path $out 'test'
$captured = Join-Path $stage 'out'
New-Item -ItemType Directory -Force $captured | Out-Null
Copy-Item -LiteralPath $dll -Destination $stage
# Next to the DLL, the test program stands in for the app and saves what it is handed.
Copy-Item -LiteralPath $probe -Destination (Join-Path $stage 'ConvertMe.exe')
$stagedDll = Join-Path $stage 'ConvertMeCommand.dll'
$described = @(& $probe describe --dll $stagedDll)
if ($LASTEXITCODE -ne 0) { throw "The command could not be loaded: $described" }
$expected = @(
    'title=0x00000000 Convert with Convert Me'
    "icon=0x00000000 $stage\ConvertMe.exe,0"
    'state=0x00000000 0'
    'flags=0x00000000 0'
)
if (Compare-Object $described $expected -CaseSensitive) { throw "The command describes itself as:`n$($described -join "`n")`nExpected:`n$($expected -join "`n")" }

# Test 2: what the command hands to the app. The names include spaces, signs that mean
# something on a command line, and letters outside ASCII.
$fixture = Join-Path $root 'scripts\fixtures\picture-rgb.png'
$names = 'plain.png', 'two words & a (sign) 100%.png', "$([char]0xDC)bung #1 $([char]0x5199)$([char]0x771F).png", '-starts with a dash.png'
$paths = foreach ($name in $names) {
    $path = Join-Path $stage $name
    Copy-Item -LiteralPath $fixture -Destination $path
    $path
}
$pathList = Join-Path $stage 'paths.txt'
[IO.File]::WriteAllLines($pathList, [string[]]$paths, [Text.UTF8Encoding]::new($false))
$env:CONVERTME_PROBE_OUT = $captured
try {
    $invoked = @(& $probe invoke --dll $stagedDll $pathList)
    if ($LASTEXITCODE -ne 0) { throw "The command failed: $invoked" }
    $list = Join-Path $captured 'convertme-files-captured.txt'
    $commandLine = Join-Path $captured 'command-line.txt'
    $deadline = (Get-Date).AddSeconds(15)
    while (-not (Test-Path -LiteralPath $commandLine) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 100 }
    if (-not (Test-Path -LiteralPath $commandLine)) { throw 'The command did not start the app.' }
} finally { Remove-Item Env:CONVERTME_PROBE_OUT }
$started = [IO.File]::ReadAllText($commandLine)
$pattern = '^"' + [regex]::Escape("$stage\ConvertMe.exe") + '" --files-from "([^"]+\\convertme-files-\d+-\d+-\d+\.txt)"$'
if ($started -notmatch $pattern) { throw "The command started the app with an unexpected command line: $started" }
$wanted = "Convert Me file list 1`n" + ($paths -join "`n") + "`n"
if ([IO.File]::ReadAllText($list) -cne $wanted) { throw "The list the command wrote is not the list of selected files:`n$([IO.File]::ReadAllText($list))" }

# Test 3: the app reads that same list with its own code.
$go = (Use-GoToolchain).Go
$env:CONVERTME_TEST_SHELL_LIST = $list
$env:CONVERTME_TEST_SHELL_PATHS = $pathList
Push-Location $root
try {
    $result = @(& $go test -count=1 -v -run '^TestReadsTheListTheShellExtensionWrote$' . 2>&1)
    if ($LASTEXITCODE -ne 0 -or -not ($result -match '^--- PASS: TestReadsTheListTheShellExtensionWrote')) {
        throw "The app did not read the list the command wrote:`n$($result -join "`n")"
    }
} finally {
    Pop-Location
    Remove-Item Env:CONVERTME_TEST_SHELL_LIST, Env:CONVERTME_TEST_SHELL_PATHS
}
Remove-Item -LiteralPath $stage -Recurse -Force

Write-Host "Built and tested the File Explorer command: $dll ($((Get-Item $dll).Length) bytes)"
