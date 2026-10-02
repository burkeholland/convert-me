package convert

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// h264Quality is the 0 to 100 quality setting handed to the Windows H.264 encoder. Measured
// against a detailed 1080p test clip, 65 looks like a careful x264 encode (CRF 20) and makes
// a file about a quarter larger. Lower values start to show blocks in busy scenes.
const h264Quality = 65

// flattenOnWhite paints transparent areas white. Formats without transparency would
// otherwise show whatever colour happens to be stored under the transparent pixels.
// It needs no picture size, so it also works after automatic rotation.
const flattenOnWhite = "format=rgba,split[fg][base];[base]lutrgb=r=255:g=255:b=255:a=255[bg];[bg][fg]overlay=format=rgb"

// Job is one conversion: a probed input, a target format and where to write.
type Job struct {
	Media   Media
	Target  Target
	Input   string
	Output  string
	WorkDir string
}

// Outcome reports how a finished conversion was produced.
type Outcome struct {
	// Copied is true when picture or sound was carried over without re-encoding.
	Copied  bool
	Warning string
}

// step is one engine run. Weight is its share of the progress bar.
type step struct {
	args   []string
	weight float64
}

// plan is one way to produce the output. Later plans are fallbacks for earlier ones.
type plan struct {
	steps  []step
	copied bool
}

func single(args []string, copied bool) plan {
	return plan{steps: []step{{args: args, weight: 1}}, copied: copied}
}

// leading holds the options that come before the input. The protocol whitelist and the
// "file:" prefix mean a file name can never be treated as a network address or an option.
func leading(input string) []string {
	return []string{
		"-hide_banner", "-nostdin", "-v", "error",
		"-protocol_whitelist", "file,pipe",
		"-max_pixels", strconv.Itoa(maxImagePixels),
		"-i", inputURL(input),
	}
}

// trailing ends every conversion command. "-n" refuses to overwrite: the output is always
// a fresh temporary name, so an existing file there means something is wrong. Note that
// the engine exits with success when "-n" stops it, which is why runPlan also insists on
// seeing the engine report that it finished.
func trailing(output string) []string {
	return []string{"-progress", "pipe:1", "-nostats", "-n", inputURL(output)}
}

func stream(index int) string {
	return "0:" + strconv.Itoa(index)
}

func join(parts ...[]string) []string {
	var all []string
	for _, part := range parts {
		all = append(all, part...)
	}
	return all
}

// buildPlans returns the engine commands for a job, best option first.
func buildPlans(job Job) ([]plan, error) {
	media, target := job.Media, job.Target
	if !offers(media.Kind, target.ID) {
		return nil, fmt.Errorf("%s files cannot be converted to %s", media.Kind, target.Label)
	}
	switch {
	case media.Kind == KindImage:
		args, err := imageArgs(media, target)
		if err != nil {
			return nil, err
		}
		return []plan{single(join(leading(job.Input), args, trailing(job.Output)), false)}, nil
	case target.Kind == KindAudio:
		if !media.HasAudio {
			return nil, errors.New("this file has no sound to convert")
		}
		var plans []plan
		if media.Kind == KindAudio && media.CoverArt && target.ID != "wav" {
			plans = append(plans, single(join(leading(job.Input), audioArgs(media, target, true), trailing(job.Output)), false))
		}
		return append(plans, single(join(leading(job.Input), audioArgs(media, target, false), trailing(job.Output)), false)), nil
	case target.ID == "gif":
		return []plan{gifPlan(job)}, nil
	case target.Kind == KindVideo:
		return videoPlans(job), nil
	}
	return nil, fmt.Errorf("no recipe for %s", target.Label)
}

// tiffFormats are the pixel layouts every TIFF reader understands. The engine could also
// store the colour model of JPEG (YCbCr) or a palette, which many programs cannot open and
// which loses transparency. Listing these makes it pick the closest plain one instead.
const tiffFormats = "format=rgb24|rgba|gray|ya8|monob|rgb48le|rgba64le|gray16le|ya16le"

