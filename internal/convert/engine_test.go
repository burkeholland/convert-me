package convert

// These tests run the real conversion engine on the files in scripts/fixtures. They are
// skipped unless CONVERTME_TEST_ENGINE names the folder with ffmpeg.exe and ffprobe.exe.
// scripts/test-native.ps1 sets that up and is part of the normal build.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"
)

// awkwardFolder has spaces, non-Latin text and characters that mean something to shells
// and to the engine's own pattern syntax. Conversions must not care.
const awkwardFolder = "Local data 日本 & (1) 100%d #1"

func realEngine(t *testing.T) Engine {
	t.Helper()
	folder := os.Getenv("CONVERTME_TEST_ENGINE")
	if folder == "" {
		t.Skip("set CONVERTME_TEST_ENGINE to the engine folder to run real conversions")
	}
	engine := Engine{FFmpeg: filepath.Join(folder, "ffmpeg.exe"), FFprobe: filepath.Join(folder, "ffprobe.exe"), Run: RunProcess}
	for _, tool := range []string{engine.FFmpeg, engine.FFprobe} {
		if _, err := os.Stat(tool); err != nil {
			t.Fatalf("engine file missing: %v", err)
		}
	}
	return engine
}

func fixtureFolder(t *testing.T) string {
	t.Helper()
	folder := os.Getenv("CONVERTME_TEST_FIXTURES")
	if folder == "" {
		folder = filepath.Join("..", "..", "scripts", "fixtures")
	}
	absolute, err := filepath.Abs(folder)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(absolute, "photo.jpg")); err != nil {
		t.Fatalf("fixtures not found in %s", absolute)
	}
	return absolute
}

