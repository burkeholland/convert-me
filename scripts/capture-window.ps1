#Requires -Version 7.2
# Takes a picture of the Convert Me window and saves it as a PNG.
# Only that one window is copied, straight from the window itself, so nothing else on the
# screen can end up in the picture, even when other windows are on top of it.
# With -Width and -Height the window is first resized (without bringing it to the front).
# Developer tool for the screenshots in docs\screenshots. It is not part of the app.
param(
    [Parameter(Mandatory)][int]$ProcessId,
    [string]$Path,
    [int]$Width,
    [int]$Height
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
if (-not ('ConvertMe.Native' -as [type])) {
    Add-Type -Namespace ConvertMe -Name Native -MemberDefinition @'
[StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
[DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr hwnd, out RECT rect);
[DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hwnd, out RECT rect);
[DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr hwnd, IntPtr hdc, uint flags);
[DllImport("user32.dll")] public static extern bool IsIconic(IntPtr hwnd);
[DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr hwnd);
[DllImport("user32.dll")] public static extern IntPtr SetThreadDpiAwarenessContext(IntPtr value);
[DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr hwnd, IntPtr after, int x, int y, int cx, int cy, uint flags);
'@
}
# Per-monitor awareness makes every size below real pixels, whatever the display scale is.
[void][ConvertMe.Native]::SetThreadDpiAwarenessContext([IntPtr]-4)
$process = Get-Process -Id $ProcessId
$window = $process.MainWindowHandle
if ($window -eq [IntPtr]::Zero) { throw "Process $ProcessId has no window." }
if ($process.MainWindowTitle -ne 'Convert Me') { throw "Process $ProcessId is not Convert Me (window title '$($process.MainWindowTitle)')." }
if ([ConvertMe.Native]::IsIconic($window)) { throw 'The window is minimised.' }
$scale = [ConvertMe.Native]::GetDpiForWindow($window) / 96.0

if ($Width -gt 0 -and $Height -gt 0) {
    # 0x0002 no move, 0x0004 keep the stacking order, 0x0010 do not activate.
    $flags = 0x0002 -bor 0x0004 -bor 0x0010
    if (-not [ConvertMe.Native]::SetWindowPos($window, [IntPtr]::Zero, 0, 0, [int]($Width * $scale), [int]($Height * $scale), $flags)) {
        throw 'The window could not be resized.'
    }
    Start-Sleep -Milliseconds 700
}
if (-not $Path) { Write-Host "Resized to $Width x $Height at scale $scale"; return }

$rect = New-Object ConvertMe.Native+RECT
if (-not [ConvertMe.Native]::GetClientRect($window, [ref]$rect)) { throw 'The window size could not be read.' }
$pixelWidth, $pixelHeight = ($rect.Right - $rect.Left), ($rect.Bottom - $rect.Top)
if ($pixelWidth -le 0 -or $pixelHeight -le 0) { throw 'The window has no size.' }
# A window without a frame keeps one spare row of pixels below its visible edge, which is
# how Wails avoids flicker while resizing. That row is not on screen, so it is cut off.
$outer = New-Object ConvertMe.Native+RECT
[void][ConvertMe.Native]::GetWindowRect($window, [ref]$outer)
$visibleWidth = [Math]::Min($pixelWidth, $outer.Right - $outer.Left)
$visibleHeight = [Math]::Min($pixelHeight, $outer.Bottom - $outer.Top)
New-Item -ItemType Directory -Force (Split-Path $Path -Parent) | Out-Null
$bitmap = [Drawing.Bitmap]::new($pixelWidth, $pixelHeight, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
$graphics = [Drawing.Graphics]::FromImage($bitmap)
try {
    $hdc = $graphics.GetHdc()
    # 1 = the inside of the window only, 2 = include what the graphics card draws.
    try { $copied = [ConvertMe.Native]::PrintWindow($window, $hdc, 3) } finally { $graphics.ReleaseHdc($hdc) }
    if (-not $copied) { throw 'Windows could not copy the window.' }
    $visible = $bitmap.Clone([Drawing.Rectangle]::new(0, 0, $visibleWidth, $visibleHeight), [Drawing.Imaging.PixelFormat]::Format24bppRgb)
    try { $visible.Save($Path, [Drawing.Imaging.ImageFormat]::Png) } finally { $visible.Dispose() }
} finally { $graphics.Dispose(); $bitmap.Dispose() }
Write-Host ("Captured {0}x{1} pixels ({2}x{3} at {4:p0} scale) to {5}" -f $visibleWidth, $visibleHeight,
    [int]($visibleWidth / $scale), [int]($visibleHeight / $scale), $scale, $Path)