func imageArgs(media Media, target Target) ([]string, error) {
	args := []string{"-map", stream(media.videoIndex), "-frames:v", "1", "-an", "-sn", "-dn"}
	// One picture is written through the plain stream writer. The engine's numbered-image
	// writer would skip the "never overwrite" check and treat "%" in a name as a pattern.
	still := []string{"-f", "image2pipe"}
	switch target.ID {
	case "jpg":
		if media.Alpha {
			args = append(args, "-vf", flattenOnWhite+",format=rgb24")
		}
		return join(args, []string{"-c:v", "mjpeg", "-q:v", "2"}, still), nil
	case "png":
		return join(args, []string{"-c:v", "png"}, still), nil
	case "webp":
		if media.Width > 16383 || media.Height > 16383 {
			return nil, errors.New("WebP images can be at most 16383 pixels on a side")
		}
		return append(args, "-c:v", "libwebp", "-quality", "85", "-f", "webp"), nil
	case "gif":
		// Without "offsetting" the picture is stored at its full size. With it, transparent
		// borders are cut off, which simple GIF readers show at the wrong size.
		return append(args,
			"-vf", "split[a][b];[a]palettegen=reserve_transparent=1[p];[b][p]paletteuse=alpha_threshold=128",
			"-c:v", "gif", "-gifflags", "-offsetting", "-f", "gif"), nil
	case "bmp":
		if media.Alpha {
			args = append(args, "-vf", flattenOnWhite+",format=rgb24")
		}
		return join(args, []string{"-c:v", "bmp", "-pix_fmt", "bgr24"}, still), nil
	case "tiff":
		layout := tiffFormats
		if strings.HasPrefix(media.PixFmt, "pal") {
			// A palette may hold transparent colours, and the automatic choice would drop them.
			layout = "format=rgba"
		}
		return join(args, []string{"-vf", layout, "-c:v", "tiff", "-compression_algo", "lzw"}, still), nil
	}
	return nil, fmt.Errorf("no image recipe for %s", target.Label)
}

func aacBitrate(channels int) string {
	switch {
	case channels <= 1:
		return "128k"
	case channels == 2:
		return "192k"
	default:
		return strconv.Itoa(min(512, 64*channels)) + "k"
	}
}

func audioArgs(media Media, target Target, cover bool) []string {
	args := []string{"-map", stream(media.audioIndex)}
	if cover {
		args = append(args, "-map", stream(media.coverIndex), "-c:v", "copy", "-disposition:v:0", "attached_pic")
	} else {
		args = append(args, "-vn")
	}
	args = append(args, "-sn", "-dn")
	// Studio-quality sources keep their extra precision. Everything else is 16-bit, which
	// is all that MP3, AAC and CD-quality files contain.
	highResolution := media.AudioBits > 16 || media.FloatAudio
	switch target.ID {
	case "mp3":
		return append(args, "-c:a", "libmp3lame", "-q:a", "2", "-id3v2_version", "3", "-f", "mp3")
	case "m4a":
		return append(args, "-c:a", "aac", "-b:a", aacBitrate(media.Channels), "-movflags", "+faststart", "-f", "ipod")
	case "wav":
		codec := "pcm_s16le"
		if highResolution {
			codec = "pcm_s24le"
		}
		return append(args, "-c:a", codec, "-f", "wav")
	default:
		sampleFormat := "s16"
		if highResolution {
			sampleFormat = "s32"
		}
		return append(args, "-c:a", "flac", "-sample_fmt", sampleFormat, "-f", "flac")
	}
}

// Limits of H.264 output. The Windows encoder refuses a picture with a side of 32 pixels or
// less, and anything above 4096 x 2304 (H.264 level 5.2) does not play on most devices.
const (
	h264MinSide   = 64
	h264MaxSide   = 4096
	h264MaxPixels = 4096 * 2304
)

