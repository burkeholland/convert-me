#Requires -Version 7.2
# Registers the test package of Convert Me for the current user, or removes it again.
#
# This is the only script here that changes Windows. While the package is registered:
#   - File Explorer shows "Convert with Convert Me" when you right-click a picture, an
#     audio file or a video.
#   - The Start menu lists "Convert Me Development".
# It needs no administrator rights, no certificate and no Store. It does need Developer
# Mode, and it does not turn that on. Nothing is copied: Windows uses the folder
# build\msix\development\layout in place, so that folder has to stay where it is.
# The Store package is never registered by this script. It has its own identity, and
# Windows installs it from the Store.
#
#   scripts\register-test-package.ps1            register (again)
#   scripts\register-test-package.ps1 -Remove    remove
[CmdletBinding()]
param([switch]$Remove)
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$layout = Join-Path $root 'build\msix\development\layout'
$name = (Get-MsixIdentity).IdentityName

function Remove-TestPackage {
    foreach ($package in @(Get-AppxPackage -Name $name)) {
        Remove-AppxPackage -Package $package.PackageFullName
    }
    if (@(Get-AppxPackage -Name $name).Count) { throw "The package $name could not be removed." }
}

if ($Remove) {
    if (-not @(Get-AppxPackage -Name $name).Count) {
        Write-Host "The test package ($name) is not registered. Nothing to remove."
        return
    }
    Remove-TestPackage
    Write-Host "Removed the test package ($name). The right-click command and the Start menu entry are gone."
    return
}

$developerMode = (Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock' -ErrorAction SilentlyContinue).AllowDevelopmentWithoutDevLicense
if ($developerMode -ne 1) {
    throw 'Developer Mode is off. Windows only registers a package from a folder when it is on (Settings > System > For developers). This script does not change that setting.'
}
$manifest = Join-Path $layout 'AppxManifest.xml'
if (-not (Test-Path -LiteralPath $manifest)) { throw 'The test package has not been built. Run scripts\package-msix.ps1 first.' }

Remove-TestPackage
Add-AppxPackage -Register $manifest
$package = @(Get-AppxPackage -Name $name) | Select-Object -First 1
if (-not $package) { throw "Windows did not register the package $name." }

# Ask the registered command for its title, the way File Explorer does.
$probe = Join-Path $root 'build\shellext\x64\probe.exe'
if (Test-Path -LiteralPath $probe) {
    $described = @(& $probe describe --registered)
    if ($LASTEXITCODE -ne 0 -or $described[0] -ne 'title=0x00000000 Convert with Convert Me') {
        throw "The package is registered, but its command does not answer: $($described -join '; ')"
    }
}
Write-Host "Registered $($package.Name) $($package.Version) for $env:USERNAME, from $($package.InstallLocation)"
Write-Host 'Right-click a picture, an audio file or a video in File Explorer: "Convert with Convert Me" is in the menu.'
Write-Host 'To remove it again: scripts\register-test-package.ps1 -Remove'
