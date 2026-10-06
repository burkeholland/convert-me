#Requires -Version 7.2
# Makes the MSIX package of Convert Me from the app that scripts\build.ps1 has built and
# tested. The package holds the app, the File Explorer command, the conversion engine with
# its source, the license texts, the logos and the package manifest.
#
#   scripts\package-msix.ps1                          the test package, for this PC only
#   scripts\package-msix.ps1 -StoreIdentity <file>    the package for the Microsoft Store
#
# The test package has a development identity. scripts\register-test-package.ps1 registers
# its folder, build\msix\development\layout.
# The Store package needs the identity that Partner Center gave the product. It is read
# from a small JSON file that is not kept in this repository. README.md shows what is in
# it. The result is dist\ConvertMe-<version>-windows-x64-store.msix with a receipt next
# to it.
#
# This script registers nothing, signs nothing and uploads nothing. The Store package is
# left unsigned on purpose: the Microsoft Store signs what it publishes.
[CmdletBinding()]
param(
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version,
    [string]$StoreIdentity
)
. (Join-Path $PSScriptRoot 'common.ps1')
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.IO.Compression.FileSystem
$root = Split-Path $PSScriptRoot -Parent
$configuredVersion = (Get-Content (Join-Path $root 'wails.json') -Raw | ConvertFrom-Json).info.productVersion
if (-not $Version) { $Version = $configuredVersion }
if ($Version -ne $configuredVersion) { throw "Package version $Version differs from the product version $configuredVersion in wails.json." }
$parts = @($Version.Split('.') | ForEach-Object { [int]$_ })
if ($parts[0] -ge 65535 -or $parts[1] -gt 65535 -or $parts[2] -gt 65535) { throw "Version $Version cannot be turned into a package version." }
# A package version needs a first number above zero, and the Store keeps the fourth number
# for itself. So the app version 0.1.0 is the package version 1.1.0.0.
$packageVersion = '{0}.{1}.{2}.0' -f ($parts[0] + 1), $parts[1], $parts[2]
if ($StoreIdentity) { $StoreIdentity = (Resolve-Path -LiteralPath $StoreIdentity).Path }
$identity = Get-MsixIdentity $StoreIdentity
$flavor = if ($identity.Store) { 'store' } else { 'development' }

# The app comes from the normal build, which has tested it. It must not be older than its
# source, and the two documents that go into the package must be the ones it was built with.
$dist = Join-Path $root 'dist'
$app = Join-Path $dist "ConvertMe-$Version-windows-x64"
$exe = Join-Path $app 'ConvertMe.exe'
if (-not (Test-Path -LiteralPath $exe)) { throw "The app was not found in $app. Run scripts\build.ps1 first." }
$sources = @(Get-ChildItem -LiteralPath $root -Filter '*.go' -File) +
    @(Get-ChildItem -LiteralPath (Join-Path $root 'internal'), (Join-Path $root 'frontend\src') -Recurse -File) +
    @(Get-Item -LiteralPath (Join-Path $root 'frontend\index.html'), (Join-Path $root 'go.mod'), (Join-Path $root 'wails.json'))
