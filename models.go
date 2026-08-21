package main

import (
	"path/filepath"
	"strings"
)

type ImageFormat string

const (
	FormatJPEG ImageFormat = "jpeg"
	FormatPNG  ImageFormat = "png"
	FormatWebP ImageFormat = "webp"
	FormatBMP  ImageFormat = "bmp"
	FormatTIFF ImageFormat = "tiff"
	FormatHEIC ImageFormat = "heic"
)

type FormatOption struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Extension     string `json:"extension"`
	Lossy         bool   `json:"lossy"`
	SupportsAlpha bool   `json:"supportsAlpha"`
}

type Settings struct {
	JPEGQuality         int  `json:"jpegQuality"`
	WebPQuality         int  `json:"webpQuality"`
	PreserveMetadata    bool `json:"preserveMetadata"`
	LaunchAtLogin       bool `json:"launchAtLogin"`
	ShowNotifications   bool `json:"showNotifications"`
	ExplorerIntegration bool `json:"explorerIntegration"`
}

type ConversionOptions struct {
	Format           string `json:"format"`
	Quality          int    `json:"quality"`
	PreserveMetadata bool   `json:"preserveMetadata"`
}

type LaunchRequest struct {
	Mode   string   `json:"mode"`
	Format string   `json:"format"`
	Files  []string `json:"files"`
}

type FileResult struct {
	Input  string `json:"input"`
	Output string `json:"output"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

type ConversionSummary struct {
	Total     int          `json:"total"`
	Completed int          `json:"completed"`
	Failed    int          `json:"failed"`
	Canceled  int          `json:"canceled"`
	Results   []FileResult `json:"results"`
}

func SupportedFormats() []FormatOption {
	return []FormatOption{
		{ID: string(FormatJPEG), Name: "JPEG", Extension: ".jpg", Lossy: true, SupportsAlpha: false},
		{ID: string(FormatPNG), Name: "PNG", Extension: ".png", Lossy: false, SupportsAlpha: true},
		{ID: string(FormatWebP), Name: "WebP", Extension: ".webp", Lossy: true, SupportsAlpha: true},
		{ID: string(FormatBMP), Name: "BMP", Extension: ".bmp", Lossy: false, SupportsAlpha: false},
		{ID: string(FormatTIFF), Name: "TIFF", Extension: ".tiff", Lossy: false, SupportsAlpha: true},
		{ID: string(FormatHEIC), Name: "HEIC", Extension: ".heic", Lossy: true, SupportsAlpha: false},
	}
}

func NormalizeFormat(value string) ImageFormat {
	normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "."))
	if normalized == "jpg" {
		return FormatJPEG
	}
	if normalized == "jpeg" {
		return FormatJPEG
	}
	if normalized == "heif" {
		return FormatHEIC
	}

	format := ImageFormat(normalized)
	for _, option := range SupportedFormats() {
		if format == ImageFormat(option.ID) {
			return format
		}
	}
	return ""
}

func FormatFromPath(path string) ImageFormat {
	return NormalizeFormat(filepath.Ext(path))
}

func FormatOptionFor(format ImageFormat) (FormatOption, bool) {
	for _, option := range SupportedFormats() {
		if ImageFormat(option.ID) == format {
			return option, true
		}
	}
	return FormatOption{}, false
}

func IsSupportedInput(path string) bool {
	return FormatFromPath(path) != ""
}