// h264Size returns the picture size to encode for a source that is shown at width x height.
// Both results are even, because H.264 with normal colour sampling needs even sizes.
func h264Size(width, height int) (int, int) {
	if width <= 0 || height <= 0 {
		return width, height
	}
	w, h := float64(width), float64(height)
	if scale := math.Min(h264MaxSide/math.Max(w, h), math.Sqrt(h264MaxPixels/(w*h))); scale < 1 {
		// The small allowance keeps an exact fit, such as 8K to 4096 x 2304, from losing
		// two pixels to rounding in the arithmetic.
		fit := func(side float64) int { return max(2, int(side*scale+1e-6)/2*2) }
		return fit(w), fit(h)
	}
	if shorter := min(width, height); shorter < h264MinSide {
		// Tiny pictures, usually small GIFs, are enlarged by a whole number so pixels stay crisp.
		factor := (h264MinSide + shorter - 1) / shorter
		if max(width, height)*factor <= h264MaxSide {
			width, height = width*factor, height*factor
		}
	}
	return width - width%2, height - height%2
}

// shrinksForH264 reports whether a video is too large to keep its size as H.264.
func shrinksForH264(media Media) bool {
	width, height := h264Size(media.Width, media.Height)
	return width < media.Width-1 || height < media.Height-1
}

// videoFilters lists the picture filters needed before the Windows H.264 encoder.
func videoFilters(media Media) []string {
	var filters []string
	if media.Interlaced {
		filters = append(filters, "bwdif=mode=send_frame")
	}
	if media.Alpha {
		filters = append(filters, flattenOnWhite)
	}
	width, height := h264Size(media.Width, media.Height)
	switch {
	case width == media.Width && height == media.Height:
	case width > media.Width || height > media.Height:
		filters = append(filters, fmt.Sprintf("scale=%d:%d:flags=neighbor", width, height))
	case media.Width-width <= 1 && media.Height-height <= 1:
		// Dropping one row or column avoids resampling the whole picture.
		filters = append(filters, fmt.Sprintf("crop=%d:%d:0:0", width, height))
	default:
		filters = append(filters, fmt.Sprintf("scale=%d:%d", width, height))
	}
	return filters
}

func containerArgs(target Target) []string {
	switch target.ID {
	case "mp4":
		return []string{"-movflags", "+faststart", "-f", "mp4"}
	case "mov":
		return []string{"-movflags", "+faststart", "-f", "mov"}
	default:
		return []string{"-f", "matroska"}
	}
}

func videoArgs(media Media, target Target, copyVideo, copyAudio bool) []string {
	args := []string{"-map", stream(media.videoIndex)}
	if media.HasAudio {
		args = append(args, "-map", stream(media.audioIndex))
	}
	args = append(args, "-sn", "-dn")
	if copyVideo {
		args = append(args, "-c:v", "copy")
	} else {
		if filters := videoFilters(media); len(filters) > 0 {
			args = append(args, "-vf", strings.Join(filters, ","))
		}
		keyframes := 60
		if media.FrameRate > 0 {
			keyframes = int(math.Round(math.Min(300, math.Max(12, media.FrameRate*2))))
		}
		args = append(args, "-c:v", "h264_mf", "-rate_control", "quality", "-quality", strconv.Itoa(h264Quality),
			"-g", strconv.Itoa(keyframes))
	}
	switch {
	case !media.HasAudio:
		args = append(args, "-an")
	case copyAudio:
		args = append(args, "-c:a", "copy")
	default:
		args = append(args, "-c:a", "aac", "-b:a", aacBitrate(media.Channels))
	}
	args = append(args, "-max_muxing_queue_size", "4096")
	return append(args, containerArgs(target)...)
}

// copyable reports which streams can be carried into the target untouched. Copying is
// instant and loses no quality, so it is preferred whenever the stream is already what the
// target would get anyway.
func copyable(media Media, target Target) (video, audio bool) {
	wellFormed := media.Format == "mov" || media.Format == "matroska"
	// MP4 and MOV remember a rotation, so a sideways phone video stays correct when copied.
	// MKV players often ignore it, so there the picture is turned for real by re-encoding.
	keepsRotation := target.ID == "mp4" || target.ID == "mov"
	video = wellFormed && media.VideoCodec == "h264" && !media.Animated && !media.Interlaced &&
		(media.Rotation == 0 || keepsRotation) && (media.PixFmt == "yuv420p" || media.PixFmt == "yuvj420p")
	audio = wellFormed && media.HasAudio && media.AudioCodec == "aac" &&
		(media.AudioProfile == "LC" || media.AudioProfile == "")
	return video, audio
}

