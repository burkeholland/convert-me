package convert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Limits that keep one odd file from exhausting memory.
const (
	maxImageSide   = 32767
	maxImagePixels = 200_000_000
	probeTimeout   = 30 * time.Second
)

// Engine runs the bundled tools. The paths are fixed at startup and never come from PATH.
type Engine struct {
	FFmpeg  string
	FFprobe string
	Run     Runner
}

type probeStream struct {
	Index            int    `json:"index"`
	CodecType        string `json:"codec_type"`
	CodecName        string `json:"codec_name"`
	Profile          string `json:"profile"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	PixFmt           string `json:"pix_fmt"`
	FieldOrder       string `json:"field_order"`
	ColorTransfer    string `json:"color_transfer"`
	AvgFrameRate     string `json:"avg_frame_rate"`
	RFrameRate       string `json:"r_frame_rate"`
	NbFrames         string `json:"nb_frames"`
	Duration         string `json:"duration"`
	SampleRate       string `json:"sample_rate"`
	Channels         int    `json:"channels"`
	BitsPerSample    int    `json:"bits_per_sample"`
	BitsPerRawSample string `json:"bits_per_raw_sample"`
	Disposition      struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
	SideData []struct {
		Rotation *float64 `json:"rotation"`
	} `json:"side_data_list"`
}

type probeResult struct {
	Streams []probeStream `json:"streams"`
	Format  struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		ProbeScore int    `json:"probe_score"`
	} `json:"format"`
}

// minProbeScore is how sure the engine has to be about what a file is. Real files score 5
// or more. Below that the engine found next to nothing and is going by the file name.
const minProbeScore = 5

// inputURL forces the plain file protocol, so no part of a file name can be read as
// another protocol or as an option.
func inputURL(path string) string {
	return "file:" + path
}

// animatedWebP reports whether a file is an animated WebP picture. The engine cannot read
// those yet, and saying so beats a vague "could not read this file".
func animatedWebP(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	header := make([]byte, 21)
	if _, err := io.ReadFull(file, header); err != nil {
		return false
	}
	const animationFlag = 0x02
	return string(header[0:4]) == "RIFF" && string(header[8:12]) == "WEBP" &&
		string(header[12:16]) == "VP8X" && header[20]&animationFlag != 0
}

// Probe asks the engine what a file really contains. The extension is not trusted.
func (e Engine) Probe(ctx context.Context, path string) (Media, error) {
	if animatedWebP(path) {
		return Media{}, errors.New("animated WebP pictures cannot be read yet")
	}
	media, err := e.read(ctx, path)
	if err == nil && media.Kind == KindImage {
		// A photo can be stored sideways with a note that says how to turn it. The engine
		// only reports that note once it has opened the picture itself.
		rotationCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		defer cancel()
		if rotation := e.stillRotation(rotationCtx, path); rotation == 90 || rotation == 270 {
			media.Rotation = rotation
			media.Width, media.Height = media.Height, media.Width
		}
	}
	return media, err
}

// read is the quick part of Probe: containers, codecs and sizes as stored in the file.
func (e Engine) read(ctx context.Context, path string) (Media, error) {
	var media Media
	if !filepath.IsAbs(path) {
		return media, errors.New("the file path must be absolute")
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	data, err := e.Run(probeCtx, e.FFprobe, []string{
		"-v", "error", "-protocol_whitelist", "file,pipe",
		"-show_format", "-show_streams", "-of", "json",
		"-i", inputURL(path),
	}, nil)
	if err != nil {
		if ctx.Err() != nil {
			return media, ctx.Err()
		}
		if probeCtx.Err() != nil {
			return media, errors.New("reading this file took too long")
		}
		return media, err
	}
	var result probeResult
	if err := json.Unmarshal(data, &result); err != nil {
		return media, fmt.Errorf("the engine returned unreadable file information: %w", err)
	}
	media, err = classify(result)
	media.sourceExt = strings.ToLower(filepath.Ext(path))
	return media, err
}

// stillRotation asks the engine how a picture is turned when it is shown. Zero also means
// "could not tell", which is harmless: converting applies the rotation either way.
func (e Engine) stillRotation(ctx context.Context, path string) int {
	data, err := e.Run(ctx, e.FFprobe, []string{
		"-v", "error", "-protocol_whitelist", "file,pipe", "-read_intervals", "%+#1",
		"-show_entries", "frame_side_data=rotation", "-of", "json",
		"-i", inputURL(path),
	}, nil)
	if err != nil {
		return 0
	}
	var result struct {
		Frames []struct {
			SideData []struct {
				Rotation *float64 `json:"rotation"`
			} `json:"side_data_list"`
		} `json:"frames"`
	}
	if json.Unmarshal(data, &result) != nil {
		return 0
	}
	for _, frame := range result.Frames {
		for _, side := range frame.SideData {
			if side.Rotation != nil {
				return normalizeRotation(*side.Rotation)
			}
		}
	}
	return 0
}

// classify turns the engine's report into the facts the app needs.
func classify(result probeResult) (Media, error) {
	media := Media{Format: strings.Split(result.Format.FormatName, ",")[0]}
	if result.Format.ProbeScore > 0 && result.Format.ProbeScore < minProbeScore {
		return media, errors.New("this does not look like an image, audio or video file")
	}
	var video, audio *probeStream
	for i := range result.Streams {
		stream := &result.Streams[i]
		switch stream.CodecType {
		case "video":
			if stream.Disposition.AttachedPic == 1 {
				if !media.CoverArt {
					media.CoverArt = true
					media.coverIndex = stream.Index
				}
				continue
			}
			if video == nil {
				video = stream
			}
		case "audio":
			if audio == nil {
				audio = stream
			}
		}
	}
	if video == nil && audio == nil {
		return media, errors.New("no picture or sound was found in this file")
	}
	if seconds, err := strconv.ParseFloat(result.Format.Duration, 64); err == nil &&
		!math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds > 0 {
		media.DurationMs = int64(math.Round(seconds * 1000))
	}
	if audio != nil {
		media.HasAudio = true
		media.AudioCodec = audio.CodecName
		media.AudioProfile = audio.Profile
		media.Channels = audio.Channels
		media.SampleRate, _ = strconv.Atoi(audio.SampleRate)
		media.AudioBits = audio.BitsPerSample
		if raw, err := strconv.Atoi(audio.BitsPerRawSample); err == nil && raw > media.AudioBits {
			media.AudioBits = raw
		}
		// Only sound stored as floating point counts as "more than 16 bits". Decoders for MP3
		// and AAC also work in floating point, but those files hold no extra precision.
		media.FloatAudio = strings.HasPrefix(audio.CodecName, "pcm_f")
		media.audioIndex = audio.Index
	}
	if video == nil {
		media.Kind = KindAudio
		return media, nil
	}

	media.videoIndex = video.Index
	media.VideoCodec = video.CodecName
	media.PixFmt = video.PixFmt
	media.Alpha = hasAlpha(video.PixFmt)
	media.Width, media.Height = video.Width, video.Height
	for _, side := range video.SideData {
		if side.Rotation != nil {
			media.Rotation = normalizeRotation(*side.Rotation)
		}
	}
	if media.Rotation == 90 || media.Rotation == 270 {
		media.Width, media.Height = media.Height, media.Width
	}
	switch video.FieldOrder {
	case "tt", "bb", "tb", "bt":
		media.Interlaced = true
	}
	media.HDR = video.ColorTransfer == "smpte2084" || video.ColorTransfer == "arib-std-b67"
	media.FrameRate = frameRate(video.AvgFrameRate)
	if media.FrameRate == 0 {
		media.FrameRate = frameRate(video.RFrameRate)
	}
	if media.Width <= 0 || media.Height <= 0 {
		return media, errors.New("the picture size could not be read")
	}

	if _, still := imageFormats[media.Format]; still {
		frames, _ := strconv.Atoi(video.NbFrames)
		if media.Format == "gif" && frames > 1 {
			// An animated GIF behaves like a silent video.
			media.Kind = KindVideo
			media.Animated = true
			media.HasAudio = false
			return media, nil
		}
		if media.Width > maxImageSide || media.Height > maxImageSide ||
			int64(media.Width)*int64(media.Height) > maxImagePixels {
			return media, fmt.Errorf("this image is %d x %d pixels, which is larger than Convert Me can handle", media.Width, media.Height)
		}
		media.Kind = KindImage
		media.DurationMs = 0
		media.HasAudio = false
		return media, nil
	}
	media.Kind = KindVideo
	return media, nil
}

// hasAlpha reports whether a pixel format can carry transparency. Palette formats are
// included because a palette entry may be transparent.
func hasAlpha(pixFmt string) bool {
	for _, prefix := range []string{"rgba", "bgra", "argb", "abgr", "ya8", "ya16", "yuva", "gbrap", "pal8"} {
		if strings.HasPrefix(pixFmt, prefix) {
			return true
		}
	}
	return false
}

// normalizeRotation converts the engine's display rotation to 0, 90, 180 or 270.
func normalizeRotation(value float64) int {
	rotation := int(math.Round(value/90)) * 90 % 360
	if rotation < 0 {
		rotation += 360
	}
	return rotation
}

func frameRate(value string) float64 {
	numerator, denominator, ok := strings.Cut(value, "/")
	if !ok {
		rate, _ := strconv.ParseFloat(value, 64)
		return rate
	}
	top, errTop := strconv.ParseFloat(numerator, 64)
	bottom, errBottom := strconv.ParseFloat(denominator, 64)
	if errTop != nil || errBottom != nil || bottom == 0 {
		return 0
	}
	rate := top / bottom
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
		return 0
	}
	return rate
}

// checkInput validates a path before it is handed to the engine.
func checkInput(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("the path is not absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, errors.New("the file could not be opened")
	}
	if info.IsDir() {
		return nil, errors.New("folders are not supported yet, add the files inside it")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("this is not a regular file")
	}
	if info.Size() == 0 {
		return nil, errors.New("the file is empty")
	}
	return info, nil
}
