# ConvertMe

ConvertMe is a Windows 11 image converter designed around File Explorer. Select one or more supported images, open **Show more options > Convert image**, and choose an output format or **Custom...**.

Choosing a format opens a small progress window that converts immediately and closes itself when done (it stays open if anything fails). **Custom...** opens the full conversion window so you can choose format and quality settings.

## Explorer integration

The **Convert image** right-click menu is optional. ConvertMe registers it on first run (per-user, no administrator prompt). Turn it off or on at any time from **Settings > App > Explorer context menu**. The setting is saved, so the menu is not re-added on the next launch once you turn it off.

## Supported formats

PNG, JPG/JPEG, BMP, TIFF, WebP, and HEIC/HEIF are supported. GIF is intentionally excluded from the first release. Animated WebP files are converted using their first frame.

Converted files are written next to the originals. Existing names are never overwritten; ConvertMe adds a numbered suffix such as `photo (1).jpg`.

## HEIC requirements

HEIC conversion uses the Windows HEIF Image Extensions codec. If it is missing, ConvertMe shows an install action that opens the Microsoft Store page for the codec.

## Development

Install Go, Node.js, and the Wails CLI, then run:

```powershell
wails dev
```

The frontend uses Microsoft's Fluent Web Components inside the Wails WebView, with a native Mica window and Windows 11 light/dark theming.

## Build

Build the Windows x64 binary:

```powershell
wails build -platform windows/amd64 -installscope user
```

Run the built app:

```powershell
.\build\bin\convert-me.exe
```

The app registers the per-user Explorer menu when it starts. No administrator prompt is required.

Build a per-user NSIS installer:

```powershell
wails build -platform windows/amd64 -installscope user --nsis
```
