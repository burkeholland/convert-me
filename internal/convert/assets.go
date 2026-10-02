package convert

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Asset is one engine file with the size and SHA-256 it must have.
type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Manifest is embedded in the application at build time. It pins the exact engine files
// the app was built and tested with, so a damaged or swapped file is noticed at startup.
type Manifest struct {
	Engine map[string]string `json:"engine"`
	Files  []Asset           `json:"files"`
}

const (
	ffmpegAsset  = "runtime/ffmpeg/bin/ffmpeg.exe"
	ffprobeAsset = "runtime/ffmpeg/bin/ffprobe.exe"
)

// ParseManifest reads the embedded manifest and rejects anything but the two engine files.
func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("read runtime manifest: %w", err)
	}
	found := map[string]bool{ffmpegAsset: false, ffprobeAsset: false}
	for _, asset := range manifest.Files {
		seen, expected := found[asset.Path]
		if !expected {
			return manifest, fmt.Errorf("unexpected runtime manifest path: %s", asset.Path)
		}
		if seen {
			return manifest, fmt.Errorf("duplicate runtime manifest path: %s", asset.Path)
		}
		hash, err := hex.DecodeString(asset.SHA256)
		if err != nil || len(hash) != sha256.Size || hex.EncodeToString(hash) != asset.SHA256 || asset.Size <= 0 {
			return manifest, fmt.Errorf("invalid runtime manifest entry: %s", asset.Path)
		}
		found[asset.Path] = true
	}
	for path, present := range found {
		if !present {
			return manifest, fmt.Errorf("runtime manifest is missing %s", path)
		}
	}
	return manifest, nil
}

// EngineLabel is the engine name and version for the About box.
func (m Manifest) EngineLabel() string {
	if version := strings.TrimSpace(m.Engine["ffmpeg"]); version != "" {
		return "FFmpeg " + version
	}
	return "FFmpeg"
}

// VerifyManifest checks every engine file under root against the manifest.
func VerifyManifest(ctx context.Context, root string, manifest Manifest) error {
	for _, asset := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := verifyAsset(root, asset); err != nil {
			return fmt.Errorf("%s: %w", asset.Path, err)
		}
	}
	return nil
}

func verifyAsset(root string, asset Asset) error {
	file, err := os.Open(filepath.Join(root, filepath.FromSlash(asset.Path)))
	if err != nil {
		return errors.New("the file is missing")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != asset.Size {
		return errors.New("unexpected file size")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return errors.New("the file does not match the version this app was built with")
	}
	return nil
}
