#Requires -Version 7.2
# Draws the Convert Me icon: a square turning into a circle, white on a teal tile.
# It is the same mark as the logo in the title bar (frontend\src\icons.ts).
param()
. (Join-Path $PSScriptRoot 'common.ps1')
Add-Type -AssemblyName System.Drawing
$root = Split-Path $PSScriptRoot -Parent
New-Item -ItemType Directory -Force (Join-Path $root 'build\windows') | Out-Null

function New-RoundedRectangle([single]$X, [single]$Y, [single]$Width, [single]$Height, [single]$Radius) {
    $path = [Drawing.Drawing2D.GraphicsPath]::new()
    $d = 2 * $Radius
    $path.AddArc($X, $Y, $d, $d, 180, 90)
    $path.AddArc($X + $Width - $d, $Y, $d, $d, 270, 90)
    $path.AddArc($X + $Width - $d, $Y + $Height - $d, $d, $d, 0, 90)
    $path.AddArc($X, $Y + $Height - $d, $d, $d, 90, 90)
    $path.CloseFigure()
    $path
}

$teal = [Drawing.Color]::FromArgb(255, 15, 118, 110)
$white = [Drawing.Color]::FromArgb(255, 255, 255, 255)
$bitmap = [Drawing.Bitmap]::new(1024, 1024, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
$graphics = [Drawing.Graphics]::FromImage($bitmap)
$tileBrush = [Drawing.SolidBrush]::new($teal)
$markBrush = [Drawing.SolidBrush]::new($white)
$markPen = [Drawing.Pen]::new($white, 57)
$tile = New-RoundedRectangle 32 32 960 960 214
$square = New-RoundedRectangle 247 449 321 321 75
try {
    $graphics.SmoothingMode = [Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $graphics.PixelOffsetMode = [Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $graphics.Clear([Drawing.Color]::Transparent)
    $graphics.FillPath($tileBrush, $tile)
    $markPen.LineJoin = [Drawing.Drawing2D.LineJoin]::Round
    $graphics.DrawPath($markPen, $square)
    # A ring in the tile colour separates the circle from the square it grows out of.
    $graphics.FillEllipse($tileBrush, [single](638.5 - 203), [single](392 - 203), [single]406, [single]406)
    $graphics.FillEllipse($markBrush, [single](638.5 - 167), [single](392 - 167), [single]334, [single]334)
    $bitmap.Save((Join-Path $root 'build\appicon.png'), [Drawing.Imaging.ImageFormat]::Png)

    $sizes = @(16, 24, 32, 48, 64, 128, 256)
    $images = [Collections.Generic.List[byte[]]]::new()
    foreach ($size in $sizes) {
        $small = [Drawing.Bitmap]::new($size, $size, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
        $canvas = [Drawing.Graphics]::FromImage($small)
        $memory = [IO.MemoryStream]::new()
        try {
            $canvas.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
            $canvas.PixelOffsetMode = [Drawing.Drawing2D.PixelOffsetMode]::HighQuality
            $canvas.DrawImage($bitmap, 0, 0, $size, $size)
            $small.Save($memory, [Drawing.Imaging.ImageFormat]::Png)
            $images.Add($memory.ToArray())
        } finally { $memory.Dispose(); $canvas.Dispose(); $small.Dispose() }
    }
    $writer = [IO.BinaryWriter]::new([IO.File]::Create((Join-Path $root 'build\windows\icon.ico')))
    try {
        $writer.Write([uint16]0); $writer.Write([uint16]1); $writer.Write([uint16]$sizes.Count)
        $offset = 6 + 16 * $sizes.Count
        for ($i = 0; $i -lt $sizes.Count; $i++) {
            $dimension = if ($sizes[$i] -eq 256) { 0 } else { $sizes[$i] }
            $writer.Write([byte]$dimension); $writer.Write([byte]$dimension)
            $writer.Write([byte]0); $writer.Write([byte]0)
            $writer.Write([uint16]1); $writer.Write([uint16]32)
            $writer.Write([uint32]$images[$i].Length); $writer.Write([uint32]$offset)
            $offset += $images[$i].Length
        }
        foreach ($image in $images) { $writer.Write($image) }
    } finally { $writer.Dispose() }
} finally {
    $graphics.Dispose(); $bitmap.Dispose(); $tileBrush.Dispose(); $markBrush.Dispose(); $markPen.Dispose()
    $tile.Dispose(); $square.Dispose()
}
Write-Host 'Generated the app icon: build\appicon.png and build\windows\icon.ico'