$newer = @($sources | Where-Object { $_.Name -notlike '*_test.go' -and $_.LastWriteTimeUtc -gt (Get-Item -LiteralPath $exe).LastWriteTimeUtc })
if ($newer.Count) { throw "The app in dist is older than its source ($($newer[0].Name)). Run scripts\build.ps1 first." }
foreach ($document in 'PRIVACY.md', 'THIRD-PARTY-NOTICES.md') {
    if ((Get-FileHash -LiteralPath (Join-Path $root $document)).Hash -ne (Get-FileHash -LiteralPath (Join-Path $app $document)).Hash) {
        throw "$document changed after the app in dist was built. Run scripts\build.ps1 first."
    }
}
$info = (Get-Item -LiteralPath $exe).VersionInfo
$exeVersion = '{0}.{1}.{2}' -f $info.FileMajorPart, $info.FileMinorPart, $info.FileBuildPart
if ($exeVersion -ne $Version) { throw "ConvertMe.exe says it is version $exeVersion, not $Version." }
$goCheck = & (Join-Path $PSScriptRoot 'verify-go-toolchain.ps1') -ApplicationPath $exe
& (Join-Path $PSScriptRoot 'verify-runtime.ps1') -RuntimePath (Join-Path $app 'runtime') -ApplicationPath $exe
$go = Find-Tool 'go.exe' @('C:\Program Files\Go\bin\go.exe')
$wails = [regex]::Match((& $go version -m $exe | Out-String), '(?m)^\s*dep\s+github\.com/wailsapp/wails/v2\s+(v\S+)')
if (-not $wails.Success) { throw 'Cannot read the Wails version from ConvertMe.exe.' }

