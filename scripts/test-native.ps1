#Requires -Version 7.2
# Runs real conversions with the bundled engine: every test file in scripts\fixtures to every
# format the app offers, plus batch, cancel and overwrite safety. The results are checked by
# reading them back and, for pictures, with decoders that are independent of the engine.
param(
    [string]$RuntimePath = (Join-Path (Split-Path $PSScriptRoot -Parent) 'runtime'),
    [string]$Run = 'Engine',
    [switch]$Detailed
)
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$engine = Join-Path $RuntimePath 'ffmpeg\bin'
foreach ($tool in 'ffmpeg.exe', 'ffprobe.exe') {
    if (-not (Test-Path (Join-Path $engine $tool) -PathType Leaf)) {
        throw "The engine is not in $engine. Run scripts\prepare-runtime.ps1 first."
    }
}
$tools = Use-GoToolchain
$testBinary = Join-Path $root 'build\tests\convert.test.exe'
New-Item -ItemType Directory -Force (Split-Path $testBinary) | Out-Null
Push-Location $root
try {
    Invoke-Checked $tools.Go @('test', '-c', '-o', $testBinary, './internal/convert')
} finally { Pop-Location }

# The tests run with a PATH that only has Windows itself on it. That proves the app never
# leans on an FFmpeg, a codec pack or a runtime library that happens to be installed here.
$saved = @{ PATH = $env:PATH; Engine = $env:CONVERTME_TEST_ENGINE; Fixtures = $env:CONVERTME_TEST_FIXTURES }
try {
    $env:PATH = "$env:SystemRoot\System32;$env:SystemRoot"
    $env:CONVERTME_TEST_ENGINE = $engine
    $env:CONVERTME_TEST_FIXTURES = Join-Path $PSScriptRoot 'fixtures'
    $arguments = @('-test.run', $Run, '-test.count', '1', '-test.timeout', '20m')
    if ($Detailed) { $arguments += '-test.v' }
    Invoke-Checked $testBinary $arguments
} finally {
    $env:PATH = $saved.PATH
    $env:CONVERTME_TEST_ENGINE = $saved.Engine
    $env:CONVERTME_TEST_FIXTURES = $saved.Fixtures
}
Write-Host 'Real conversion tests passed.'