// alreadyIn reports whether a file already is what the target would produce. Converting it
// would only make a second, slightly worse copy.
func alreadyIn(media Media, target Target) bool {
	switch media.Kind {
	case KindImage:
		return imageFormats[media.Format] == target.Label
	case KindAudio:
		if target.ID == "m4a" {
			return media.Format == "mov" && media.AudioCodec == "aac"
		}
		return media.Format == target.ID
	case KindVideo:
		if media.Animated {
			return target.ID == "gif"
		}
		if target.Kind != KindVideo || target.ID == "gif" {
			return false
		}
		sameContainer := media.sourceExt == target.Ext || (media.sourceExt == ".m4v" && target.ID == "mp4")
		video, audio := copyable(media, target)
		return sameContainer && video && (audio || !media.HasAudio)
	}
	return false
}

// videoPlans prefers copying streams that are already H.264 or AAC. If a copy fails for
// any reason the next plan re-encodes.
func videoPlans(job Job) []plan {
	media := job.Media
	copyVideo, copyAudio := copyable(media, job.Target)
	build := func(video, audio bool) plan {
		return single(join(leading(job.Input), videoArgs(media, job.Target, video, audio), trailing(job.Output)), video || audio)
	}
	var plans []plan
	if copyVideo {
		plans = append(plans, build(true, copyAudio))
	}
	if copyAudio {
		plans = append(plans, build(false, true))
	}
	return append(plans, build(false, false))
}

// gifPlan makes an animated GIF in two runs: the first works out the best 256 colours,
// the second uses them. Doing both in one run would hold every frame in memory.
func gifPlan(job Job) plan {
	media := job.Media
	var pre []string
	if !media.Animated && (media.FrameRate == 0 || media.FrameRate > 12) {
		pre = append(pre, "fps=12")
	}
	if media.Width > 480 {
		pre = append(pre, "scale=480:-1:flags=lanczos")
	}
	chain := "null"
	if len(pre) > 0 {
		chain = strings.Join(pre, ",")
	}
	palette := filepath.Join(job.WorkDir, "palette.png")
	first := join(leading(job.Input), []string{
		"-map", stream(media.videoIndex), "-an", "-sn", "-dn",
		"-vf", chain + ",palettegen=stats_mode=diff",
		"-frames:v", "1", "-c:v", "png", "-f", "image2pipe",
	}, trailing(palette))
	second := join(leading(job.Input), []string{
		"-protocol_whitelist", "file,pipe", "-i", inputURL(palette),
		"-filter_complex", fmt.Sprintf("[%s]%s[frames];[frames][1:v]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle[out]",
			stream(media.videoIndex), chain),
		"-map", "[out]", "-an", "-sn", "-dn", "-c:v", "gif", "-loop", "0", "-f", "gif",
	}, trailing(job.Output))
	return plan{steps: []step{{args: first, weight: 0.3}, {args: second, weight: 0.7}}}
}

// thumbnailArgs asks for one small JPEG on standard output. Nothing is written to disk.
func thumbnailArgs(input string, media Media) []string {
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-protocol_whitelist", "file,pipe",
		"-max_pixels", strconv.Itoa(maxImagePixels)}
	if media.Kind == KindVideo && !media.Animated && media.DurationMs > 2000 {
		// A tenth of the way in usually clears title cards and fades from black.
		seconds := math.Min(float64(media.DurationMs)/1000*0.1, 10)
		args = append(args, "-ss", strconv.FormatFloat(seconds, 'f', 3, 64))
	}
	return append(args, "-i", inputURL(input), "-map", stream(media.videoIndex),
		"-frames:v", "1", "-an", "-sn", "-dn",
		"-vf", "scale=w=96:h=96:force_original_aspect_ratio=increase:flags=bilinear,crop=96:96,"+flattenOnWhite,
		"-c:v", "mjpeg", "-q:v", "4", "-f", "image2pipe", "pipe:1")
}