# The package carries the source of the engine itself. Make sure the archive is the one
# this build published, and that it was made for exactly these two engine files.
$sourceName = "ConvertMe-$Version-native-source"
$sourceArchive = Join-Path $dist "$sourceName.zip"
if (-not (Test-Path -LiteralPath $sourceArchive)) { throw "$sourceName.zip was not found in dist. Run scripts\build.ps1 first." }
$publishedSha256 = $null
foreach ($line in Get-Content -LiteralPath (Join-Path $dist 'SHA256SUMS.txt')) {
    if ($line -match '^([0-9a-f]{64}) [ *](.+)$' -and $Matches[2] -ceq "$sourceName.zip") { $publishedSha256 = $Matches[1] }
}
$sourceSha256 = (Get-FileHash -LiteralPath $sourceArchive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($sourceSha256 -ne $publishedSha256) { throw "$sourceName.zip does not match its line in dist\SHA256SUMS.txt. Run scripts\build.ps1 again." }
$zip = [IO.Compression.ZipFile]::OpenRead($sourceArchive)
try {
    $receiptEntry = @($zip.Entries | Where-Object { $_.FullName.Replace('\', '/') -ceq "$sourceName/build-receipt.json" })
    if ($receiptEntry.Count -ne 1) { throw "$sourceName.zip has no build-receipt.json." }
    $reader = [IO.StreamReader]::new($receiptEntry[0].Open())
    try { $engine = $reader.ReadToEnd() | ConvertFrom-Json } finally { $reader.Dispose() }
} finally { $zip.Dispose() }
foreach ($tool in 'ffmpeg.exe', 'ffprobe.exe') {
    $recorded = @($engine.files | Where-Object { $_.name -ceq $tool })
    $toolSha256 = (Get-FileHash -LiteralPath (Join-Path $app "runtime\ffmpeg\bin\$tool") -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($recorded.Count -ne 1 -or $recorded[0].sha256 -ne $toolSha256) { throw "runtime\ffmpeg\bin\$tool is not the file that $sourceName.zip was made for." }
}

function Find-SdkTool([string]$Name) {
    $candidates = @(Get-ChildItem (Join-Path ${env:ProgramFiles(x86)} "Windows Kits\10\bin\*\x64\$Name") -ErrorAction SilentlyContinue |
        Sort-Object { [version]$_.Directory.Parent.Name } -Descending | ForEach-Object FullName)
    Find-Tool $Name $candidates
}
function Invoke-Quiet([string]$Command, [string[]]$Arguments) {
    $output = & $Command @Arguments 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { throw "$Command failed with exit code $LASTEXITCODE.`n$output" }
}
$makeAppx = Find-SdkTool 'makeappx.exe'
$makePri = Find-SdkTool 'makepri.exe'

# A registered package uses its folder in place, so that folder must not change under it.
$stage = Join-Path $root "build\msix\$flavor"
$layout = Join-Path $stage 'layout'
$inUse = @(Get-AppxPackage -Name $identity.IdentityName | Where-Object { $_.InstallLocation -and ($_.InstallLocation.TrimEnd('\') -ieq $layout) })
if ($inUse.Count) {
    $how = if ($identity.Store) { "Remove-AppxPackage -Package $($inUse[0].PackageFullName)" } else { 'scripts\register-test-package.ps1 -Remove' }
    throw "Windows uses the folder $layout in place, because the package $($identity.IdentityName) is registered from it. Remove that package first: $how"
}

& (Join-Path $PSScriptRoot 'build-shell-extension.ps1') -Version $Version
$command = Join-Path $root 'build\shellext\x64\ConvertMeCommand.dll'

$priRoot = Join-Path $stage 'pri'
$verification = Join-Path $stage 'verification'
if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
New-Item -ItemType Directory -Force $layout, (Join-Path $priRoot 'Assets'), $verification, (Join-Path $layout 'source') | Out-Null
Copy-Item -LiteralPath $exe, $command, (Join-Path $app 'PRIVACY.md'), (Join-Path $app 'THIRD-PARTY-NOTICES.md') -Destination $layout
Copy-Item -LiteralPath (Join-Path $app 'runtime'), (Join-Path $app 'licenses') -Destination $layout -Recurse
Copy-Item -LiteralPath $sourceArchive -Destination (Join-Path $layout 'source')
@(
    "Convert Me $Version for Windows x64, package version $packageVersion"
    "Go version inside ConvertMe.exe: $($goCheck.Binary). Minimum in go.mod: $($goCheck.Required)"
    "Wails: $($wails.Groups[1].Value)"
    "Engine: FFmpeg $($engine.sources.ffmpeg) with zlib $($engine.sources.zlib), libwebp $($engine.sources.libwebp) and LAME $($engine.sources.lame)"
    "Engine compiler: $($engine.tools.gcc)"
    ''
    'The source of the engine is part of this package:'
    "source\$sourceName.zip"
    "SHA-256 $sourceSha256"
    'That archive holds the unmodified source of FFmpeg, LAME, libwebp and zlib, the build'
    'recipe, and a receipt with the hashes of the two engine files. Its README.txt says how'
    'to rebuild the engine. THIRD-PARTY-NOTICES.md lists every license, and the full texts'
    'are in the licenses folder.'
    ''
    if ($identity.Store) {
        'This package was not signed by its maker. The Microsoft Store signs the package that it publishes.'
    } else {
        'This is a test package with a development identity. It is not signed and it was not published anywhere.'
    }
) | Set-Content -LiteralPath (Join-Path $layout 'BUILD-INFO.txt') -Encoding utf8NoBOM
foreach ($document in Get-ChildItem -LiteralPath $layout -Filter '*.md') {
    foreach ($link in [regex]::Matches((Get-Content -LiteralPath $document.FullName -Raw), '\]\((?!https?:|#)([^)#]+)')) {
        if (-not (Test-Path -LiteralPath (Join-Path $layout $link.Groups[1].Value))) {
            throw "$($document.Name) in the package links to $($link.Groups[1].Value), which is not in the package."
        }
    }
}

# The logos, drawn from the app icon: one for every display scale, and the small sizes that
# the taskbar and the Start menu ask for.
$icon = Join-Path $root 'build\appicon.png'
if (-not (Test-Path -LiteralPath $icon)) { & (Join-Path $PSScriptRoot 'generate-icon.ps1') }
$assets = [ordered]@{}
foreach ($logo in @(@{ Name = 'StoreLogo'; Size = 50 }, @{ Name = 'Square44x44Logo'; Size = 44 }, @{ Name = 'Square150x150Logo'; Size = 150 })) {
    foreach ($scale in 100, 125, 150, 200, 400) {
        $assets["$($logo.Name).scale-$scale.png"] = [int][Math]::Round($logo.Size * $scale / 100, [MidpointRounding]::AwayFromZero)
    }
}
foreach ($size in 16, 24, 32, 48, 256) {
    $assets["Square44x44Logo.targetsize-$size.png"] = $size
    $assets["Square44x44Logo.targetsize-${size}_altform-unplated.png"] = $size
}
$drawing = [Drawing.Image]::FromFile($icon)
$attributes = [Drawing.Imaging.ImageAttributes]::new()
try {
    # Mirroring at the edges keeps the border of the icon from fading when it is scaled down.
    $attributes.SetWrapMode([Drawing.Drawing2D.WrapMode]::TileFlipXY)
    foreach ($name in $assets.Keys) {
        $size = $assets[$name]
        $bitmap = [Drawing.Bitmap]::new($size, $size, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
        $canvas = [Drawing.Graphics]::FromImage($bitmap)
        try {
            $canvas.CompositingMode = [Drawing.Drawing2D.CompositingMode]::SourceCopy
            $canvas.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
            $canvas.PixelOffsetMode = [Drawing.Drawing2D.PixelOffsetMode]::HighQuality
            $canvas.DrawImage($drawing, [Drawing.Rectangle]::new(0, 0, $size, $size),
                0, 0, $drawing.Width, $drawing.Height, [Drawing.GraphicsUnit]::Pixel, $attributes)
            $bitmap.Save((Join-Path $priRoot "Assets\$name"), [Drawing.Imaging.ImageFormat]::Png)
        } finally { $canvas.Dispose(); $bitmap.Dispose() }
    }
} finally { $attributes.Dispose(); $drawing.Dispose() }
Copy-Item -LiteralPath (Join-Path $priRoot 'Assets') -Destination $layout -Recurse

$manifest = (Get-Content -LiteralPath (Join-Path $root 'packaging\msix\AppxManifest.xml.in') -Raw).
    Replace('{{IDENTITY_NAME}}', [Security.SecurityElement]::Escape($identity.IdentityName)).
    Replace('{{PUBLISHER}}', [Security.SecurityElement]::Escape($identity.Publisher)).
    Replace('{{PUBLISHER_DISPLAY_NAME}}', [Security.SecurityElement]::Escape($identity.PublisherDisplayName)).
    Replace('{{PRODUCT_DISPLAY_NAME}}', [Security.SecurityElement]::Escape($identity.DisplayName)).
    Replace('{{VERSION}}', $packageVersion)
if ($manifest.Contains('{{')) { throw 'The manifest template has a placeholder that this script does not know.' }
$manifestPath = Join-Path $layout 'AppxManifest.xml'
Set-Content -LiteralPath $manifestPath -Value $manifest -Encoding utf8NoBOM -NoNewline

# resources.pri is the index that lets Windows pick the logo for the scale and the size it needs.
$priConfig = Join-Path $stage 'priconfig.xml'
@'
<?xml version="1.0" encoding="utf-8"?>
<resources targetOsVersion="10.0.0" majorVersion="1">
  <index root="\" startIndexAt="\">
    <default>
      <qualifier name="Language" value="en-US" />
      <qualifier name="Contrast" value="standard" />
      <qualifier name="Scale" value="100" />
      <qualifier name="HomeRegion" value="001" />
      <qualifier name="TargetSize" value="256" />
      <qualifier name="LayoutDirection" value="LTR" />
      <qualifier name="Theme" value="dark" />
      <qualifier name="AlternateForm" value="" />
      <qualifier name="DXFeatureLevel" value="DX9" />
      <qualifier name="Configuration" value="" />
      <qualifier name="DeviceFamily" value="Universal" />
      <qualifier name="Custom" value="" />
    </default>
    <indexer-config type="folder" foldernameAsQualifier="true" filenameAsQualifier="true" qualifierDelimiter="." />
  </index>
</resources>
'@ | Set-Content -LiteralPath $priConfig -Encoding utf8NoBOM
Invoke-Quiet $makePri @('new', '/pr', $priRoot, '/cf', $priConfig, '/mn', $manifestPath, '/of', (Join-Path $layout 'resources.pri'), '/o')
$priDump = Join-Path $stage 'resources.pri.xml'
Invoke-Quiet $makePri @('dump', '/if', (Join-Path $layout 'resources.pri'), '/of', $priDump, '/o')
[xml]$priIndex = Get-Content -LiteralPath $priDump -Raw
foreach ($logo in 'StoreLogo', 'Square44x44Logo', 'Square150x150Logo') {
    $drawn = @($assets.Keys | Where-Object { $_.StartsWith("$logo.") }).Count
    $indexed = @($priIndex.SelectNodes("//NamedResource[@name='$logo.png']/Candidate")).Count
    if ($indexed -ne $drawn) { throw "resources.pri lists $indexed of the $drawn $logo images." }
}

# Pack the folder, then unpack the result and compare it with the folder, file by file.
$packageName = "ConvertMe-$Version-windows-x64-$flavor.msix"
$package = Join-Path $(if ($identity.Store) { $dist } else { $stage }) $packageName
Invoke-Quiet $makeAppx @('pack', '/d', $layout, '/p', $package, '/o')
Invoke-Quiet $makeAppx @('unpack', '/p', $package, '/d', $verification, '/o')
$packed = @{}
foreach ($file in Get-ChildItem -LiteralPath $verification -Recurse -File) {
    $packed[[IO.Path]::GetRelativePath($verification, $file.FullName)] = $file.FullName
}
$staged = @(Get-ChildItem -LiteralPath $layout -Recurse -File)
foreach ($file in $staged) {
    $relative = [IO.Path]::GetRelativePath($layout, $file.FullName)
    if (-not $packed.ContainsKey($relative) -or
        (Get-FileHash -LiteralPath $packed[$relative]).Hash -ne (Get-FileHash -LiteralPath $file.FullName).Hash) {
        throw "The package differs from the folder it was made from: $relative"
    }
    $packed.Remove($relative)
}
# Only the two files that the packing tool adds may be extra. A signature would be a third one.
$extra = @($packed.Keys | Where-Object { $_ -notin 'AppxBlockMap.xml', '[Content_Types].xml' })
if ($extra.Count) { throw "Unexpected files in the package: $($extra -join ', ')" }

[xml]$document = Get-Content -LiteralPath (Join-Path $verification 'AppxManifest.xml') -Raw
$ns = [Xml.XmlNamespaceManager]::new($document.NameTable)
$ns.AddNamespace('f', 'http://schemas.microsoft.com/appx/manifest/foundation/windows10')
$ns.AddNamespace('uap', 'http://schemas.microsoft.com/appx/manifest/uap/windows10')
$ns.AddNamespace('uap10', 'http://schemas.microsoft.com/appx/manifest/uap/windows10/10')
$ns.AddNamespace('com', 'http://schemas.microsoft.com/appx/manifest/com/windows10')
$ns.AddNamespace('desktop5', 'http://schemas.microsoft.com/appx/manifest/desktop/windows10/5')
$packageIdentity = $document.SelectSingleNode('/f:Package/f:Identity', $ns)
$application = $document.SelectSingleNode('/f:Package/f:Applications/f:Application', $ns)
$header = Get-Content -LiteralPath (Join-Path $root 'shellext\clsid.h') -Raw
if ($header -notmatch 'CONVERTME_COMMAND_CLSID "([0-9A-F-]{36})"') { throw 'shellext\clsid.h does not define the class id.' }
$classId = $Matches[1]
$actual = [ordered]@{
    'identity name' = $packageIdentity.GetAttribute('Name')
    'publisher' = $packageIdentity.GetAttribute('Publisher')
    'version' = $packageIdentity.GetAttribute('Version')
    'architecture' = $packageIdentity.GetAttribute('ProcessorArchitecture')
    'package display name' = $document.SelectSingleNode('/f:Package/f:Properties/f:DisplayName', $ns).InnerText
    'publisher display name' = $document.SelectSingleNode('/f:Package/f:Properties/f:PublisherDisplayName', $ns).InnerText
    'application display name' = $application.SelectSingleNode('uap:VisualElements', $ns).GetAttribute('DisplayName')
    'executable' = $application.GetAttribute('Executable')
    'runtime behavior' = $application.GetAttribute('RuntimeBehavior', $ns.LookupNamespace('uap10'))
    'trust level' = $application.GetAttribute('TrustLevel', $ns.LookupNamespace('uap10'))
    'command class ids' = @($document.SelectNodes('//com:Class/@Id | //desktop5:Verb/@Clsid', $ns) | ForEach-Object Value | Sort-Object -Unique) -join ', '
    'command library' = @($document.SelectNodes('//com:Class/@Path', $ns) | ForEach-Object Value) -join ', '
}
$expected = [ordered]@{
    'identity name' = $identity.IdentityName
    'publisher' = $identity.Publisher
    'version' = $packageVersion
    'architecture' = 'x64'
    'package display name' = $identity.DisplayName
    'publisher display name' = $identity.PublisherDisplayName
    'application display name' = $identity.DisplayName
    'executable' = 'ConvertMe.exe'
    'runtime behavior' = 'packagedClassicApp'
    'trust level' = 'mediumIL'
    'command class ids' = $classId
    'command library' = 'ConvertMeCommand.dll'
}
foreach ($key in $expected.Keys) {
    if ($actual[$key] -cne $expected[$key]) { throw "The manifest in the package has $key '$($actual[$key])', expected '$($expected[$key])'." }
}
$capabilities = @($document.SelectNodes('/f:Package/f:Capabilities/*', $ns) | ForEach-Object { $_.GetAttribute('Name') })
if (($capabilities -join ',') -ne 'runFullTrust') { throw "Unexpected capabilities in the package: $($capabilities -join ', ')" }

$files = "{0} files, {1:n1} MB" -f $staged.Count, ((Get-Item -LiteralPath $package).Length / 1MB)
if (-not $identity.Store) {
    Write-Host "The test package is ready in $layout ($files when packed)."
    Write-Host "It has the development identity $($identity.IdentityName), version $packageVersion. It cannot go to the Store."
    Write-Host 'Nothing was registered. To try it: scripts\register-test-package.ps1'
    return
}
$receipt = [ordered]@{
    applicationVersion = $Version
    packageVersion = $packageVersion
    identityName = $identity.IdentityName
    publisher = $identity.Publisher
    publisherDisplayName = $identity.PublisherDisplayName
    displayName = $identity.DisplayName
    packageFamilyName = $identity.PackageFamilyName
    architecture = 'x64'
    minimumWindowsVersion = $document.SelectSingleNode('//f:TargetDeviceFamily', $ns).GetAttribute('MinVersion')
    capabilities = $capabilities
    signed = $false
    package = $packageName
    packageSha256 = (Get-FileHash -LiteralPath $package -Algorithm SHA256).Hash.ToLowerInvariant()
    executableSha256 = (Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash.ToLowerInvariant()
    commandSha256 = (Get-FileHash -LiteralPath $command -Algorithm SHA256).Hash.ToLowerInvariant()
    nativeSourceArchive = "source\$sourceName.zip"
    nativeSourceSha256 = $sourceSha256
    fileCount = $staged.Count
    verifiedByUnpack = $true
}
$receipt | ConvertTo-Json | Set-Content -LiteralPath "$package.json" -Encoding utf8NoBOM
Write-Host "The Store package is ready: $package ($files)"
Write-Host "Identity $($identity.IdentityName), version $packageVersion, shown as '$($identity.DisplayName)', not signed, SHA-256 $($receipt.packageSha256)"
Write-Host 'Nothing was uploaded. Run the Windows App Certification Kit on it before it goes to Partner Center.'
[pscustomobject]$receipt