func copyFile(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// look describes what a still picture fixture shows, so results can be checked by pixel.
type look int

const (
	lookAny     look = iota // only the size is checked
	lookColor               // red marker top-left, colour gradient
	lookRotated             // the marker ends up top-right once the EXIF rotation is applied
	lookAlpha               // left half transparent, right half opaque gradient
	lookPalette             // like lookAlpha, with too few colours for an exact gradient
	lookGray                // no colour at all
)

type sample struct {
	name          string
	kind          Kind
	refuse        string // not empty: the file must be refused with a message containing this
	width, height int    // size as shown, after any rotation
	look          look
	highRes       bool // sound with more than 16 bits
	channels      int
	silent        bool
	hdr           bool
	interlaced    bool
	cover         bool
	// copied lists targets that must be made by copying, already those that need no work.
	copied, already []string
	// reencoded lists targets where nothing may be copied.
	reencoded []string
}

var samples = []sample{
	{name: "photo.jpg", kind: KindImage, width: 320, height: 240, look: lookColor, already: []string{"jpg"}},
	{name: "photo-progressive.jpg", kind: KindImage, width: 320, height: 240, look: lookColor, already: []string{"jpg"}},
	{name: "photo-gray.jpg", kind: KindImage, width: 320, height: 240, look: lookGray, already: []string{"jpg"}},
	{name: "photo-cmyk.jpg", kind: KindImage, width: 320, height: 240, look: lookColor, already: []string{"jpg"}},
	{name: "photo-exif-rotated.jpg", kind: KindImage, width: 240, height: 320, look: lookRotated, already: []string{"jpg"}},
	{name: "picture-rgb.png", kind: KindImage, width: 320, height: 240, look: lookColor, already: []string{"png"}},
	{name: "picture-alpha.png", kind: KindImage, width: 320, height: 240, look: lookAlpha, already: []string{"png"}},
	{name: "picture-palette.png", kind: KindImage, width: 320, height: 240, look: lookPalette, already: []string{"png"}},
	{name: "picture-gray16.png", kind: KindImage, width: 320, height: 240, look: lookGray, already: []string{"png"}},
	{name: "picture-odd-size.png", kind: KindImage, width: 33, height: 17, look: lookAny, already: []string{"png"}},
	{name: "picture-lossy.webp", kind: KindImage, width: 320, height: 240, look: lookColor, already: []string{"webp"}},
	{name: "picture-lossless.webp", kind: KindImage, width: 320, height: 240, look: lookColor, already: []string{"webp"}},
	{name: "picture-alpha.webp", kind: KindImage, width: 320, height: 240, look: lookAlpha, already: []string{"webp"}},
	{name: "picture.gif", kind: KindImage, width: 320, height: 240, look: lookColor, already: []string{"gif"}},
	{name: "picture.bmp", kind: KindImage, width: 160, height: 120, look: lookColor, already: []string{"bmp"}},
	{name: "picture-alpha.bmp", kind: KindImage, width: 160, height: 120, look: lookAlpha, already: []string{"bmp"}},
	{name: "picture-lzw.tiff", kind: KindImage, width: 160, height: 120, look: lookColor, already: []string{"tiff"}},
	{name: "picture-deflate.tiff", kind: KindImage, width: 160, height: 120, look: lookColor, already: []string{"tiff"}},
	{name: "picture-plain.tif", kind: KindImage, width: 160, height: 120, look: lookColor, already: []string{"tiff"}},
	{name: "picture-alpha.tiff", kind: KindImage, width: 160, height: 120, look: lookAlpha, already: []string{"tiff"}},
	{name: "animated.webp", refuse: "animated WebP"},
	{name: "not-really.png", refuse: ""},

	{name: "tone.mp3", kind: KindAudio, channels: 2, already: []string{"mp3"}},
	{name: "tone-cover.mp3", kind: KindAudio, channels: 2, cover: true, already: []string{"mp3"}},
	{name: "tone-16bit.wav", kind: KindAudio, channels: 2, already: []string{"wav"}},
	{name: "tone-24bit.wav", kind: KindAudio, channels: 2, highRes: true, already: []string{"wav"}},
	{name: "tone-float.wav", kind: KindAudio, channels: 2, highRes: true, already: []string{"wav"}},
	{name: "tone-mono.wav", kind: KindAudio, channels: 1, already: []string{"wav"}},
	{name: "tone-surround.wav", kind: KindAudio, channels: 6, already: []string{"wav"}},
	{name: "tone-16bit.flac", kind: KindAudio, channels: 2, already: []string{"flac"}},
	{name: "tone-24bit.flac", kind: KindAudio, channels: 2, highRes: true, already: []string{"flac"}},
	{name: "tone.m4a", kind: KindAudio, channels: 2, already: []string{"m4a"}},
	{name: "tone.aac", kind: KindAudio, channels: 2},
	{name: "tone.ogg", kind: KindAudio, channels: 2},
	{name: "tone.opus", kind: KindAudio, channels: 2},
	{name: "tone.wma", kind: KindAudio, channels: 2},
	{name: "tone.aiff", kind: KindAudio, channels: 2},
	{name: "garbage.mp3", refuse: ""},

	{name: "h264-aac.mp4", kind: KindVideo, width: 320, height: 240, channels: 2, already: []string{"mp4"}, copied: []string{"mov", "mkv"}},
	{name: "h264-aac.mov", kind: KindVideo, width: 320, height: 240, channels: 2, already: []string{"mov"}, copied: []string{"mp4", "mkv"}},
	{name: "h264-aac.mkv", kind: KindVideo, width: 320, height: 240, channels: 2, already: []string{"mkv"}, copied: []string{"mp4", "mov"}},
	{name: "h264-ac3.mkv", kind: KindVideo, width: 320, height: 240, channels: 2, copied: []string{"mp4", "mov", "mkv"}},
	{name: "h264-aac.ts", kind: KindVideo, width: 320, height: 240, channels: 2, reencoded: []string{"mp4", "mov", "mkv"}},
	{name: "h264-silent.mp4", kind: KindVideo, width: 320, height: 240, silent: true, already: []string{"mp4"}, copied: []string{"mov", "mkv"}},
	{name: "h264-rotated.mp4", kind: KindVideo, width: 240, height: 320, channels: 2, already: []string{"mp4"}, copied: []string{"mov", "mkv"}},
	{name: "h264-odd-size.mkv", kind: KindVideo, width: 319, height: 239, channels: 2, copied: []string{"mp4", "mov", "mkv"}},
	{name: "hevc-aac.mov", kind: KindVideo, width: 320, height: 240, channels: 2, copied: []string{"mp4", "mov", "mkv"}},
	{name: "hevc-hdr.mov", kind: KindVideo, width: 320, height: 240, channels: 2, hdr: true},
	{name: "vp9-opus.webm", kind: KindVideo, width: 320, height: 240, channels: 2, reencoded: []string{"mp4", "mov", "mkv"}},
	{name: "vp8-vorbis.webm", kind: KindVideo, width: 320, height: 240, channels: 2},
	{name: "mpeg4-mp3.avi", kind: KindVideo, width: 320, height: 240, channels: 2},
	{name: "wmv2-wma.wmv", kind: KindVideo, width: 320, height: 240, channels: 2},
	{name: "mpeg2-mp2.mpg", kind: KindVideo, width: 320, height: 240, channels: 2},
	{name: "mpeg2-interlaced.mpg", kind: KindVideo, width: 352, height: 288, channels: 2, interlaced: true},
	{name: "prores-pcm.mov", kind: KindVideo, width: 320, height: 240, channels: 2},
	{name: "animated.gif", kind: KindVideo, width: 160, height: 120, silent: true, already: []string{"gif"}},
	{name: "animated-tiny.gif", kind: KindVideo, width: 24, height: 20, silent: true, already: []string{"gif"}},
	{name: "av1-opus.mp4", refuse: "AV1"},
	{name: "truncated.mp4", refuse: ""},
}

func contains(list []string, value string) bool {
	for _, entry := range list {
		if entry == value {
			return true
		}
	}
	return false
}

// TestEngineKnowsEveryFixture makes sure a new fixture cannot be added without saying what
// should happen to it.
func TestEngineKnowsEveryFixture(t *testing.T) {
	realEngine(t)
	listed := map[string]bool{}
	for _, entry := range samples {
		listed[entry.name] = true
	}
	entries, err := os.ReadDir(fixtureFolder(t))
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, entry := range entries {
		if entry.Name() == "README.txt" {
			continue
		}
		found++
		if !listed[entry.Name()] {
			t.Errorf("fixture %s has no expectation in the conversion matrix", entry.Name())
		}
	}
	if found != len(samples) {
		t.Errorf("%d fixtures on disk, %d in the matrix", found, len(samples))
	}
}

// TestEngineFormatListMatchesTheBuild checks that every format the app offers has an
// encoder in the engine, and that every reader the app lists has a decoder.
func TestEngineFormatListMatchesTheBuild(t *testing.T) {
	engine := realEngine(t)
	ctx := context.Background()
	decoders, err := engine.Decoders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, codec := range []string{
		"mjpeg", "png", "webp", "gif", "bmp", "tiff",
		"mp3", "pcm_s16le", "pcm_s24le", "flac", "aac", "alac", "vorbis", "opus", "wmav2", "pcm_s16be",
		"h264", "hevc", "vp8", "vp9", "mpeg4", "wmv2", "mpeg2video", "prores",
	} {
		if !decoders[codec] {
			t.Errorf("the engine has no decoder for %s", codec)
		}
	}
	if decoders["av1"] {
		t.Error("the engine can read AV1 now: list it as supported and update the fixtures")
	}
	if caps := engine.SelfTest(ctx); !caps.H264 {
		t.Fatalf("the Windows H.264 encoder did not start: %s", caps.H264Reason)
	}
}

func TestEngineConvertsEveryOfferedFormat(t *testing.T) {
	engine := realEngine(t)
	fixtures := fixtureFolder(t)
	workspace := filepath.Join(t.TempDir(), awkwardFolder)
	inputs, outputs := filepath.Join(workspace, "in"), filepath.Join(workspace, "out put")
	for _, folder := range []string{inputs, outputs} {
		if err := os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
	}
	decoders, err := engine.Decoders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, entry := range samples {
		copyFile(t, filepath.Join(fixtures, entry.name), filepath.Join(inputs, entry.name))
		before[entry.name] = fileHash(t, filepath.Join(inputs, entry.name))
	}

	var counts struct {
		sync.Mutex
		converted, skipped, refused int
	}
	slots := make(chan struct{}, max(2, runtime.NumCPU()/2))
	t.Run("matrix", func(t *testing.T) {
		for _, entry := range samples {
			t.Run(entry.name, func(t *testing.T) {
				t.Parallel()
				slots <- struct{}{}
				defer func() { <-slots }()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				input := filepath.Join(inputs, entry.name)
				media, err := engine.Probe(ctx, input)
				if err == nil && media.Kind != KindAudio && !decoders[media.VideoCodec] {
					err = fmt.Errorf("uses %s, which cannot be read yet", codecName(media.VideoCodec))
				}
				if entry.kind == "" {
					if err == nil {
						t.Fatalf("expected the file to be refused, but it was read as %+v", media)
					}
					if !strings.Contains(err.Error(), entry.refuse) {
						t.Fatalf("refused with %q, expected a message containing %q", err, entry.refuse)
					}
					counts.Lock()
					counts.refused++
					counts.Unlock()
					return
				}
				if err != nil {
					t.Fatalf("probe: %v", err)
				}
				checkProbe(t, entry, media)
				for _, id := range offered[media.Kind] {
					target, _ := targetByID(id)
					if reason := skipReason(media, target); reason != "" {
						expected := contains(entry.already, id) || (entry.silent && target.Kind == KindAudio)
						if !expected {
							t.Errorf("-> %s was skipped unexpectedly: %s", target.Label, reason)
						}
						counts.Lock()
						counts.skipped++
						counts.Unlock()
						continue
					}
					if contains(entry.already, id) {
						t.Errorf("-> %s should have been skipped as already in that format", target.Label)
					}
					output := partialPath(outputs, stem(entry.name), target.Ext)
					workDir := filepath.Join(workspace, "work", token())
					if err := os.MkdirAll(workDir, 0700); err != nil {
						t.Fatal(err)
					}
					var reported []float64
					outcome, err := engine.Convert(ctx, Job{Media: media, Target: target, Input: input, Output: output, WorkDir: workDir},
						func(percent float64) { reported = append(reported, percent) })
					if err != nil {
						message, detail := explain(err)
						t.Errorf("-> %s failed: %s\n%s", target.Label, message, detail)
						continue
					}
					for _, percent := range reported {
						if percent < 0 || percent > 100.0001 {
							t.Errorf("-> %s reported progress %v", target.Label, percent)
						}
					}
					if media.DurationMs > 0 && len(reported) == 0 {
						t.Errorf("-> %s reported no progress", target.Label)
					}
					checkOutput(t, engine, entry, media, target, output, outcome)
					counts.Lock()
					counts.converted++
					counts.Unlock()
				}
			})
		}
	})

	for _, entry := range samples {
		if after := fileHash(t, filepath.Join(inputs, entry.name)); after != before[entry.name] {
			t.Errorf("%s was changed by converting it", entry.name)
		}
	}
	leftovers, _ := os.ReadDir(inputs)
	if len(leftovers) != len(samples) {
		t.Errorf("the input folder has %d entries, expected only the %d originals", len(leftovers), len(samples))
	}
	t.Logf("%d conversions checked, %d skipped as unnecessary or impossible, %d files refused", counts.converted, counts.skipped, counts.refused)
	if counts.converted < 250 {
		t.Errorf("only %d conversions ran, the matrix looks incomplete", counts.converted)
	}
}

func checkProbe(t *testing.T, entry sample, media Media) {
	t.Helper()
	if media.Kind != entry.kind {
		t.Fatalf("read as %s, expected %s", media.Kind, entry.kind)
	}
	if entry.kind != KindAudio && (media.Width != entry.width || media.Height != entry.height) {
		t.Errorf("size read as %dx%d, expected %dx%d", media.Width, media.Height, entry.width, entry.height)
	}
	if entry.kind != KindImage && media.HasAudio == entry.silent {
		t.Errorf("sound detected: %v, expected %v", media.HasAudio, !entry.silent)
	}
	if media.HasAudio && media.Channels != entry.channels {
		t.Errorf("%d channels, expected %d", media.Channels, entry.channels)
	}
	if high := media.AudioBits > 16 || media.FloatAudio; entry.kind == KindAudio && high != entry.highRes {
		t.Errorf("high resolution sound: %v, expected %v (bits %d, float %v)", high, entry.highRes, media.AudioBits, media.FloatAudio)
	}
	if media.HDR != entry.hdr || media.Interlaced != entry.interlaced || media.CoverArt != entry.cover {
		t.Errorf("HDR %v, interlaced %v, cover %v: expected %v, %v, %v", media.HDR, media.Interlaced, media.CoverArt, entry.hdr, entry.interlaced, entry.cover)
	}
	if entry.kind != KindImage && media.DurationMs < 500 {
		t.Errorf("duration read as %d ms", media.DurationMs)
	}
	if wantAlpha := entry.look == lookAlpha || entry.look == lookPalette; entry.kind == KindImage && wantAlpha && !media.Alpha {
		t.Errorf("transparency was not detected (pixel format %s)", media.PixFmt)
	}
}

func checkOutput(t *testing.T, engine Engine, entry sample, media Media, target Target, output string, outcome Outcome) {
	t.Helper()
	label := "-> " + target.Label
	ctx := context.Background()
	result, err := engine.Probe(ctx, output)
	if err != nil {
		t.Errorf("%s: the result cannot be read back: %v", label, err)
		return
	}
	switch {
	case media.Kind == KindImage:
		checkImage(t, entry, target, output, label)
	case target.Kind == KindAudio:
		checkSound(t, engine, entry, media, target, result, output, label)
	case target.ID == "gif":
		if !result.Animated || result.Format != "gif" {
			t.Errorf("%s: expected an animated GIF, got %+v", label, result)
		}
		if result.Width > 480 || result.Width != min(480, media.Width) {
			t.Errorf("%s: width %d for a source of %d", label, result.Width, media.Width)
		}
		checkMoving(t, engine, output, label)
	default:
		checkVideo(t, engine, entry, media, target, result, output, outcome, label)
	}
}

func decodeStill(path, id string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	switch id {
	case "jpg":
		return jpeg.Decode(file)
	case "png":
		return png.Decode(file)
	case "gif":
		return gif.Decode(file)
	case "bmp":
		return bmp.Decode(file)
	case "tiff":
		return tiff.Decode(file)
	case "webp":
		return webp.Decode(file)
	}
	return nil, fmt.Errorf("no decoder for %s", id)
}

func pixel(picture image.Image, x, y int) color.NRGBA {
	bounds := picture.Bounds()
	return color.NRGBAModel.Convert(picture.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
}

func near(value uint8, want, tolerance int) bool {
	return math.Abs(float64(int(value)-want)) <= float64(tolerance)
}

func reddish(c color.NRGBA) bool {
	return c.R > 150 && c.G < 100 && c.B < 100
}

// checkImage reads the result with a decoder that has nothing to do with the engine and
// looks at the pixels: right size, right way up, transparency kept or painted white.
func checkImage(t *testing.T, entry sample, target Target, output, label string) {
	t.Helper()
	picture, err := decodeStill(output, target.ID)
	if err != nil {
		t.Errorf("%s: an independent %s decoder rejects the file: %v", label, target.Label, err)
		return
	}
	width, height := picture.Bounds().Dx(), picture.Bounds().Dy()
	if width != entry.width || height != entry.height {
		t.Errorf("%s: %dx%d, expected %dx%d", label, width, height, entry.width, entry.height)
		return
	}
	keepsAlpha := target.ID != "jpg" && target.ID != "bmp"
	// GIF has 256 colours at most, so a gradient that went through one is only roughly right.
	tolerance := 14
	if target.ID == "gif" || strings.HasSuffix(entry.name, ".gif") {
		tolerance = 60
	}
	markerLeft, markerRight := pixel(picture, width/16, height/16), pixel(picture, width-1-width/16, height/16)
	low := pixel(picture, 3*width/4, 3*height/4)
	switch entry.look {
	case lookColor:
		if !reddish(markerLeft) || reddish(markerRight) {
			t.Errorf("%s: the corner marker is wrong: left %v, right %v", label, markerLeft, markerRight)
		}
		if !near(low.R, 191, tolerance) || !near(low.G, 191, tolerance) || low.A != 255 {
			t.Errorf("%s: the gradient is off: %v", label, low)
		}
	case lookRotated:
		// The stored picture is 320x240. Shown upright it is 240x320 with the marker top-right.
		if reddish(markerLeft) || !reddish(markerRight) {
			t.Errorf("%s: the photo is not the right way up: left %v, right %v", label, markerLeft, markerRight)
		}
	case lookAlpha, lookPalette:
		hidden := pixel(picture, width/4, height/2)
		if keepsAlpha {
			if hidden.A != 0 {
				t.Errorf("%s: the transparent half is no longer transparent: %v", label, hidden)
			}
		} else if hidden.R < 250 || hidden.G < 250 || hidden.B < 250 || hidden.A != 255 {
			t.Errorf("%s: the transparent half should be white, got %v", label, hidden)
		}
		if low.A != 255 {
			t.Errorf("%s: the solid half became transparent: %v", label, low)
		}
		if entry.look == lookAlpha && (!near(low.R, 191, tolerance) || !near(low.G, 191, tolerance)) {
			t.Errorf("%s: the gradient is off: %v", label, low)
		}
	case lookGray:
		if !near(low.R, int(low.G), 3) || !near(low.G, int(low.B), 3) || low.A != 255 {
			t.Errorf("%s: a gray picture came out coloured: %v", label, low)
		}
	}
}

// decodeTone turns the sound of a file into plain mono samples using the engine.
func decodeTone(engine Engine, path string) ([]int16, error) {
	data, err := engine.Run(context.Background(), engine.FFmpeg, []string{
		"-hide_banner", "-nostdin", "-v", "error", "-protocol_whitelist", "file,pipe",
		"-i", inputURL(path), "-map", "0:a:0", "-vn", "-ac", "1", "-ar", "8000",
		"-c:a", "pcm_s16le", "-f", "wav", "pipe:1",
	}, nil)
	if err != nil {
		return nil, err
	}
	start := bytes.Index(data, []byte("data"))
	if start < 0 || start+8 > len(data) {
		return nil, fmt.Errorf("no sound data in %d bytes", len(data))
	}
	raw := data[start+8:]
	samples := make([]int16, len(raw)/2)
	if err := binary.Read(bytes.NewReader(raw[:len(samples)*2]), binary.LittleEndian, samples); err != nil {
		return nil, err
	}
	return samples, nil
}

// checkTone confirms the sound is the 440 Hz test tone, not silence and not noise.
func checkTone(t *testing.T, engine Engine, path, label string) {
	t.Helper()
	all, err := decodeTone(engine, path)
	if err != nil {
		t.Errorf("%s: the sound cannot be decoded: %v", label, err)
		return
	}
	if len(all) < 2000 {
		t.Errorf("%s: only %d samples of sound", label, len(all))
		return
	}
	// Lossy formats add a little silence at the ends, so only the middle is measured.
	middle := all[len(all)/5 : len(all)*4/5]
	crossings, energy := 0, 0.0
	for i, value := range middle {
		energy += float64(value) * float64(value)
		if i > 0 && (middle[i-1] < 0) != (value < 0) {
			crossings++
		}
	}
	frequency := float64(crossings) / 2 / (float64(len(middle)) / 8000)
	loudness := math.Sqrt(energy / float64(len(middle)))
	if frequency < 425 || frequency > 455 || loudness < 300 {
		t.Errorf("%s: expected a 440 Hz tone, measured %.0f Hz at level %.0f", label, frequency, loudness)
	}
}

func checkSound(t *testing.T, engine Engine, entry sample, media Media, target Target, result Media, output, label string) {
	t.Helper()
	wantCodec := map[string]string{"mp3": "mp3", "m4a": "aac", "flac": "flac", "wav": "pcm_s16le"}[target.ID]
	if target.ID == "wav" && entry.highRes {
		wantCodec = "pcm_s24le"
	}
	wantFormat := map[string]string{"mp3": "mp3", "m4a": "mov", "flac": "flac", "wav": "wav"}[target.ID]
	if result.Kind != KindAudio || result.AudioCodec != wantCodec || result.Format != wantFormat {
		t.Errorf("%s: got %s sound in a %s file, expected %s in %s", label, result.AudioCodec, result.Format, wantCodec, wantFormat)
	}
	wantChannels := media.Channels
	if target.ID == "mp3" {
		wantChannels = min(2, media.Channels)
	}
	if result.Channels != wantChannels {
		t.Errorf("%s: %d channels, expected %d", label, result.Channels, wantChannels)
	}
	if target.ID == "flac" || target.ID == "wav" {
		wantBits := 16
		if entry.highRes {
			wantBits = 24
		}
		if result.AudioBits != wantBits {
			t.Errorf("%s: %d bits, expected %d", label, result.AudioBits, wantBits)
		}
	}
	if difference := math.Abs(float64(result.DurationMs - media.DurationMs)); difference > 200 {
		t.Errorf("%s: %d ms long, the source is %d ms", label, result.DurationMs, media.DurationMs)
	}
	if entry.cover && target.ID != "wav" && !result.CoverArt {
		t.Errorf("%s: the cover picture was lost", label)
	}
	checkTone(t, engine, output, label)
}

// frame grabs one picture from a video through the engine.
func frame(engine Engine, path string) (image.Image, error) {
	data, err := engine.Run(context.Background(), engine.FFmpeg, []string{
		"-hide_banner", "-nostdin", "-v", "error", "-protocol_whitelist", "file,pipe",
		"-i", inputURL(path), "-map", "0:v:0", "-frames:v", "1", "-an",
		"-c:v", "png", "-f", "image2pipe", "pipe:1",
	}, nil)
	if err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(data))
}

// checkMoving makes sure a video result shows a real picture and not a blank frame.
func checkMoving(t *testing.T, engine Engine, path, label string) image.Image {
	t.Helper()
	picture, err := frame(engine, path)
	if err != nil {
		t.Errorf("%s: no frame can be read from the result: %v", label, err)
		return nil
	}
	seen := map[color.NRGBA]bool{}
	bounds := picture.Bounds()
	for y := 0; y < bounds.Dy(); y += max(1, bounds.Dy()/12) {
		for x := 0; x < bounds.Dx(); x += max(1, bounds.Dx()/12) {
			c := pixel(picture, x, y)
			// Rounded so compression noise does not count as detail.
			seen[color.NRGBA{R: c.R / 32, G: c.G / 32, B: c.B / 32}] = true
		}
	}
	if len(seen) < 6 {
		t.Errorf("%s: the picture looks blank (%d distinct colours)", label, len(seen))
	}
	return picture
}

func checkVideo(t *testing.T, engine Engine, entry sample, media Media, target Target, result Media, output string, outcome Outcome, label string) {
	t.Helper()
	wantFormat := "mov"
	if target.ID == "mkv" {
		wantFormat = "matroska"
	}
	if result.Kind != KindVideo || result.VideoCodec != "h264" || result.Format != wantFormat {
		t.Errorf("%s: got %s video in a %s file", label, result.VideoCodec, result.Format)
	}
	copiedVideo, _ := copyable(media, target)
	wantWidth, wantHeight := media.Width, media.Height
	if !copiedVideo {
		wantWidth, wantHeight = h264Size(media.Width, media.Height)
		if result.PixFmt != "yuv420p" {
			t.Errorf("%s: pixel format %s will not play everywhere", label, result.PixFmt)
		}
		if result.Rotation != 0 {
			t.Errorf("%s: a re-encoded video still carries a rotation of %d", label, result.Rotation)
		}
	}
	if result.Width != wantWidth || result.Height != wantHeight {
		t.Errorf("%s: %dx%d, expected %dx%d", label, result.Width, result.Height, wantWidth, wantHeight)
	}
	if result.HasAudio != media.HasAudio {
		t.Errorf("%s: sound present %v, expected %v", label, result.HasAudio, media.HasAudio)
	}
	if result.HasAudio && result.AudioCodec != "aac" {
		t.Errorf("%s: sound is %s, expected AAC", label, result.AudioCodec)
	}
	if difference := math.Abs(float64(result.DurationMs - media.DurationMs)); difference > 250 {
		t.Errorf("%s: %d ms long, the source is %d ms", label, result.DurationMs, media.DurationMs)
	}
	if contains(entry.copied, target.ID) && !outcome.Copied {
		t.Errorf("%s: expected picture or sound to be copied without re-encoding", label)
	}
	if contains(entry.reencoded, target.ID) && outcome.Copied {
		t.Errorf("%s: nothing in this file can be copied safely", label)
	}
	if picture := checkMoving(t, engine, output, label); picture != nil {
		// The engine applies a stored rotation when it reads a frame, so this is the size a
		// player shows.
		if shown := picture.Bounds(); shown.Dx() != wantWidth || shown.Dy() != wantHeight {
			t.Errorf("%s: plays at %dx%d, expected %dx%d", label, shown.Dx(), shown.Dy(), wantWidth, wantHeight)
		}
	}
	if result.HasAudio {
		checkTone(t, engine, output, label)
	}
}

// realService builds a service around a private copy of the engine, with a manifest made
// for that copy, and runs the same start-up checks as the app.
func realService(t *testing.T) (*Service, string) {
	t.Helper()
	engine := realEngine(t)
	root, dataDir := filepath.Join(t.TempDir(), "app root"), filepath.Join(t.TempDir(), "app data")
	bin := filepath.Join(root, "runtime", "ffmpeg", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Engine: map[string]string{"ffmpeg": "test"}}
	for asset, source := range map[string]string{ffmpegAsset: engine.FFmpeg, ffprobeAsset: engine.FFprobe} {
		destination := filepath.Join(root, filepath.FromSlash(asset))
		copyFile(t, source, destination)
		info, err := os.Stat(destination)
		if err != nil {
			t.Fatal(err)
		}
		manifest.Files = append(manifest.Files, Asset{Path: asset, SHA256: fileHash(t, destination), Size: info.Size()})
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(root, dataDir, RunProcess)
	t.Cleanup(service.Close)
	if err := service.Initialize(data); err != nil {
		t.Fatalf("start-up checks failed: %v", err)
	}
	return service, dataDir
}

func TestEngineServiceConvertsABatchSafely(t *testing.T) {
	service, dataDir := realService(t)
	fixtures := fixtureFolder(t)
	workspace := filepath.Join(t.TempDir(), awkwardFolder)
	inputs, destination := filepath.Join(workspace, "originals"), filepath.Join(workspace, "converted files")
	for _, folder := range []string{inputs, destination} {
		if err := os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// A very long name and names that look like options or patterns.
	long := strings.Repeat("long name ", 22) + "end.png"
	names := map[string]string{
		"photo-exif-rotated.jpg": "IMG_0001 (edited) [final].jpg",
		"picture-alpha.png":      "-i %03d.png",
		"picture-rgb.png":        long,
		"tone-16bit.wav":         "Ünïcödé 音楽 🎵.wav",
		"hevc-aac.mov":           "holiday;clip & more.mov",
		"av1-opus.mp4":           "modern.mp4",
		"truncated.mp4":          "broken.mp4",
		"animated.webp":          "sticker.webp",
	}
	var paths []string
	before := map[string]string{}
	for source, name := range names {
		path := filepath.Join(inputs, name)
		copyFile(t, filepath.Join(fixtures, source), path)
		before[path] = fileHash(t, path)
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if err := service.SetDestination(DestinationFolder, destination); err != nil {
		t.Fatal(err)
	}
	for kind, id := range map[Kind]string{KindImage: "png", KindAudio: "mp3", KindVideo: "mp4"} {
		if err := service.SetTarget(kind, id); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := addAndWait(t, service, paths...)

	for name, want := range map[string]string{
		"modern.mp4": "AV1", "broken.mp4": "could not read this file", "sticker.webp": "Animated WebP",
	} {
		if item := itemNamed(t, snapshot, name); item.Status != StatusUnsupported || !strings.Contains(item.Error, want) {
			t.Errorf("%s: status %s, message %q", name, item.Status, item.Error)
		}
	}
	rotated := itemNamed(t, snapshot, "IMG_0001 (edited) [final].jpg")
	if rotated.Width != 240 || rotated.Height != 320 {
		t.Errorf("the rotated photo is listed as %dx%d", rotated.Width, rotated.Height)
	}
	waitFor(t, service, "thumbnails", func(s Snapshot) bool {
		return itemNamed(t, s, "holiday;clip & more.mov").Thumb && itemNamed(t, s, long).Thumb
	})
	thumb, err := jpeg.Decode(bytes.NewReader(service.Thumbnail(rotated.ID)))
	if err != nil || thumb.Bounds().Dx() != 96 || thumb.Bounds().Dy() != 96 {
		t.Errorf("the thumbnail is not a 96x96 JPEG: %v", err)
	}

	snapshot = mustStart(t, service, PolicyAsk)
	// Three files need converting. The two PNG pictures are already PNG and stay out of it.
	if snapshot.Batch.Done != 3 || snapshot.Batch.Total != 3 || snapshot.Batch.Failed != 0 {
		for _, item := range snapshot.Items {
			t.Logf("%s: %s %s %s", item.Name, item.Status, item.Error, item.Detail)
		}
		t.Fatalf("unexpected batch result: %+v", snapshot.Batch)
	}
	for name, want := range map[string]string{
		"IMG_0001 (edited) [final].jpg": "IMG_0001 (edited) [final].png",
		"Ünïcödé 音楽 🎵.wav":              "Ünïcödé 音楽 🎵.mp3",
		"holiday;clip & more.mov":       "holiday;clip & more.mp4",
	} {
		item := itemNamed(t, snapshot, name)
		if item.Status != StatusDone || item.OutputName != want || filepath.Dir(item.OutputPath) != destination || item.OutputSize == 0 {
			t.Errorf("%s: %+v", name, item)
		}
	}
	// The two PNG inputs were already PNG, so they are left alone.
	for _, name := range []string{"-i %03d.png", long} {
		if item := itemNamed(t, snapshot, name); item.Status != StatusSkipped {
			t.Errorf("%s should be skipped as already PNG: %+v", name, item)
		}
	}

	// Convert the PNG files to JPG and WebP too, so the awkward names are really used.
	if err := service.SetTarget(KindImage, "jpg"); err != nil {
		t.Fatal(err)
	}
	snapshot = mustStart(t, service, PolicyAsk)
	flattened := itemNamed(t, snapshot, "-i %03d.png")
	if flattened.Status != StatusDone || flattened.OutputName != "-i %03d.jpg" {
		t.Fatalf("the file with an option-like name: %+v", flattened)
	}
	picture, err := decodeStill(flattened.OutputPath, "jpg")
	if err != nil {
		t.Fatal(err)
	}
	if hidden := pixel(picture, 80, 120); hidden.R < 250 || hidden.G < 250 || hidden.B < 250 {
		t.Errorf("transparent areas should be white in a JPG, got %v", hidden)
	}
	lengthy := itemNamed(t, snapshot, long)
	if lengthy.Status != StatusDone || !strings.HasSuffix(lengthy.OutputName, ".jpg") || len(lengthy.OutputName) > 210 {
		t.Errorf("the very long name: status %s, output name of %d characters", lengthy.Status, len(lengthy.OutputName))
	}
	if item := itemNamed(t, snapshot, "IMG_0001 (edited) [final].jpg"); item.Status != StatusSkipped {
		t.Errorf("the JPG should be skipped for a JPG target: %+v", item)
	}

	// Existing results are never replaced without a decision.
	if err := service.SetTarget(KindAudio, "mp3"); err != nil {
		t.Fatal(err)
	}
	song := filepath.Join(destination, "Ünïcödé 音楽 🎵.mp3")
	songHash := fileHash(t, song)
	result, err := service.Start(PolicyAsk)
	if err != nil || result.Started || len(result.Conflicts) != 1 || result.Conflicts[0].Name != filepath.Base(song) {
		t.Fatalf("expected a question about the existing MP3: %+v %v", result, err)
	}
	snapshot = mustStart(t, service, PolicyKeep)
	if item := itemNamed(t, snapshot, "Ünïcödé 音楽 🎵.wav"); item.OutputName != "Ünïcödé 音楽 🎵 (1).mp3" {
		t.Errorf("keep both should number the new file: %q", item.OutputName)
	}
	if fileHash(t, song) != songHash {
		t.Error("the existing MP3 was changed by keep both")
	}

	for path, hash := range before {
		if fileHash(t, path) != hash {
			t.Errorf("an original was changed: %s", filepath.Base(path))
		}
	}
	for _, folder := range []string{inputs, destination} {
		if left := partials(t, folder); len(left) != 0 {
			t.Errorf("temporary files left in %s: %v", filepath.Base(folder), left)
		}
	}
	if entries, _ := os.ReadDir(inputs); len(entries) != len(names) {
		t.Errorf("the originals folder has %d entries, expected %d", len(entries), len(names))
	}
	if entries, _ := os.ReadDir(filepath.Join(dataDir, "work")); len(entries) != 0 {
		t.Errorf("%d work folders were left behind", len(entries))
	}
}

// longVideo makes a clip that takes several seconds to convert, using the engine itself.
func longVideo(t *testing.T, engine Engine, path string) {
	t.Helper()
	_, err := engine.Run(context.Background(), engine.FFmpeg, []string{
		"-hide_banner", "-nostdin", "-v", "error",
		"-filter_complex", "testsrc2=s=1280x720:r=30:d=120[v]", "-map", "[v]",
		"-c:v", "mjpeg", "-q:v", "6", "-f", "matroska", "-y", inputURL(path),
	}, nil)
	if err != nil {
		t.Fatalf("could not make the long test video: %v", err)
	}
}

func TestEngineCancelStopsQuicklyAndCleansUp(t *testing.T) {
	service, _ := realService(t)
	folder := filepath.Join(t.TempDir(), awkwardFolder)
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	clip := filepath.Join(folder, "long clip.mkv")
	longVideo(t, service.engine, clip)
	photo := filepath.Join(folder, "photo.png")
	copyFile(t, filepath.Join(fixtureFolder(t), "picture-rgb.png"), photo)
	if err := service.SetDestination(DestinationSource, ""); err != nil {
		t.Fatal(err)
	}
	for kind, id := range map[Kind]string{KindImage: "jpg", KindVideo: "mp4"} {
		if err := service.SetTarget(kind, id); err != nil {
			t.Fatal(err)
		}
	}
	addAndWait(t, service, clip, photo)

	if result, err := service.Start(PolicyAsk); err != nil || !result.Started {
		t.Fatalf("start: %v %+v", err, result)
	}
	running := waitFor(t, service, "the conversion to make progress", func(s Snapshot) bool {
		item := itemNamed(t, s, "long clip.mkv")
		return item.Status == StatusConverting && item.Progress > 1
	})
	if progress := itemNamed(t, running, "long clip.mkv").Progress; progress > 90 {
		t.Fatalf("the test clip converted too fast to cancel (%.0f%%)", progress)
	}
	if left := partials(t, folder); len(left) != 1 {
		t.Fatalf("expected one temporary file while converting, found %v", left)
	}
	started := time.Now()
	if err := service.Cancel(); err != nil {
		t.Fatal(err)
	}
	snapshot := waitFor(t, service, "the batch to stop", finished)
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("cancelling took %s", took)
	}
	for _, item := range snapshot.Items {
		if item.Status != StatusCancelled || item.OutputPath != "" {
			t.Errorf("after cancel: %+v", item)
		}
	}
	entries, _ := os.ReadDir(folder)
	if len(entries) != 2 {
		var found []string
		for _, entry := range entries {
			found = append(found, entry.Name())
		}
		t.Errorf("only the two originals should remain, found %v", found)
	}

	// After a cancel the same files can be converted again, and closing the app in the
	// middle of that also leaves nothing unfinished behind.
	if result, err := service.Start(PolicyAsk); err != nil || !result.Started {
		t.Fatalf("restart: %v %+v", err, result)
	}
	waitFor(t, service, "progress after the restart", func(s Snapshot) bool {
		return itemNamed(t, s, "long clip.mkv").Progress > 1
	})
	service.Close()
	if left := partials(t, folder); len(left) != 0 {
		t.Errorf("closing left temporary files behind: %v", left)
	}
	if _, err := os.Stat(filepath.Join(folder, "long clip.mp4")); err == nil {
		t.Error("an unfinished video was given its final name")
	}
}

// TestEngineRefusesToOverwrite proves the last lines of defence. A file that is already at
// the output name survives the engine command itself, a direct run of the plan, and Convert.
func TestEngineRefusesToOverwrite(t *testing.T) {
	engine := realEngine(t)
	ctx := context.Background()
	folder := t.TempDir()
	input := filepath.Join(folder, "photo.png")
	copyFile(t, filepath.Join(fixtureFolder(t), "picture-rgb.png"), input)
	media, err := engine.Probe(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(folder, "existing file")
	untouched := func(stage string) {
		t.Helper()
		if data, err := os.ReadFile(existing); err != nil || string(data) != "keep me" {
			t.Fatalf("%s: the existing file was changed: %q %v", stage, data, err)
		}
	}
	if err := os.WriteFile(existing, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"jpg", "png", "webp", "gif", "bmp", "tiff"} {
		target, _ := targetByID(id)
		job := Job{Media: media, Target: target, Input: input, Output: existing, WorkDir: folder}
		plans, err := buildPlans(job)
		if err != nil {
			t.Fatal(err)
		}
		// The engine reports success here even though it wrote nothing, so the result of
		// the call says little. What matters is the file.
		_, _ = engine.Run(ctx, engine.FFmpeg, plans[0].steps[0].args, nil)
		untouched(target.Label + " command")
		if _, err := engine.runPlan(ctx, job, plans[0], func(float64) {}); err == nil {
			t.Fatalf("%s: a plan that wrote nothing was accepted as a finished conversion", target.Label)
		}
		untouched(target.Label + " plan")
		if _, err := engine.Convert(ctx, job, func(float64) {}); err == nil {
			t.Fatalf("%s: Convert accepted an output name that is already in use", target.Label)
		}
		untouched(target.Label + " convert")
	}
	for _, name := range []string{"tone-16bit.wav", "hevc-aac.mov"} {
		source := filepath.Join(folder, name)
		copyFile(t, filepath.Join(fixtureFolder(t), name), source)
		media, err := engine.Probe(ctx, source)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range offered[media.Kind] {
			target, _ := targetByID(id)
			job := Job{Media: media, Target: target, Input: source, Output: existing, WorkDir: folder}
			plans, err := buildPlans(job)
			if err != nil {
				continue
			}
			for _, candidate := range plans {
				last := candidate.steps[len(candidate.steps)-1]
				_, _ = engine.Run(ctx, engine.FFmpeg, last.args, nil)
				untouched(name + " to " + target.Label)
			}
		}
	}
}
