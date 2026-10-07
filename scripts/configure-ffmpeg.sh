#!/usr/bin/env bash
set -euo pipefail

# Run from the extracted, unmodified FFmpeg 8.1.3 source directory in Git Bash.
# Native MinGW-w64 GCC, mingw32-make and nasm must be on PATH.
# build-native.ps1 builds the pinned zlib, libwebp and LAME into ../prefix first.
#
# Everything is off unless it is listed here. The lists are the exact set of formats
# Convert Me can read and write, so keep them in step with internal/convert/formats.go.
# Paths are relative on purpose: the build works from a folder with spaces in its name.

# Response files are off because Git Bash, started by the native mingw32-make, cuts a command
# line at about 8000 characters. That silently broke the step that lists libavcodec's objects.
# Without response files make starts the archiver directly, which has no such limit.
export TMPDIR="$PWD/.configure-work"
mkdir -p "$TMPDIR"
export CONVERTME_PREFIX=../prefix

containers="mov,matroska,avi,asf,mpegps,mpegts,ogg"
audio_files="mp3,wav,aac,flac,aiff"
# Image readers probe by content. The pattern-matching image2 demuxer is left out, so a
# file name such as "100%d.png" can never be read as a numbered sequence.
image_files="gif,image_png_pipe,image_jpeg_pipe,image_bmp_pipe,image_tiff_pipe,image_webp_pipe"
# MPEG program and transport streams do not always say what they carry. The engine finds out
# by trying these raw stream readers. Without them MPEG-2 video is mistaken for MP3 sound.
stream_probes="mpegvideo,h264,m4v,ac3,eac3,dts,truehd,loas"

# No HEVC (H.265) here, on purpose: no decoder, no parser, no stream reader. The engine still
# tells the app that a file holds HEVC, because the container says so, and the app then
# says that it cannot read it. HEIC photos are read by the app through the codecs that are
# part of Windows (internal/convert/sysimage_windows.go), not by this engine.
video_decoders="h264,vp8,vp9,mpeg4,msmpeg4v1,msmpeg4v2,msmpeg4v3,mpeg1video,mpeg2video"
video_decoders="$video_decoders,wmv1,wmv2,wmv3,vc1,mjpeg,prores,dnxhd,theora,flv,h263,dvvideo,rawvideo,qtrle"
image_decoders="png,gif,bmp,tiff,webp"
audio_decoders="aac,aac_latm,ac3,eac3,mp3,mp3float,mp2,mp2float,flac,vorbis,opus,alac"
audio_decoders="$audio_decoders,wmav1,wmav2,wmapro,wmalossless"
audio_decoders="$audio_decoders,pcm_s16le,pcm_s16be,pcm_s24le,pcm_s24be,pcm_s32le,pcm_s32be"
audio_decoders="$audio_decoders,pcm_f32le,pcm_f32be,pcm_f64le,pcm_f64be,pcm_u8,pcm_s8,pcm_alaw,pcm_mulaw,pcm_bluray,pcm_dvd"
audio_decoders="$audio_decoders,adpcm_ima_wav,adpcm_ms,adpcm_ima_qt,amrnb,amrwb,dca,truehd,mlp"

parsers="h264,vp8,vp9,vp3,mpeg4video,mpegvideo,vc1,mjpeg,h263,png,gif,bmp,webp"
parsers="$parsers,aac,aac_latm,ac3,mpegaudio,flac,vorbis,opus,dca,mlp"

# H.264 comes from the encoder that ships with Windows (Media Foundation). No H.264 encoder is built here.
# FFmpeg 8.1's Media Foundation wrapper does not compile without the Direct3D 11 types, so d3d11va is
# switched on as well. It adds no decoder and no dependency: the system DLLs are loaded only on demand.
encoders="mjpeg,png,bmp,tiff,gif,libwebp,h264_mf,aac,libmp3lame,flac,pcm_s16le,pcm_s24le"
muxers="image2,image2pipe,webp,gif,mp4,mov,matroska,ipod,mp3,wav,flac,null"
bitstream_filters="aac_adtstoasc,h264_mp4toannexb,vp9_superframe,vp9_superframe_split,extract_extradata"

video_filters="scale,format,fps,setsar,pad,crop,transpose,hflip,vflip,rotate,trim,setpts,split,overlay,lutrgb,color"
video_filters="$video_filters,palettegen,paletteuse,bwdif,xstack,null,testsrc2"
audio_filters="aresample,aformat,anull,atrim,asetpts,sine,anullsrc"

./configure \
  --target-os=mingw32 --arch=x86_64 \
  --disable-autodetect --disable-network --disable-gpl --disable-nonfree \
  --disable-version3 --disable-shared --enable-static \
  --disable-doc --disable-debug --disable-response-files \
  --disable-pthreads --enable-w32threads \
  --disable-everything --disable-avdevice \
  --enable-ffmpeg --enable-ffprobe \
  --enable-zlib --enable-libwebp --enable-libmp3lame --enable-mediafoundation --enable-d3d11va \
  --pkg-config=../../../scripts/pkg-config-shim.sh \
  --enable-protocol=file,pipe \
  --enable-demuxer="$containers,$audio_files,$image_files,$stream_probes" \
  --enable-decoder="$video_decoders,$image_decoders,$audio_decoders" \
  --enable-parser="$parsers" \
  --enable-encoder="$encoders" \
  --enable-muxer="$muxers" \
  --enable-bsf="$bitstream_filters" \
  --enable-filter="$video_filters,$audio_filters" \
  --extra-cflags="-I$CONVERTME_PREFIX/include" \
  --extra-ldflags="-static -L$CONVERTME_PREFIX/lib"