// h264SelfTestArgs encodes half a second of a blank picture with the Windows H.264
// encoder and throws the result away. It answers "does that encoder exist and start?".
func h264SelfTestArgs() []string {
	return []string{"-hide_banner", "-nostdin", "-v", "error",
		"-filter_complex", "color=c=black:s=256x144:r=30:d=0.5[v]", "-map", "[v]",
		"-c:v", "h264_mf", "-rate_control", "quality", "-quality", strconv.Itoa(h264Quality),
		"-f", "null", "-"}
}

func progressFromLine(line string, durationMs int64) (float64, bool) {
	value, ok := strings.CutPrefix(line, "out_time_us=")
	if !ok || durationMs <= 0 {
		return 0, false
	}
	microseconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || microseconds < 0 {
		return 0, false
	}
	return math.Min(100, microseconds/1000/float64(durationMs)*100), true
}

// Convert runs the plans for a job until one produces a valid file at job.Output.
// On failure or cancellation nothing is left at job.Output. A file that is already at
// job.Output is never touched: the conversion is refused instead.
func (e Engine) Convert(ctx context.Context, job Job, progress func(float64)) (Outcome, error) {
	plans, err := buildPlans(job)
	if err != nil {
		return Outcome{}, err
	}
	if fileExists(job.Output) {
		return Outcome{}, errors.New("the temporary file name is already in use")
	}
	var last error
	for _, candidate := range plans {
		outcome, err := e.runPlan(ctx, job, candidate, progress)
		if err == nil {
			return outcome, nil
		}
		// Whatever is there now was written by the plan that just failed.
		_ = os.Remove(job.Output)
		if ctx.Err() != nil {
			return Outcome{}, ctx.Err()
		}
		last = err
	}
	return Outcome{}, last
}

func (e Engine) runPlan(ctx context.Context, job Job, candidate plan, progress func(float64)) (Outcome, error) {
	base := 0.0
	for _, current := range candidate.steps {
		start, weight := base, current.weight
		finished := false
		_, err := e.Run(ctx, e.FFmpeg, current.args, func(line string) {
			if line == "progress=end" {
				finished = true
			}
			if percent, ok := progressFromLine(line, job.Media.DurationMs); ok {
				progress(start + percent*weight)
			}
		})
		if err != nil {
			return Outcome{}, err
		}
		if !finished {
			// The engine stopped early without an error code, as it does when the output
			// name is taken. Nothing it left behind can be trusted.
			return Outcome{}, errors.New("the engine stopped before it finished writing")
		}
		base += 100 * weight
		progress(base)
	}
	return e.verify(ctx, job, candidate)
}

// verify checks that the engine really produced what was asked for before the file is
// given its final name.
func (e Engine) verify(ctx context.Context, job Job, candidate plan) (Outcome, error) {
	outcome := Outcome{Copied: candidate.copied}
	info, err := os.Stat(job.Output)
	if err != nil || info.Size() == 0 {
		return outcome, errors.New("the engine finished without writing a file")
	}
	result, err := e.read(ctx, job.Output)
	if err != nil {
		return outcome, fmt.Errorf("the converted file could not be read back: %w", err)
	}
	switch job.Target.Kind {
	case KindAudio:
		if !result.HasAudio {
			return outcome, errors.New("the converted file has no sound")
		}
	default:
		if result.Kind == KindAudio {
			return outcome, errors.New("the converted file has no picture")
		}
	}
	if job.Media.DurationMs > 5000 && job.Media.Kind != KindImage && !(job.Target.ID == "gif") &&
		result.DurationMs > 0 && float64(result.DurationMs) < float64(job.Media.DurationMs)*0.9 {
		outcome.Warning = "The converted file is shorter than the original. The original may be damaged."
	}
	return outcome, nil
}
