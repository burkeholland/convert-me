#!/bin/sh
# Minimal stand-in for pkg-config, used only while configuring FFmpeg for Convert Me.
#
# FFmpeg's configure insists on pkg-config to find libwebp. This script answers for the
# libraries that build-native.ps1 compiles into its private prefix and for nothing else,
# so a library from the wider toolchain can never be picked up by accident.
# No pkg-config installation is needed or used.
prefix="${CONVERTME_PREFIX:?CONVERTME_PREFIX must point at the private build prefix}"

action=""
skip=0
packages=""
# Word splitting is intended: configure passes "name >= version" as one or as three words.
for arg in $*; do
    if [ "$skip" = 1 ]; then
        skip=0
        continue
    fi
    case "$arg" in
        --version) echo "0.29.2"; exit 0 ;;
        --exists|--cflags|--libs) action="${arg#--}" ;;
        --variable=includedir) action="includedir" ;;
        --variable=*) exit 1 ;;
        ">="|">"|"="|"<="|"<") skip=1 ;;
        -*) ;;
        *) packages="$packages $arg" ;;
    esac
done

[ -n "$action" ] && [ -n "$packages" ] || exit 1

out=""
for package in $packages; do
    case "$package" in
        zlib)        archive="libz.a";        libs="-lz" ;;
        libsharpyuv) archive="libsharpyuv.a"; libs="-lsharpyuv" ;;
        libwebp)     archive="libwebp.a";     libs="-lwebp -lsharpyuv" ;;
        libwebpmux)  archive="libwebpmux.a";  libs="-lwebpmux -lwebp -lsharpyuv" ;;
        *)
            echo "pkg-config shim: package '$package' is not part of this build" >&2
            exit 1
            ;;
    esac
    if [ ! -f "$prefix/lib/$archive" ]; then
        echo "pkg-config shim: $prefix/lib/$archive is missing" >&2
        exit 1
    fi
    case "$action" in
        exists) ;;
        cflags) out="$out -I$prefix/include" ;;
        libs) out="$out -L$prefix/lib $libs" ;;
        includedir) out="$prefix/include" ;;
    esac
done

[ "$action" = exists ] || echo "$out"
exit 0
