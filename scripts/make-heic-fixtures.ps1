#Requires -Version 7.2
# Makes the two HEIC pictures the project needs, with the HEIF encoder of Windows:
#   internal\convert\heic-check.heic   64 x 48, part of the app. At startup the app reads it
#                                      to find out whether this PC can read HEIC photos.
#   scripts\fixtures\photo.heic        320 x 240, read by the conversion tests.
# Both are made from test pictures in scripts\fixtures, so they hold nothing personal.
#
# This needs the HEIF Image Extensions and the HEVC Video Extensions from Microsoft on this
# PC. The script installs nothing. The two files are kept in the repository, so a normal
# build never runs this script.
. (Join-Path $PSScriptRoot 'common.ps1')
Add-Type -AssemblyName PresentationCore, WindowsBase
$root = Split-Path $PSScriptRoot -Parent
$heif = [guid]'e1e62521-6787-405b-a339-500715b5763f'   # GUID_ContainerFormatHeif

function Save-Heic([string]$Source, [string]$Target, [int]$MaxSide) {
    $in = [IO.File]::OpenRead($Source)
    try {
        $frame = [Windows.Media.Imaging.BitmapDecoder]::Create($in, 'PreservePixelFormat', 'OnLoad').Frames[0]
        $scale = [Math]::Min(1.0, $MaxSide / [Math]::Max($frame.PixelWidth, $frame.PixelHeight))
        $picture = if ($scale -lt 1) {
            [Windows.Media.Imaging.TransformedBitmap]::new($frame, [Windows.Media.ScaleTransform]::new($scale, $scale))
        } else { $frame }
        $picture = [Windows.Media.Imaging.FormatConvertedBitmap]::new($picture, [Windows.Media.PixelFormats]::Bgr24, $null, 0)
        try { $encoder = [Windows.Media.Imaging.BitmapEncoder]::Create($heif) }
        catch { throw 'Windows has no HEIC encoder on this PC. It comes with the HEIF Image Extensions and the HEVC Video Extensions from Microsoft.' }
        $encoder.Frames.Add([Windows.Media.Imaging.BitmapFrame]::Create($picture))
        $out = [IO.File]::Create($Target)
        try { $encoder.Save($out) } finally { $out.Dispose() }
    } finally { $in.Dispose() }
    $bytes = [IO.File]::ReadAllBytes($Target)
    if ($bytes.Length -lt 12 -or [Text.Encoding]::ASCII.GetString($bytes, 4, 8) -ne 'ftypheic') { throw "$Target is not a HEIC file." }
    Write-Host ("{0}: {1} x {2}, {3} bytes" -f (Split-Path $Target -Leaf), $picture.PixelWidth, $picture.PixelHeight, $bytes.Length)
}

Save-Heic (Join-Path $root 'scripts\fixtures\picture-rgb.png') (Join-Path $root 'internal\convert\heic-check.heic') 64
Save-Heic (Join-Path $root 'scripts\fixtures\photo.jpg') (Join-Path $root 'scripts\fixtures\photo.heic') 4000
