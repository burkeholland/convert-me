package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/deepteams/webp"
	"github.com/gen2brain/heic"
	"golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/tiff"
)

var (
	ErrUnsupportedInput      = errors.New("unsupported image format")
	ErrHEICCodecMissing      = errors.New("HEIF codec is not installed")
	ErrHEICEncodeUnsupported = errors.New("HEIC output requires the Windows HEIF codec")
)

func decodeImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	switch FormatFromPath(path) {
	case FormatWebP:
		return webp.Decode(file)
	case FormatHEIC:
		if !heicCodecAvailable() {
			return nil, ErrHEICCodecMissing
		}
		return heic.Decode(file)
	case FormatJPEG, FormatPNG, FormatBMP, FormatTIFF:
		decoded, _, err := image.Decode(file)
		return decoded, err
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedInput, filepath.Ext(path))
	}
}

func ConvertFile(ctx context.Context, input string, options ConversionOptions) (FileResult, error) {
	result := FileResult{Input: input, Status: "failed"}
	if err := ctx.Err(); err != nil {
		result.Status = "canceled"
		return result, err
	}

	format := NormalizeFormat(options.Format)
	if format == "" {
		return result, fmt.Errorf("unsupported output format %q", options.Format)
	}
	if !IsSupportedInput(input) {
		return result, fmt.Errorf("%w: %s", ErrUnsupportedInput, filepath.Ext(input))
	}

	info, err := os.Stat(input)
	if err != nil {
		return result, err
	}
	if info.IsDir() {
		return result, fmt.Errorf("input is a directory")
	}

	img, err := decodeImage(input)
	if err != nil {
		return result, fmt.Errorf("decode %s: %w", filepath.Base(input), err)
	}
	if err := ctx.Err(); err != nil {
		result.Status = "canceled"
		return result, err
	}

	output, err := nextAvailablePath(input, format)
	if err != nil {
		return result, err
	}
	result.Output = output

	metadata, err := compatibleMetadata(input, format, options.PreserveMetadata)
	if err != nil {
		return result, err
	}

	temp, err := os.CreateTemp(filepath.Dir(output), ".convert-me-*"+extensionFor(format))
	if err != nil {
		return result, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	quality := normalizeQuality(options.Quality)
	if err := temp.Close(); err != nil {
		return result, err
	}
	var encodeErr error
	if format == FormatHEIC {
		if err := os.Remove(tempPath); err != nil {
			return result, err
		}
		encodeErr = encodeHEICFile(tempPath, flattenAlpha(img), quality)
	} else {
		var outputFile *os.File
		outputFile, encodeErr = os.OpenFile(tempPath, os.O_WRONLY|os.O_TRUNC, 0o644)
		if encodeErr == nil {
			encodeErr = encodeImage(outputFile, img, format, quality, metadata)
			if syncErr := outputFile.Sync(); encodeErr == nil {
				encodeErr = syncErr
			}
			if closeErr := outputFile.Close(); encodeErr == nil {
				encodeErr = closeErr
			}
		}
	}
	if encodeErr != nil {
		return result, fmt.Errorf("encode %s: %w", filepath.Base(output), encodeErr)
	}
	if err := os.Chtimes(tempPath, info.ModTime(), info.ModTime()); err != nil {
		return result, err
	}
	if err := os.Rename(tempPath, output); err != nil {
		return result, err
	}

	result.Status = "completed"
	return result, nil
}

func encodeImage(file *os.File, img image.Image, format ImageFormat, quality int, metadata []byte) error {
	switch format {
	case FormatJPEG:
		return encodeJPEG(file, flattenAlpha(img), quality, metadata)
	case FormatPNG:
		return png.Encode(file, img)
	case FormatBMP:
		return bmp.Encode(file, img)
	case FormatTIFF:
		return tiff.Encode(file, img, &tiff.Options{Compression: tiff.Deflate})
	case FormatWebP:
		options := webp.OptionsForPreset(webp.PresetPhoto, float32(quality))
		options.EXIF = metadata
		return webp.Encode(file, img, options)
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

func encodeJPEG(file *os.File, img image.Image, quality int, exif []byte) error {
	if len(exif) == 0 {
		return jpeg.Encode(file, img, &jpeg.Options{Quality: quality})
	}

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: quality}); err != nil {
		return err
	}
	data := encoded.Bytes()
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 || len(exif) > 65533 {
		return jpeg.Encode(file, img, &jpeg.Options{Quality: quality})
	}

	if _, err := file.Write(data[:2]); err != nil {
		return err
	}
	segmentLength := uint16(len(exif) + 2)
	if _, err := file.Write([]byte{0xff, 0xe1, byte(segmentLength >> 8), byte(segmentLength)}); err != nil {
		return err
	}
	if _, err := file.Write(exif); err != nil {
		return err
	}
	_, err := file.Write(data[2:])
	return err
}

func flattenAlpha(img image.Image) image.Image {
	bounds := img.Bounds()
	opaque := image.NewRGBA(bounds)
	draw.Draw(opaque, bounds, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(opaque, bounds, img, bounds.Min, draw.Over)
	return opaque
}

func normalizeQuality(quality int) int {
	if quality < 1 {
		return 85
	}
	if quality > 100 {
		return 100
	}
	return quality
}

func extensionFor(format ImageFormat) string {
	if option, ok := FormatOptionFor(format); ok {
		return option.Extension
	}
	return "." + string(format)
}

func nextAvailablePath(input string, format ImageFormat) (string, error) {
	dir := filepath.Dir(input)
	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	candidate := filepath.Join(dir, base+extensionFor(format))
	inputAbs, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}

	startSuffix := 0
	if strings.EqualFold(inputAbs, candidateAbs) {
		startSuffix = 1
	}
	for suffix := startSuffix; ; suffix++ {
		if suffix > 0 {
			candidate = filepath.Join(dir, base+fmt.Sprintf(" (%d)", suffix)+extensionFor(format))
		}
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
}

func previewDataURL(path string) (string, error) {
	img, err := decodeImage(path)
	if err != nil {
		return "", err
	}

	const maxWidth, maxHeight = 960, 640
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	scale := minFloat(float64(maxWidth)/float64(width), float64(maxHeight)/float64(height))
	if scale > 1 {
		scale = 1
	}
	previewWidth := maxInt(1, int(float64(width)*scale))
	previewHeight := maxInt(1, int(float64(height)*scale))
	preview := image.NewRGBA(image.Rect(0, 0, previewWidth, previewHeight))
	xdraw.CatmullRom.Scale(preview, preview.Bounds(), img, bounds, xdraw.Over, nil)

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, preview); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()), nil
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func resultForError(input string, err error) FileResult {
	status := "failed"
	if errors.Is(err, context.Canceled) {
		status = "canceled"
	}
	return FileResult{Input: input, Status: status, Error: err.Error()}
}

func elapsedSince(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}
