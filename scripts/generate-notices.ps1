#Requires -Version 7.2
# Collects the license texts of everything that ships with Convert Me into notices\.
# The texts come straight from the pinned source archives and from the Go module cache,
# so they always match the versions that were built.
param()
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$target = Join-Path $root 'notices'
$native = Join-Path $root 'build\native'
$lock = Get-Content (Join-Path $PSScriptRoot 'runtime-lock.json') -Raw | ConvertFrom-Json
$tools = Use-GoToolchain
New-Item -ItemType Directory -Force $target | Out-Null

function Copy-Notice([string]$Source, [string]$Name) {
    if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) {
        throw "License text not found: $Source. Run scripts\prepare-runtime.ps1 first."
    }
    Copy-Item -LiteralPath $Source -Destination (Join-Path $target $Name) -Force
}
$ffmpeg = Join-Path $native $lock.ffmpeg.directory
Copy-Notice (Join-Path $ffmpeg 'COPYING.LGPLv2.1') 'FFmpeg-LGPL-2.1.txt'
Copy-Notice (Join-Path $ffmpeg 'LICENSE.md') 'FFmpeg-LICENSE.md'
$lame = Join-Path $native $lock.lame.directory
Copy-Notice (Join-Path $lame 'COPYING') 'LAME-LGPL-2.0.txt'
Copy-Notice (Join-Path $lame 'LICENSE') 'LAME-LICENSE.txt'
$webp = Join-Path $native $lock.libwebp.directory
Copy-Notice (Join-Path $webp 'COPYING') 'libwebp-COPYING.txt'
Copy-Notice (Join-Path $webp 'PATENTS') 'libwebp-PATENTS.txt'
Copy-Notice (Join-Path $native "$($lock.zlib.directory)\LICENSE") 'zlib-LICENSE.txt'

$goRoot = (& $tools.Go env GOROOT | Out-String).Trim()
if ($LASTEXITCODE -or -not $goRoot) { throw 'Cannot locate the Go installation for its license.' }
Copy-Notice (Join-Path $goRoot 'LICENSE') 'Go-LICENSE.txt'

# Only the modules that end up inside the Windows app are listed. Test-only modules are not shipped.
Push-Location $root
try {
    $lines = & $tools.Go list -tags 'desktop,production' -deps -f '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}' .
    if ($LASTEXITCODE) { throw 'Cannot list the Go modules in the app. Run "go mod download" first.' }
} finally { Pop-Location }
$lines = $lines | Where-Object { $_ } | Sort-Object -Unique
$modules = Join-Path $target 'go-modules'
if (Test-Path $modules) { Remove-Item -LiteralPath $modules -Recurse -Force }
New-Item -ItemType Directory -Force $modules | Out-Null
$index = [Collections.Generic.List[string]]::new()
$index.Add('Go modules that are compiled into ConvertMe.exe, with the folder that holds each license.')
$index.Add('The Go runtime has its own notice (Go-LICENSE.txt). Modules used only by tests are not shipped.')
$webviewLoaderLicense = $null
foreach ($line in $lines) {
    $parts = $line.Split('|')
    if ($parts[0] -eq 'convertme') { continue }
    if ($parts.Count -ne 3 -or -not $parts[2]) { throw "Module source missing: $line. Run `"go mod download`"." }
    $name = ($parts[0] + '@' + $parts[1]) -replace '[^a-zA-Z0-9._@-]', '_'
    $licenses = @(Get-ChildItem $parts[2] -File | Where-Object { $_.Name -match '^(LICENSE|LICENCE|COPYING|NOTICE|PATENTS)([._-]|$)' })
    if ($licenses.Count -eq 0) { throw "No license file found for module $($parts[0]). Look into it before distributing." }
    $moduleTarget = Join-Path $modules $name
    New-Item -ItemType Directory -Force $moduleTarget | Out-Null
    $licenses | Copy-Item -Destination $moduleTarget -Force
    if ($parts[0] -eq 'github.com/wailsapp/wails/v2') {
        foreach ($nested in @('internal\frontend\desktop\windows\winc\LICENSE', 'internal\go-common-file-dialog\LICENSE')) {
            Copy-Item (Join-Path $parts[2] $nested) (Join-Path $moduleTarget ($nested.Replace('\', '_') + '.txt')) -Force
        }
    }
    if ($parts[0] -eq 'github.com/wailsapp/go-webview2') {
        $webviewLoaderLicense = Join-Path $moduleTarget 'webviewloader-LICENSE.txt'
        Copy-Item (Join-Path $parts[2] 'webviewloader\LICENSE') $webviewLoaderLicense -Force
    }
    $index.Add("$($parts[0]) $($parts[1]) -> go-modules\$name")
}
if (-not $webviewLoaderLicense -or -not (Test-Path $webviewLoaderLicense -PathType Leaf) -or
    (Get-Item $webviewLoaderLicense).Length -eq 0) {
    throw 'The WebView2 loader license must be present before distributing.'
}
$index | Set-Content (Join-Path $target 'GO-MODULES.txt') -Encoding utf8NoBOM

# These standard texts are kept in the repository because the compiler does not ship them.
foreach ($required in @('MinGW-w64-COPYING.txt', 'MinGW-w64-CRT-COPYING.txt', 'MinGW-w64-AUTHORS.txt',
        'MinGW-w64-Winpthreads-COPYING.txt', 'GCC-RUNTIME-EXCEPTION.txt', 'GCC-GPL-3.txt')) {
    if (-not (Test-Path (Join-Path $target $required))) { throw "Required notice missing: notices\$required" }
}
Write-Host "Collected notices for the engine, its libraries and $($index.Count - 2) Go modules."
