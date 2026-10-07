"""Regenerates the small test inputs in scripts/fixtures.

Developer tool only. It is not part of the build and not part of the app.
Needs Python 3 with Pillow, and a full FFmpeg on PATH (any recent build with libx264,
libx265, libvpx, libopus, libvorbis, libmp3lame and an AV1 encoder). That FFmpeg is only
used here to make inputs in formats the app's own engine can read but not write.
Everything is synthetic: colour bars, gradients and a sine tone. No real recordings.

    python scripts/make-fixtures.py
"""

import math
import pathlib
import shutil
import struct
import subprocess
import sys

from PIL import Image, ImageDraw

ROOT = pathlib.Path(__file__).resolve().parent
OUT = ROOT / "fixtures"
FFMPEG = shutil.which("ffmpeg")


def run(*args: str) -> None:
    command = [FFMPEG, "-hide_banner", "-loglevel", "error", "-nostdin", "-y", *args]
    result = subprocess.run(command, capture_output=True, text=True)
    if result.returncode != 0:
        raise SystemExit(f"ffmpeg failed: {' '.join(args)}\n{result.stderr}")


def picture(width: int = 320, height: int = 240) -> Image.Image:
    """A photo-like test picture: smooth gradients, hard edges and a marker in one corner."""
    image = Image.new("RGB", (width, height))
    pixels = image.load()
    for y in range(height):
        for x in range(width):
            pixels[x, y] = (
                int(255 * x / (width - 1)),
                int(255 * y / (height - 1)),
                int(127 + 127 * math.sin((x + y) / 23.0)),
            )
    draw = ImageDraw.Draw(image)
    # A red block in the top-left corner makes rotation mistakes obvious in tests.
    draw.rectangle([0, 0, width // 8, height // 8], fill=(220, 20, 20))
    draw.ellipse([width // 3, height // 3, 2 * width // 3, 2 * height // 3], outline=(255, 255, 255), width=3)
    return image


def with_alpha(image: Image.Image) -> Image.Image:
    """Left half fully transparent, right half opaque. The hidden colour is pure black."""
    rgba = image.convert("RGBA")
    pixels = rgba.load()
    for y in range(rgba.height):
        for x in range(rgba.width // 2):
            pixels[x, y] = (0, 0, 0, 0)
    return rgba


def images() -> None:
    base = picture()
    base.save(OUT / "photo.jpg", quality=90)
    base.save(OUT / "photo-progressive.jpg", quality=85, progressive=True)
    base.convert("L").save(OUT / "photo-gray.jpg", quality=90)
    base.convert("CMYK").save(OUT / "photo-cmyk.jpg", quality=90)
    # EXIF orientation 6: the stored pixels are landscape, the photo is meant to be shown
    # rotated 90 degrees clockwise, so it displays as 240 x 320.
    exif = Image.Exif()
    exif[0x0112] = 6
    base.save(OUT / "photo-exif-rotated.jpg", quality=90, exif=exif.tobytes())

    base.save(OUT / "picture-rgb.png")
    with_alpha(base).save(OUT / "picture-alpha.png")
    palette = with_alpha(base).quantize(colors=64, method=Image.Quantize.FASTOCTREE)
    palette.save(OUT / "picture-palette.png")
    gray16 = Image.new("I;16", (320, 240))
    gray16.putdata([int(65535 * (x / 319)) for _ in range(240) for x in range(320)])
    gray16.save(OUT / "picture-gray16.png")

    base.save(OUT / "picture-lossy.webp", quality=80)
    base.save(OUT / "picture-lossless.webp", lossless=True)
    with_alpha(base).save(OUT / "picture-alpha.webp", quality=80)
    frames = [picture(160, 120).rotate(angle) for angle in (0, 90, 180, 270)]
    frames[0].save(OUT / "animated.webp", save_all=True, append_images=frames[1:], duration=200, loop=0)

    base.quantize(colors=128).save(OUT / "picture.gif")
    frames[0].save(OUT / "animated.gif", save_all=True, append_images=frames[1:], duration=200, loop=0)

    # Uncompressed formats get a smaller picture so the fixtures stay small.
    small = picture(160, 120)
    small.save(OUT / "picture.bmp")
    with_alpha(small).save(OUT / "picture-alpha.bmp")
    small.save(OUT / "picture-lzw.tiff", compression="tiff_lzw")
    small.save(OUT / "picture-deflate.tiff", compression="tiff_adobe_deflate")
    small.save(OUT / "picture-plain.tif")
    with_alpha(small).save(OUT / "picture-alpha.tiff", compression="tiff_lzw")
    # A tiny picture and an odd-sized one catch rounding and even-size assumptions.
    picture(33, 17).save(OUT / "picture-odd-size.png")


def tone(seconds: float = 1.0, rate: int = 44100, channels: int = 2) -> str:
    layout = {1: "mono", 2: "stereo", 6: "5.1"}[channels]
    return f"sine=frequency=440:sample_rate={rate}:duration={seconds},aformat=channel_layouts={layout}"


def audio() -> None:
    def make(name: str, *codec: str, channels: int = 2, rate: int = 44100, seconds: float = 1.0) -> None:
        run("-f", "lavfi", "-i", tone(seconds=seconds, channels=channels, rate=rate), *codec, str(OUT / name))

    make("tone.mp3", "-c:a", "libmp3lame", "-b:a", "128k")
    make("tone-16bit.wav", "-c:a", "pcm_s16le")
    make("tone-24bit.wav", "-c:a", "pcm_s24le", rate=48000)
    make("tone-float.wav", "-c:a", "pcm_f32le", rate=48000, seconds=0.6)
    make("tone-mono.wav", "-c:a", "pcm_s16le", channels=1, rate=22050)
    make("tone-surround.wav", "-c:a", "pcm_s16le", channels=6, rate=48000, seconds=0.6)
    make("tone-16bit.flac", "-c:a", "flac")
    make("tone-24bit.flac", "-c:a", "flac", "-sample_fmt", "s32", "-bits_per_raw_sample", "24", rate=96000)
    make("tone.m4a", "-c:a", "aac", "-b:a", "128k")
    make("tone.aac", "-c:a", "aac", "-b:a", "128k", "-f", "adts")
    make("tone.ogg", "-c:a", "libvorbis", "-q:a", "4")
    make("tone.opus", "-c:a", "libopus", "-b:a", "96k", rate=48000)
    make("tone.wma", "-c:a", "wmav2", "-b:a", "128k")
    make("tone.aiff", "-c:a", "pcm_s16be")
    # An MP3 with album art: the picture is a video stream that must not turn it into a video.
    run("-f", "lavfi", "-i", tone(), "-i", str(OUT / "photo.jpg"), "-map", "0:a", "-map", "1:v",
        "-c:a", "libmp3lame", "-b:a", "128k", "-c:v", "copy", "-id3v2_version", "3",
        "-disposition:v", "attached_pic", "-metadata", "title=Fixture tone", "-metadata", "artist=Convert Me tests",
        str(OUT / "tone-cover.mp3"))


def video() -> None:
    def source(size: str = "320x240", rate: int = 30, seconds: int = 2) -> list[str]:
        return ["-f", "lavfi", "-i", f"testsrc2=size={size}:rate={rate}:duration={seconds}",
                "-f", "lavfi", "-i", tone(seconds=seconds, rate=48000)]

    x264 = ["-c:v", "libx264", "-preset", "veryfast", "-crf", "30", "-pix_fmt", "yuv420p"]
    aac = ["-c:a", "aac", "-b:a", "96k"]
    run(*source(), *x264, *aac, str(OUT / "h264-aac.mp4"))
    run(*source(), *x264, *aac, str(OUT / "h264-aac.mov"))
    run(*source(), *x264, *aac, str(OUT / "h264-aac.mkv"))
    run(*source(), *x264, "-c:a", "ac3", "-b:a", "128k", str(OUT / "h264-ac3.mkv"))
    run(*source(), *x264, *aac, "-f", "mpegts", str(OUT / "h264-aac.ts"))
    run("-f", "lavfi", "-i", "testsrc2=size=320x240:rate=30:duration=2", *x264, "-an", str(OUT / "h264-silent.mp4"))
    # Phone-style rotation: the stored picture is landscape with a "rotate 90" note.
    run("-display_rotation", "270", "-i", str(OUT / "h264-aac.mp4"), "-c", "copy", str(OUT / "h264-rotated.mp4"))
    # testsrc2 only makes even sizes, so an odd one is cut out of it. 4:4:4 colour allows odd sizes.
    run("-f", "lavfi", "-i", "testsrc2=size=320x240:rate=30:duration=2,format=yuv444p,crop=319:239:0:0:exact=1",
        "-f", "lavfi", "-i", tone(seconds=2, rate=48000),
        "-c:v", "libx264", "-preset", "veryfast", "-crf", "30", "-pix_fmt", "yuv444p",
        *aac, str(OUT / "h264-odd-size.mkv"))
    # Smaller than the Windows H.264 encoder accepts. Typical for small animated GIFs.
    tiny = [picture(24, 20).rotate(angle) for angle in (0, 180)]
    tiny[0].save(OUT / "animated-tiny.gif", save_all=True, append_images=tiny[1:], duration=300, loop=0)
    # HEVC is what most phones record. The engine has no HEVC decoder, so this one must be refused.
    run(*source(), "-c:v", "libx265", "-preset", "veryfast", "-crf", "32", "-pix_fmt", "yuv420p", "-tag:v", "hvc1",
        "-x265-params", "log-level=error", *aac, str(OUT / "hevc-aac.mov"))
    # HDR: 10 bit with the HLG curve, in a codec the engine reads.
    run(*source(), "-vf", "format=yuv420p10le,setparams=color_primaries=bt2020:color_trc=arib-std-b67:colorspace=bt2020nc",
        "-c:v", "libvpx-vp9", "-b:v", "200k", "-deadline", "realtime", "-cpu-used", "8",
        "-color_primaries", "bt2020", "-color_trc", "arib-std-b67", "-colorspace", "bt2020nc",
        "-c:a", "libopus", "-b:a", "64k", str(OUT / "vp9-hdr.mkv"))
    run(*source(), "-c:v", "libvpx-vp9", "-b:v", "200k", "-deadline", "realtime", "-cpu-used", "8",
        "-c:a", "libopus", "-b:a", "64k", str(OUT / "vp9-opus.webm"))
    run(*source(), "-c:v", "libvpx", "-b:v", "200k", "-deadline", "realtime", "-cpu-used", "8",
        "-c:a", "libvorbis", "-q:a", "3", str(OUT / "vp8-vorbis.webm"))
    run(*source(), "-c:v", "mpeg4", "-q:v", "8", "-c:a", "libmp3lame", "-b:a", "96k", str(OUT / "mpeg4-mp3.avi"))
    run(*source(), "-c:v", "wmv2", "-q:v", "8", "-c:a", "wmav2", "-b:a", "96k", str(OUT / "wmv2-wma.wmv"))
    run(*source(), "-c:v", "mpeg2video", "-q:v", "8", "-c:a", "mp2", "-b:a", "128k", "-f", "vob",
        str(OUT / "mpeg2-mp2.mpg"))
    run(*source(size="352x288", rate=25), "-vf", "setfield=tff", "-c:v", "mpeg2video", "-q:v", "8", "-flags", "+ilme+ildct",
        "-c:a", "mp2", "-b:a", "128k", "-f", "vob", str(OUT / "mpeg2-interlaced.mpg"))
    run(*source(), "-c:v", "prores_ks", "-profile:v", "0", "-c:a", "pcm_s16le", str(OUT / "prores-pcm.mov"))
    run(*source(), "-c:v", "libsvtav1", "-preset", "12", "-crf", "50", "-svtav1-params", "lp=2",
        "-c:a", "libopus", "-b:a", "64k", str(OUT / "av1-opus.mp4"))


def broken() -> None:
    """Files that must fail cleanly."""
    data = (OUT / "h264-aac.mp4").read_bytes()
    (OUT / "truncated.mp4").write_bytes(data[: len(data) // 5])
    (OUT / "not-really.png").write_bytes(b"This is plain text with a picture name.\n" * 20)
    (OUT / "garbage.mp3").write_bytes(struct.pack("<64I", *range(64)) * 8)


def main() -> None:
    if not FFMPEG:
        sys.exit("A full FFmpeg must be on PATH to regenerate the fixtures.")
    OUT.mkdir(exist_ok=True)
    for old in OUT.iterdir():
        if old.name != "README.txt":
            old.unlink()
    images()
    audio()
    video()
    broken()
    files = sorted(path for path in OUT.iterdir() if path.name != "README.txt")
    total = sum(path.stat().st_size for path in files)
    print(f"{len(files)} fixtures, {total / 1024:.0f} KB in {OUT}")


if __name__ == "__main__":
    main()
