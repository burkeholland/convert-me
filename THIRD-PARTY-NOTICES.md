# Third-party software

Convert Me's own code is MIT licensed. The parts below keep their own licenses. The full
license texts are in the `notices` folder of the source, and in `licenses\notices` next
to the app. In the app, **About** and then **Open licenses folder** takes you there.

| Component | Version | License | How it is used |
| --- | --- | --- | --- |
| FFmpeg (`ffmpeg.exe`, `ffprobe.exe`) | 8.1.3 | LGPL 2.1 or later | The conversion engine. Two separate programs that the app starts. |
| LAME | 3.100 | LGPL 2.0 or later | MP3 encoding, linked into the engine |
| libwebp | 1.6.0 | BSD 3-Clause, with a patent grant | WebP encoding, linked into the engine |
| zlib | 1.3.2 | zlib | Compression for PNG and others, linked into the engine |
| MinGW-w64 runtime | 11.0.1 | Permissive and public domain parts | Linked into the engine by the compiler |
| GCC runtime | 13.2.0 | GPL 3 with the GCC Runtime Library Exception 3.1 | Linked into the engine by the compiler |
| Go standard library and runtime | See `BUILD-INFO.txt` in the app folder | BSD 3-Clause | Part of `ConvertMe.exe` |
| Wails and other Go modules | Listed in `notices\GO-MODULES.txt` | Each under its own license, copied to `notices\go-modules` | Part of `ConvertMe.exe` |
| Microsoft C and C++ runtime libraries | From the Microsoft C++ build tools and the Windows SDK used for the build | Microsoft's license terms for those tools, which allow passing this code on inside a program | Linked into `ConvertMeCommand.dll`, the File Explorer command. Only the packaged version of the app has that file. |

Not included, but used:

- **H.264 video** is encoded by the encoder that is part of Windows (Media Foundation).
  Convert Me contains no H.264 encoder.
- **HEIC photos** are read by codecs that belong to Windows: Microsoft's HEIF Image
  Extensions and HEVC Video Extensions, when the PC has them. Convert Me reaches them
  through the Windows Imaging Component. Convert Me contains no HEVC (H.265) decoder and
  no HEVC encoder, neither in the app nor in the engine, and the engine is built without
  the HEVC parser and stream reader as well. Microsoft's terms apply to those codecs.
- **Microsoft Edge WebView2** draws the interface. It is part of Windows and is not in
  the app folder. The app contains Microsoft's small installer for it, which is only
  used when WebView2 is missing. Microsoft's terms apply to that installer and to
  WebView2 itself.

## The engine, its source, and your rights

The engine is **not** a downloaded FFmpeg build. The scripts in this repository build it
from the official, unmodified source archives, with a short list of formats:

- LGPL only. No GPL parts, no "nonfree" parts, no "version 3" parts.
- No network protocols. It can read and write local files and nothing else.
- Everything is linked statically. No DLL is copied from anywhere.
- The exact configuration is in `scripts\configure-ffmpeg.sh`.

The build checks all of this by itself (`scripts\verify-runtime.ps1`) and stops if any
of it is not true.

Every build makes a companion archive, `ConvertMe-0.2.0-native-source.zip`. It contains
the four exact source archives (FFmpeg, LAME, libwebp, zlib), the build scripts, the
hashes of everything that went in, and a receipt with the compiler versions and the
hashes of the binaries that came out. That is the complete corresponding source of the
engine. **Whoever passes the app on must pass that archive on with it**, with the same
ease of access, and must keep `SHA256SUMS.txt` and these notices with it.

The packaged version of the app (the one from the Microsoft Store) carries that archive
inside the package, in its `source` folder. `BUILD-INFO.txt` in the package gives its
name and its hash. To get there, choose **About** and then **Open licenses folder** in
the app, and go one folder up.

You may modify, rebuild, replace and redistribute the engine under its license.
Nothing in the license of Convert Me restricts that, including reverse engineering for
the purpose of debugging your changes. The app checks the two engine files against
hashes that are compiled into it, so to use a changed engine, build the app from source
after running `scripts\prepare-runtime.ps1`. That script writes the new hashes.

The engine talks to the app only through command-line arguments, files, and its
standard output. It is not linked into `ConvertMe.exe`.

## Patents

Some audio and video formats are covered by patents in some countries. Convert Me is
free software from an individual, provided as it is. If you distribute it, or use it
commercially, it is up to you to check whether you need a license for the formats you
use. The H.264 encoder is the one licensed with Windows. HEIC photos are decoded by the
codecs that are installed in Windows, and the engine has no HEVC (H.265) code. This
paragraph describes what is in the app. It is not legal advice, and it does not say
that any format is free of patents.

## Upstream projects

- https://ffmpeg.org/
- https://lame.sourceforge.io/
- https://developers.google.com/speed/webp
- https://zlib.net/
- https://www.mingw-w64.org/
- https://gcc.gnu.org/onlinedocs/libstdc++/manual/license.html
- https://go.dev/
- https://wails.io/

No affiliation with, or endorsement by, any of these projects is implied.
