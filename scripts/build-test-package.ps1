#Requires -Version 7.2
# Puts the test package of Convert Me together: the app from dist\, the File Explorer
# command, the package manifest and the logos. The result is a folder that Windows can
# register for the current user (scripts\register-test-package.ps1).
# This script registers nothing, signs nothing and publishes nothing.
[CmdletBinding()]
param([string]$Version)
. (Join-Path $PSScriptRoot 'common.ps1')
Add-Type -AssemblyName System.Drawing
$root = Split-Path $PSScriptRoot -Parent
if (-not $Version) { $Version = (Get-Content (Join-Path $root 'wails.json') -Raw | ConvertFrom-Json).info.productVersion }
$app = Join-Path $root "dist\ConvertMe-$Version-windows-x64"
$layout = Join-Path $root 'build\test-package\layout'
$manifestPath = Join-Path $root 'packaging\msix\AppxManifest.xml'
$manifest = [xml](Get-Content -LiteralPath $manifestPath -Raw)
$name = $manifest.Package.Identity.Name

# The app comes from the normal build, which has tested it. It must not be older than its source.
$exe = Join-Path $app 'ConvertMe.exe'
if (-not (Test-Path -LiteralPath $exe)) { throw "The app was not found in $app. Run scripts\build.ps1 first." }
$sources = @(Get-ChildItem -LiteralPath $root -Filter '*.go' -File) +
    @(Get-ChildItem -LiteralPath (Join-Path $root 'internal'), (Join-Path $root 'frontend\src') -Recurse -File) +
    @(Get-Item -LiteralPath (Join-Path $root 'frontend\index.html'), (Join-Path $root 'go.mod'), (Join-Path $root 'wails.json'))
$newer = @($sources | Where-Object { $_.Name -notlike '*_test.go' -and $_.LastWriteTimeUtc -gt (Get-Item -LiteralPath $exe).LastWriteTimeUtc })
if ($newer.Count) { throw "The app in dist is older than its source ($($newer[0].Name)). Run scripts\build.ps1 first." }

# A registered package uses its folder in place, so that folder must not change under it.
$registered = @(Get-AppxPackage -Name $name | Where-Object { $_.InstallLocation -and ($_.InstallLocation.TrimEnd('\') -ieq $layout) })
if ($registered.Count) { throw 'The test package is registered from this folder. Run scripts\register-test-package.ps1 -Remove first.' }

if ($manifest.Package.Identity.Version -ne "$Version.0") { throw "The manifest says version $($manifest.Package.Identity.Version), the app is $Version." }
$header = Get-Content -LiteralPath (Join-Path $root 'shellext\clsid.h') -Raw
if ($header -notmatch 'CONVERTME_COMMAND_CLSID "([0-9A-F-]{36})"') { throw 'shellext\clsid.h does not define the class id.' }
$classId = $Matches[1]
$namespaces = [Xml.XmlNamespaceManager]::new($manifest.NameTable)
$namespaces.AddNamespace('com', 'http://schemas.microsoft.com/appx/manifest/com/windows10')
$namespaces.AddNamespace('desktop5', 'http://schemas.microsoft.com/appx/manifest/desktop/windows10/5')
$named = @($manifest.SelectNodes('//com:Class/@Id | //desktop5:Verb/@Clsid', $namespaces) | ForEach-Object Value | Sort-Object -Unique)
if ($named.Count -ne 1 -or $named[0] -ne $classId) { throw "The manifest names class id $($named -join ', '), the command is $classId." }

& (Join-Path $PSScriptRoot 'build-shell-extension.ps1') -Version $Version
$command = Join-Path $root 'build\shellext\x64\ConvertMeCommand.dll'

if (Test-Path -LiteralPath $layout) { Remove-Item -LiteralPath $layout -Recurse -Force }
New-Item -ItemType Directory -Force (Join-Path $layout 'Assets') | Out-Null
Copy-Item -LiteralPath $exe, $command, $manifestPath -Destination $layout
Copy-Item -LiteralPath (Join-Path $app 'runtime'), (Join-Path $app 'licenses') -Destination $layout -Recurse
Copy-Item -LiteralPath (Join-Path $app 'PRIVACY.md'), (Join-Path $app 'THIRD-PARTY-NOTICES.md'), (Join-Path $app 'BUILD-INFO.txt') -Destination $layout

# The three logos a package must have, drawn from the app icon.
$icon = Join-Path $root 'build\appicon.png'
if (-not (Test-Path -LiteralPath $icon)) { & (Join-Path $PSScriptRoot 'generate-icon.ps1') }
$drawing = [Drawing.Image]::FromFile($icon)
try {
    foreach ($logo in @{ 'StoreLogo.png' = 50; 'Square44x44Logo.png' = 44; 'Square150x150Logo.png' = 150 }.GetEnumerator()) {
        $bitmap = [Drawing.Bitmap]::new($logo.Value, $logo.Value, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
        $canvas = [Drawing.Graphics]::FromImage($bitmap)
        try {
            $canvas.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
            $canvas.PixelOffsetMode = [Drawing.Drawing2D.PixelOffsetMode]::HighQuality
            $canvas.DrawImage($drawing, 0, 0, $logo.Value, $logo.Value)
            $bitmap.Save((Join-Path $layout "Assets\$($logo.Key)"), [Drawing.Imaging.ImageFormat]::Png)
        } finally { $canvas.Dispose(); $bitmap.Dispose() }
    }
} finally { $drawing.Dispose() }

& (Join-Path $PSScriptRoot 'verify-runtime.ps1') -RuntimePath (Join-Path $layout 'runtime') -ApplicationPath (Join-Path $layout 'ConvertMe.exe')

# Packing the folder makes Windows' own tool check the manifest and every file it names.
# The package file is not signed, so it cannot be installed. It is only kept as the result
# of that check.
$packer = Get-ChildItem -Path (Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin\*\x64\makeappx.exe') -ErrorAction SilentlyContinue |
    Sort-Object { [version]$_.Directory.Parent.Name } | Select-Object -Last 1
if ($packer) {
    $checked = Join-Path $root "build\test-package\ConvertMe-$Version-test-unsigned.msix"
    $output = & $packer.FullName pack /o /d $layout /p $checked 2>&1
    if ($LASTEXITCODE -ne 0) { throw "The package manifest was refused:`n$($output -join "`n")" }
    Write-Host "The manifest was checked with $($packer.Directory.Parent.Name)\makeappx.exe."
} else {
    Write-Host 'makeappx.exe (Windows SDK) was not found, so the manifest was not checked here. Registering it checks it as well.'
}
$files = @(Get-ChildItem -LiteralPath $layout -Recurse -File)
Write-Host ("The test package is ready in {0} ({1} files, {2:n1} MB)." -f $layout, $files.Count, (($files | Measure-Object Length -Sum).Sum / 1MB))
Write-Host 'Nothing was registered. To try it: scripts\register-test-package.ps1'
