//go:build windows

package convert

import (
	"errors"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These tests read real HEIC pictures through Windows. A PC without Microsoft's HEIF and
// HEVC extensions cannot do that, so there they are skipped, and they say so.
func requireHEIC(t *testing.T) SystemImages {
	t.Helper()
	system := newSystemImages()
	if err := system.Check(); err != nil {
		t.Skipf("this PC cannot read HEIC photos, so this test is skipped: %v", err)
	}
	return system
}

func heicFixture(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "scripts", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func decodePicture(t *testing.T, path string) image.Image {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	picture, _, err := image.Decode(file)
	if err != nil {
		t.Fatalf("%s: %v", filepath.Base(path), err)
	}
	return picture
}

func TestWindowsTurnsAHEICPhotoIntoAPNG(t *testing.T) {
	system := requireHEIC(t)
	// The name has a space, signs that mean something on a command line, and letters outside ASCII.
	folder := filepath.Join(t.TempDir(), "Überraschung & more 写真")
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(heicFixture(t, "photo.heic"))
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(folder, "-holiday 100% (1).heic")
	if err := os.WriteFile(input, original, 0600); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(folder, "full.png")
	width, height, err := system.Export(input, output, 0)
	if err != nil {
		t.Fatal(err)
	}
	if width != 320 || height != 240 {
		t.Fatalf("size %d x %d, want 320 x 240", width, height)
	}
	got := decodePicture(t, output)
	if got.Bounds().Dx() != 320 || got.Bounds().Dy() != 240 {
		t.Fatalf("the PNG is %v, want 320 x 240", got.Bounds())
	}
	// The HEIC picture was made from photo.jpg. The same colours have to come back, which
	// also proves that red and blue did not change places on the way.
	want := decodePicture(t, heicFixture(t, "photo.jpg"))
	var difference, samples int64
	for y := 0; y < 240; y += 3 {
		for x := 0; x < 320; x += 3 {
			r1, g1, b1, _ := got.At(x, y).RGBA()
			r2, g2, b2, _ := want.At(x, y).RGBA()
			for _, pair := range [][2]uint32{{r1, r2}, {g1, g2}, {b1, b2}} {
				delta := int64(pair[0]>>8) - int64(pair[1]>>8)
				if delta < 0 {
					delta = -delta
				}
				difference += delta
				samples++
			}
		}
	}
	if average := float64(difference) / float64(samples); average > 6 {
		t.Errorf("the picture differs from its source by %.1f levels on average, want at most 6", average)
	}

	small := filepath.Join(folder, "small.png")
	width, height, err = system.Export(input, small, 100)
	if err != nil {
		t.Fatal(err)
	}
	if width != 320 || height != 240 {
		t.Errorf("a scaled export reports %d x %d, want the full size 320 x 240", width, height)
	}
	if bounds := decodePicture(t, small).Bounds(); bounds.Dx() != 100 || bounds.Dy() != 75 {
		t.Errorf("the small PNG is %v, want 100 x 75", bounds)
	}
	// The original is only read.
	if after, err := os.ReadFile(input); err != nil || string(after) != string(original) {
		t.Error("the HEIC photo was changed")
	}
}

func TestWindowsSaysSoWhenAFileIsNotAPicture(t *testing.T) {
	system := requireHEIC(t)
	folder := t.TempDir()
	input := filepath.Join(folder, "notes.heic")
	if err := os.WriteFile(input, []byte("this is text, not a photo"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(folder, "out.png")
	_, _, err := system.Export(input, output, 0)
	var failure *SystemImageError
	if !errors.As(err, &failure) || failure.Message != heicUnreadable || failure.Detail == "" {
		t.Fatalf("got %v, want the message for an unreadable HEIC photo with a detail", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Error("a failed export left a file behind")
	}
	if _, _, err := system.Export("relative.heic", output, 0); err == nil {
		t.Error("a relative path was accepted")
	}
}

// TestWindowsReadsSamplePhotos reads every HEIC file in the folder named by
// CONVERTME_TEST_HEIC_SAMPLES. It is for trying real phone photos by hand. With
// CONVERTME_TEST_HEIC_OUTPUT set to a folder, the pictures are kept there to look at.
func TestWindowsReadsSamplePhotos(t *testing.T) {
	folder := os.Getenv("CONVERTME_TEST_HEIC_SAMPLES")
	if folder == "" {
		t.Skip("set CONVERTME_TEST_HEIC_SAMPLES to a folder with HEIC photos to try them")
	}
	system := requireHEIC(t)
	samples, _ := filepath.Glob(filepath.Join(folder, "*.hei[cf]"))
	if len(samples) == 0 {
		t.Fatal("no HEIC files in " + folder)
	}
	keep := os.Getenv("CONVERTME_TEST_HEIC_OUTPUT")
	for _, sample := range samples {
		output := filepath.Join(t.TempDir(), "sample.png")
		if keep != "" {
			output = filepath.Join(keep, filepath.Base(sample)+".png")
			_ = os.Remove(output)
		}
		start := time.Now()
		width, height, err := system.Export(sample, output, 0)
		if err != nil {
			var failure *SystemImageError
			if errors.As(err, &failure) {
				t.Errorf("%s (told as HEIC: %v): %s %s", filepath.Base(sample), systemImage(sample), failure.Message, failure.Detail)
			} else {
				t.Errorf("%s: %v", filepath.Base(sample), err)
			}
			continue
		}
		file, err := os.Open(output)
		if err != nil {
			t.Fatal(err)
		}
		config, err := png.DecodeConfig(file)
		file.Close()
		if err != nil || config.Width != width || config.Height != height {
			t.Errorf("%s: the PNG is %d x %d (%v), want %d x %d", filepath.Base(sample), config.Width, config.Height, err, width, height)
		}
		info, _ := os.Stat(output)
		t.Logf("%-24s %5d x %-5d %6.2f MB PNG in %4d ms", filepath.Base(sample), width, height,
			float64(info.Size())/1e6, time.Since(start).Milliseconds())
	}
}
