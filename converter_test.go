package main

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNextAvailablePathNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(input, []byte("input"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := nextAvailablePath(input, FormatJPEG)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "photo (1).jpg"); output != want {
		t.Fatalf("expected %q, got %q", want, output)
	}
}

func TestNextAvailablePathAvoidsSameFile(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(input, []byte("input"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := nextAvailablePath(input, FormatPNG)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "photo (1).png"); output != want {
		t.Fatalf("expected %q, got %q", want, output)
	}
}

func TestConvertFileSupportedFormats(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "transparent.png")
	source := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			source.SetRGBA(x, y, color.RGBA{R: 240, G: 80, B: 60, A: uint8(x * 60)})
		}
	}

	file, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, source); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	for _, format := range []ImageFormat{FormatJPEG, FormatPNG, FormatWebP, FormatBMP, FormatTIFF} {
		result, err := ConvertFile(context.Background(), input, ConversionOptions{Format: string(format), Quality: 80})
		if err != nil {
			t.Fatalf("convert to %s: %v", format, err)
		}
		if result.Status != "completed" {
			t.Fatalf("expected completed result for %s, got %q", format, result.Status)
		}
		if _, err := os.Stat(result.Output); err != nil {
			t.Fatalf("expected output for %s: %v", format, err)
		}
		if _, err := decodeImage(result.Output); err != nil {
			t.Fatalf("expected output %s to decode: %v", format, err)
		}
	}
}

func TestConvertFileHEICWhenCodecAvailable(t *testing.T) {
	if !heicCodecAvailable() {
		t.Skip("Windows HEIF encoder is not installed")
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	file, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := ConvertFile(context.Background(), input, ConversionOptions{
		Format:  string(FormatHEIC),
		Quality: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result.Output)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 12 || string(data[4:8]) != "ftyp" {
		t.Fatalf("HEIC output has an invalid file signature")
	}
}

func TestRunQuickConversionCreatesOutputBesideInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "quick.png")
	source := image.NewRGBA(image.Rect(0, 0, 2, 2))
	source.Set(0, 0, color.RGBA{R: 10, G: 120, B: 240, A: 255})

	file, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, source); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	settings := defaultSettings()
	summary, err := runQuickConversion(context.Background(), LaunchRequest{
		Mode:   "convert",
		Format: "jpeg",
		Files:  []string{input},
	}, settings)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Completed != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	output := filepath.Join(dir, "quick.jpg")
	if summary.Results[0].Output != output {
		t.Fatalf("expected output %q, got %q", output, summary.Results[0].Output)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("expected quick-conversion output: %v", err)
	}
}

func TestRunQuickConversionRejectsEmptySelection(t *testing.T) {
	summary, err := runQuickConversion(context.Background(), LaunchRequest{
		Mode:   "convert",
		Format: "jpeg",
	}, defaultSettings())
	if err == nil {
		t.Fatal("expected an empty selection to fail")
	}
	if summary.Total != 0 || summary.Completed != 0 {
		t.Fatalf("empty selection must not produce a success summary: %+v", summary)
	}
}

func TestEmptyQuickConversionDoesNotNotify(t *testing.T) {
	notificationCalls := 0
	conversionErr, notificationErr := runQuickConversionAndNotify(
		context.Background(),
		LaunchRequest{Mode: "convert", Format: "jpeg"},
		defaultSettings(),
		func(ConversionSummary, ImageFormat, bool) error {
			notificationCalls++
			return nil
		},
	)
	if conversionErr == nil {
		t.Fatal("expected an empty selection to fail")
	}
	if notificationErr != nil {
		t.Fatalf("unexpected notification error: %v", notificationErr)
	}
	if notificationCalls != 0 {
		t.Fatalf("empty selection triggered %d notification(s)", notificationCalls)
	}
}

func TestRunQuickConversionConvertsMultipleFilesWithSpaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "images with spaces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	inputs := []string{
		filepath.Join(dir, "first image.png"),
		filepath.Join(dir, "second image.png"),
	}
	for _, input := range inputs {
		file, err := os.Create(input)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}

	summary, err := runQuickConversion(context.Background(), LaunchRequest{
		Mode:   "convert",
		Format: "jpeg",
		Files:  inputs,
	}, defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 2 || summary.Completed != 2 || summary.Failed != 0 {
		t.Fatalf("unexpected multi-file summary: %+v", summary)
	}
	for _, input := range inputs {
		output := strings.TrimSuffix(input, filepath.Ext(input)) + ".jpg"
		if _, err := os.Stat(output); err != nil {
			t.Fatalf("expected output %q: %v", output, err)
		}
	}
}
