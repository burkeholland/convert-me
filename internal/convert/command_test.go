package convert

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func classifyJSON(t *testing.T, text string) (Media, error) {
	t.Helper()
	var result probeResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("bad test JSON: %v", err)
	}
	return classify(result)
}

func TestClassifyReadsWhatMatters(t *testing.T) {
	phone := `{"streams":[
		{"index":0,"codec_type":"video","codec_name":"hevc","width":1920,"height":1080,"pix_fmt":"yuv420p10le","color_transfer":"arib-std-b67","avg_frame_rate":"30000/1001","field_order":"progressive","side_data_list":[{"side_data_type":"Display Matrix","rotation":-90}]},
		{"index":1,"codec_type":"audio","codec_name":"aac","profile":"LC","sample_rate":"44100","channels":2},
		{"index":2,"codec_type":"data","codec_name":"bin_data"}],
		"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"12.345","probe_score":100}}`
	media, err := classifyJSON(t, phone)
	if err != nil {
		t.Fatal(err)
	}
	if media.Kind != KindVideo || media.Format != "mov" || media.Width != 1080 || media.Height != 1920 || media.Rotation != 270 {
		t.Errorf("a sideways phone video should be listed upright: %+v", media)
	}
	if !media.HDR || media.Interlaced || !media.HasAudio || media.AudioCodec != "aac" || media.DurationMs != 12345 {
		t.Errorf("unexpected facts: %+v", media)
	}
	if media.FrameRate < 29.9 || media.FrameRate > 30 || media.videoIndex != 0 || media.audioIndex != 1 {
		t.Errorf("frame rate %v, streams %d and %d", media.FrameRate, media.videoIndex, media.audioIndex)
	}

	song := `{"streams":[
		{"index":0,"codec_type":"audio","codec_name":"flac","sample_rate":"96000","channels":2,"bits_per_raw_sample":"24"},
		{"index":1,"codec_type":"video","codec_name":"mjpeg","width":600,"height":600,"pix_fmt":"yuvj420p","disposition":{"attached_pic":1}}],
		"format":{"format_name":"flac","duration":"200.5"}}`
	media, err = classifyJSON(t, song)
	if err != nil {
		t.Fatal(err)
	}
	if media.Kind != KindAudio || !media.CoverArt || media.coverIndex != 1 || media.AudioBits != 24 || media.SampleRate != 96000 {
		t.Errorf("album art must not turn a song into a video: %+v", media)
	}

	tape := `{"streams":[{"index":0,"codec_type":"video","codec_name":"mpeg2video","width":720,"height":576,"pix_fmt":"yuv420p","field_order":"tt","r_frame_rate":"25/1","avg_frame_rate":"0/0"}],"format":{"format_name":"mpeg","duration":"60","probe_score":26}}`
	media, err = classifyJSON(t, tape)
	if err != nil {
		t.Fatal(err)
	}
	if !media.Interlaced || media.FrameRate != 25 || media.HasAudio {
		t.Errorf("unexpected facts: %+v", media)
	}
}

func TestClassifyTellsStillsFromAnimations(t *testing.T) {
	still := `{"streams":[{"index":0,"codec_type":"video","codec_name":"gif","width":320,"height":240,"pix_fmt":"bgra","nb_frames":"1"}],"format":{"format_name":"gif"}}`
	moving := `{"streams":[{"index":0,"codec_type":"video","codec_name":"gif","width":320,"height":240,"pix_fmt":"bgra","nb_frames":"12"}],"format":{"format_name":"gif","duration":"1.2"}}`
	media, _ := classifyJSON(t, still)
	if media.Kind != KindImage || media.Animated || !media.Alpha {
		t.Errorf("a one-frame GIF is a picture: %+v", media)
	}
	media, _ = classifyJSON(t, moving)
	if media.Kind != KindVideo || !media.Animated || media.HasAudio || media.DurationMs != 1200 {
		t.Errorf("an animated GIF is a silent video: %+v", media)
	}
	photo := `{"streams":[{"index":0,"codec_type":"video","codec_name":"mjpeg","width":4032,"height":3024,"pix_fmt":"yuvj420p"}],"format":{"format_name":"jpeg_pipe","duration":"0.04","probe_score":26}}`
	media, _ = classifyJSON(t, photo)
	if media.Kind != KindImage || media.DurationMs != 0 || media.Alpha {
		t.Errorf("a photo has no duration: %+v", media)
	}
}

func TestClassifyRefusesWhatItCannotUse(t *testing.T) {
	cases := map[string]string{
		`{"streams":[],"format":{"format_name":"mov"}}`:                                                                                                        "no picture or sound",
		`{"streams":[{"index":0,"codec_type":"subtitle","codec_name":"subrip"}],"format":{"format_name":"matroska"}}`:                                          "no picture or sound",
		`{"streams":[{"index":0,"codec_type":"audio","codec_name":"mp3"}],"format":{"format_name":"mp3","probe_score":1}}`:                                     "does not look like",
		`{"streams":[{"index":0,"codec_type":"video","codec_name":"webp","width":0,"height":0}],"format":{"format_name":"webp_pipe"}}`:                         "picture size",
		`{"streams":[{"index":0,"codec_type":"video","codec_name":"png","width":40000,"height":10,"pix_fmt":"rgb24"}],"format":{"format_name":"png_pipe"}}`:    "larger than Convert Me can handle",
		`{"streams":[{"index":0,"codec_type":"video","codec_name":"png","width":20000,"height":20000,"pix_fmt":"rgb24"}],"format":{"format_name":"png_pipe"}}`: "larger than Convert Me can handle",
	}
	for text, want := range cases {
		if _, err := classifyJSON(t, text); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("expected an error about %q, got %v for %s", want, err, text)
		}
	}
	weak := `{"streams":[{"index":0,"codec_type":"video","codec_name":"mjpeg","width":10,"height":10,"pix_fmt":"yuvj420p"}],"format":{"format_name":"jpeg_pipe","probe_score":7}}`
	if _, err := classifyJSON(t, weak); err != nil {
		t.Errorf("a photo with a large header scores low but is real: %v", err)
	}
}

func TestSmallHelpers(t *testing.T) {
	for value, want := range map[float64]int{0: 0, 90: 90, -90: 270, 180: 180, -180: 180, 270: 270, 360: 0, 89.6: 90, -270: 90} {
		if got := normalizeRotation(value); got != want {
			t.Errorf("rotation %v normalised to %d, want %d", value, got, want)
		}
	}
	for value, want := range map[string]float64{"30/1": 30, "25": 25, "0/0": 0, "abc": 0, "1/0": 0, "-5/1": 0, "": 0} {
		if got := frameRate(value); got != want {
			t.Errorf("frame rate %q read as %v, want %v", value, got, want)
		}
	}
	for format, want := range map[string]bool{"rgba": true, "yuva420p": true, "pal8": true, "ya8": true, "gbrap": true, "rgb24": false, "yuv420p": false, "gray": false, "": false} {
		if hasAlpha(format) != want {
			t.Errorf("transparency of %q should be %v", format, want)
		}
	}
	for line, want := range map[string]float64{"out_time_us=5000000": 50, "out_time_us=20000000": 100, "out_time_us=0": 0} {
		if got, ok := progressFromLine(line, 10000); !ok || got != want {
			t.Errorf("%s gave %v %v, want %v", line, got, ok, want)
		}
	}
	for _, line := range []string{"out_time_us=N/A", "out_time_us=-5", "frame=12", "progress=end"} {
		if _, ok := progressFromLine(line, 10000); ok {
			t.Errorf("%q is not usable progress", line)
		}
	}
	if _, ok := progressFromLine("out_time_us=5000000", 0); ok {
		t.Error("a picture has no duration, so it has no time-based progress")
	}
	if aacBitrate(1) != "128k" || aacBitrate(2) != "192k" || aacBitrate(6) != "384k" || aacBitrate(16) != "512k" {
		t.Error("AAC bit rates are off")
	}
}

func TestAnimatedWebPIsRecognisedFromItsHeader(t *testing.T) {
	folder := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(folder, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	header := func(flags byte) []byte {
		return append([]byte("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00"), flags, 0, 0, 0)
	}
	if !animatedWebP(write("moving.webp", header(0x02))) || !animatedWebP(write("moving-alpha.webp", header(0x12))) {
		t.Error("the animation flag was not seen")
	}
	if animatedWebP(write("still.webp", header(0x10))) {
		t.Error("a still picture with transparency is not an animation")
	}
	if animatedWebP(write("simple.webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 \x0a\x00\x00\x00\x02\x00\x00\x00"))) {
		t.Error("a simple WebP has no flags at all")
	}
	if animatedWebP(write("short.webp", []byte("RIFF"))) || animatedWebP(filepath.Join(folder, "missing.webp")) {
		t.Error("short or missing files are simply not animations")
	}
	engine := Engine{Run: newFake().run, FFprobe: "ffprobe.exe"}
	if _, err := engine.Probe(t.Context(), filepath.Join(folder, "moving.webp")); err == nil || !strings.Contains(err.Error(), "animated WebP") {
		t.Errorf("expected a clear refusal, got %v", err)
	}
	if _, err := engine.Probe(t.Context(), "relative.png"); err == nil {
		t.Error("relative paths must be refused")
	}
}

func plansFor(t *testing.T, media Media, id string) []plan {
	t.Helper()
	target, ok := targetByID(id)
	if !ok {
		t.Fatalf("unknown target %s", id)
	}
	plans, err := buildPlans(Job{Media: media, Target: target, Input: `C:\in\-source %d.file`, Output: `C:\out\result.` + id + `.0a1b2c3d` + partialSuffix, WorkDir: `C:\work\job`})
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return plans
}

func hasPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

// TestEveryCommandIsSafe checks the rules that keep a file name from ever being treated as
// an option, a pattern or a network address, for every kind of source and every target.
func TestEveryCommandIsSafe(t *testing.T) {
	sources := map[string]Media{
		"image": {Kind: KindImage, Format: "png_pipe", Width: 640, Height: 480, PixFmt: "rgba", Alpha: true},
		"audio": {Kind: KindAudio, Format: "wav", HasAudio: true, AudioCodec: "pcm_s16le", Channels: 2, DurationMs: 9000, CoverArt: true, coverIndex: 1},
		"video": {Kind: KindVideo, Format: "mov", Width: 1920, Height: 1080, PixFmt: "yuv420p", VideoCodec: "h264", HasAudio: true,
			AudioCodec: "aac", AudioProfile: "LC", Channels: 2, DurationMs: 9000, FrameRate: 30, audioIndex: 1},
	}
	muxers := map[string]bool{"image2pipe": true, "webp": true, "gif": true, "mp3": true, "ipod": true, "wav": true, "flac": true, "mp4": true, "mov": true, "matroska": true}
	checked := 0
	for name, media := range sources {
		for _, id := range offered[media.Kind] {
			for _, candidate := range plansFor(t, media, id) {
				total := 0.0
				for _, current := range candidate.steps {
					args := current.args
					total += current.weight
					joined := strings.Join(args, " ")
					last := args[len(args)-1]
					switch {
					case !strings.HasPrefix(last, "file:"):
						t.Errorf("%s to %s: the output is not a plain file address: %s", name, id, last)
					case args[len(args)-2] != "-n":
						t.Errorf("%s to %s: the command may overwrite: %s", name, id, joined)
					case !hasPair(args, "-i", `file:C:\in\-source %d.file`):
						t.Errorf("%s to %s: the input is not a plain file address: %s", name, id, joined)
					case !hasPair(args, "-protocol_whitelist", "file,pipe") || !hasPair(args, "-progress", "pipe:1"):
						t.Errorf("%s to %s: missing protocol limit or progress: %s", name, id, joined)
					case args[0] != "-hide_banner" || args[1] != "-nostdin":
						t.Errorf("%s to %s: the engine could wait for keyboard input: %s", name, id, joined)
					}
					format := argAfter(args, "-f")
					if !muxers[format] {
						t.Errorf("%s to %s: unexpected output format %q", name, id, format)
					}
					for _, arg := range args {
						if arg == "-y" || arg == "image2" || arg == "-update" {
							t.Errorf("%s to %s: %q must not be used: %s", name, id, arg, joined)
						}
					}
					for i, arg := range args {
						if strings.Contains(arg, `C:\in`) && (i == 0 || args[i-1] != "-i") {
							t.Errorf("%s to %s: the input path appears outside -i: %s", name, id, arg)
						}
					}
					checked++
				}
				if total < 0.999 || total > 1.001 {
					t.Errorf("%s to %s: step weights add up to %v", name, id, total)
				}
			}
		}
	}
	if checked < 20 {
		t.Fatalf("only %d commands were checked", checked)
	}
}

func TestImageRecipes(t *testing.T) {
	solid := Media{Kind: KindImage, Format: "png_pipe", Width: 640, Height: 480, PixFmt: "rgb24"}
	clear := Media{Kind: KindImage, Format: "png_pipe", Width: 640, Height: 480, PixFmt: "rgba", Alpha: true}
	for _, id := range []string{"jpg", "bmp"} {
		if joined := strings.Join(plansFor(t, clear, id)[0].steps[0].args, " "); !strings.Contains(joined, "lutrgb=r=255:g=255:b=255") {
			t.Errorf("%s has no transparency, so it must be painted white: %s", id, joined)
		}
		if joined := strings.Join(plansFor(t, solid, id)[0].steps[0].args, " "); strings.Contains(joined, "lutrgb") {
			t.Errorf("%s from a solid picture needs no white background: %s", id, joined)
		}
	}
	for _, id := range []string{"webp", "tiff", "gif"} {
		if joined := strings.Join(plansFor(t, clear, id)[0].steps[0].args, " "); strings.Contains(joined, "lutrgb") {
			t.Errorf("%s keeps transparency and must not be flattened: %s", id, joined)
		}
	}
	tiff := plansFor(t, solid, "tiff")[0].steps[0].args
	if !hasPair(tiff, "-vf", tiffFormats) || !hasPair(tiff, "-compression_algo", "lzw") {
		t.Errorf("TIFF should use a widely readable layout: %v", tiff)
	}
	palette := solid
	palette.PixFmt, palette.Alpha = "pal8", true
	if args := plansFor(t, palette, "tiff")[0].steps[0].args; !hasPair(args, "-vf", "format=rgba") {
		t.Errorf("a palette picture must keep its transparent colours in TIFF: %v", args)
	}
	if args := plansFor(t, solid, "gif")[0].steps[0].args; !hasPair(args, "-gifflags", "-offsetting") {
		t.Errorf("a still GIF must be stored at full size: %v", args)
	}
	huge := solid
	huge.Width = 20000
	target, _ := targetByID("webp")
	if _, err := buildPlans(Job{Media: huge, Target: target, Input: `C:\a.png`, Output: `C:\a.webp`}); err == nil {
		t.Error("WebP cannot hold a 20000 pixel wide picture")
	}
	video, _ := targetByID("mp4")
	if _, err := buildPlans(Job{Media: solid, Target: video, Input: `C:\a.png`, Output: `C:\a.mp4`}); err == nil {
		t.Error("a picture cannot be converted to a video format")
	}
}

func TestAudioRecipes(t *testing.T) {
	ordinary := Media{Kind: KindAudio, Format: "mp3", HasAudio: true, AudioCodec: "mp3", Channels: 2}
	studio := Media{Kind: KindAudio, Format: "flac", HasAudio: true, AudioCodec: "flac", Channels: 2, AudioBits: 24}
	float := Media{Kind: KindAudio, Format: "wav", HasAudio: true, AudioCodec: "pcm_f32le", Channels: 2, AudioBits: 32, FloatAudio: true}
	if args := plansFor(t, ordinary, "wav")[0].steps[0].args; !hasPair(args, "-c:a", "pcm_s16le") {
		t.Errorf("an MP3 holds no more than 16 bits: %v", args)
	}
	if args := plansFor(t, ordinary, "flac")[0].steps[0].args; !hasPair(args, "-sample_fmt", "s16") {
		t.Errorf("FLAC from an MP3 should be 16-bit: %v", args)
	}
	for _, media := range []Media{studio, float} {
		if args := plansFor(t, media, "wav")[0].steps[0].args; !hasPair(args, "-c:a", "pcm_s24le") {
			t.Errorf("studio-quality sound should stay 24-bit in WAV: %v", args)
		}
	}
	if args := plansFor(t, float, "flac")[0].steps[0].args; !hasPair(args, "-sample_fmt", "s32") {
		t.Errorf("studio-quality sound should stay 24-bit in FLAC: %v", args)
	}
	surround := ordinary
	surround.Format, surround.Channels = "wav", 6
	if args := plansFor(t, surround, "m4a")[0].steps[0].args; !hasPair(args, "-b:a", "384k") || !hasPair(args, "-f", "ipod") {
		t.Errorf("surround sound needs a higher bit rate: %v", args)
	}

	covered := ordinary
	covered.Format, covered.CoverArt, covered.coverIndex = "ogg", true, 1
	plans := plansFor(t, covered, "m4a")
	if len(plans) != 2 || !hasPair(plans[0].steps[0].args, "-map", "0:1") || !hasPair(plans[1].steps[0].args, "-map", "0:0") {
		t.Fatalf("cover art is tried first, with a plan without it as the fallback: %d plans", len(plans))
	}
	for _, arg := range plans[1].steps[0].args {
		if arg == "0:1" {
			t.Error("the fallback must not include the cover picture")
		}
	}
	if plans := plansFor(t, covered, "wav"); len(plans) != 1 {
		t.Errorf("WAV cannot hold cover art, so there is only one plan: %d", len(plans))
	}

	silent := Media{Kind: KindVideo, Format: "mov", Width: 640, Height: 480, VideoCodec: "h264", PixFmt: "yuv420p"}
	target, _ := targetByID("mp3")
	if _, err := buildPlans(Job{Media: silent, Target: target, Input: `C:\a.mp4`, Output: `C:\a.mp3`}); err == nil {
		t.Error("a silent video has nothing to turn into an MP3")
	}
}

func TestH264SizeRules(t *testing.T) {
	cases := []struct{ width, height, wantWidth, wantHeight int }{
		{1920, 1080, 1920, 1080},
		{1080, 1920, 1080, 1920},
		{3840, 2160, 3840, 2160},
		{4096, 2160, 4096, 2160},
		{2160, 3840, 2160, 3840},
		{319, 239, 318, 238},
		{1921, 1080, 1920, 1080},
		{7680, 4320, 4096, 2304},
		{4320, 7680, 2304, 4096},
		{8192, 4320, 4096, 2160},
		{32, 32, 64, 64},
		{24, 20, 96, 80},
		{16, 128, 64, 512},
		{63, 63, 126, 126},
		{64, 64, 64, 64},
		{0, 0, 0, 0},
	}
	for _, c := range cases {
		width, height := h264Size(c.width, c.height)
		if width != c.wantWidth || height != c.wantHeight {
			t.Errorf("%dx%d encodes as %dx%d, want %dx%d", c.width, c.height, width, height, c.wantWidth, c.wantHeight)
		}
		if width%2 != 0 || height%2 != 0 || width > h264MaxSide || height > h264MaxSide || width*height > h264MaxPixels {
			t.Errorf("%dx%d encodes as %dx%d, which the encoder or players cannot handle", c.width, c.height, width, height)
		}
	}
	// A shape that cannot be made valid is left alone, and the encoder then reports it.
	if width, height := h264Size(4000, 10); width != 4000 || height != 10 {
		t.Errorf("4000x10 became %dx%d", width, height)
	}
	filters := func(width, height int) string {
		return strings.Join(videoFilters(Media{Width: width, Height: height}), ",")
	}
	if got := filters(1920, 1080); got != "" {
		t.Errorf("a normal video needs no filter, got %q", got)
	}
	if got := filters(319, 239); got != "crop=318:238:0:0" {
		t.Errorf("an odd size should lose one row and column, got %q", got)
	}
	if got := filters(7680, 4320); got != "scale=4096:2304" {
		t.Errorf("8K should be scaled to fit, got %q", got)
	}
	if got := filters(24, 20); got != "scale=96:80:flags=neighbor" {
		t.Errorf("a tiny picture should be enlarged with crisp pixels, got %q", got)
	}
	both := strings.Join(videoFilters(Media{Width: 720, Height: 576, Interlaced: true, Alpha: true}), ",")
	if !strings.HasPrefix(both, "bwdif=mode=send_frame,format=rgba") {
		t.Errorf("deinterlacing comes first, then the white background: %q", both)
	}
	if !shrinksForH264(Media{Width: 7680, Height: 4320}) || shrinksForH264(Media{Width: 319, Height: 239}) || shrinksForH264(Media{Width: 24, Height: 20}) {
		t.Error("only pictures above the size limit count as scaled down")
	}
}

func TestVideoPlansCopyOnlyWhatIsSafe(t *testing.T) {
	phone := Media{Kind: KindVideo, Format: "mov", sourceExt: ".mov", Width: 1920, Height: 1080, PixFmt: "yuv420p", VideoCodec: "h264",
		HasAudio: true, AudioCodec: "aac", AudioProfile: "LC", Channels: 2, DurationMs: 9000, FrameRate: 30, audioIndex: 1}
	plans := plansFor(t, phone, "mp4")
	if len(plans) != 3 {
		t.Fatalf("copy everything, copy sound only, then re-encode: got %d plans", len(plans))
	}
	first, last := plans[0].steps[0].args, plans[2].steps[0].args
	if !hasPair(first, "-c:v", "copy") || !hasPair(first, "-c:a", "copy") || !plans[0].copied {
		t.Errorf("the first plan should copy both streams: %v", first)
	}
	if !hasPair(last, "-c:v", "h264_mf") || !hasPair(last, "-c:a", "aac") || plans[2].copied || !hasPair(last, "-quality", "65") || !hasPair(last, "-g", "60") {
		t.Errorf("the last plan should re-encode everything: %v", last)
	}
	if !hasPair(first, "-movflags", "+faststart") || !hasPair(plansFor(t, phone, "mkv")[0].steps[0].args, "-f", "matroska") {
		t.Error("container options are off")
	}

	for name, change := range map[string]func(*Media){
		"hevc":         func(m *Media) { m.VideoCodec = "hevc" },
		"10-bit":       func(m *Media) { m.PixFmt = "yuv420p10le" },
		"interlaced":   func(m *Media) { m.Interlaced = true },
		"from a TS":    func(m *Media) { m.Format = "mpegts" },
		"animated":     func(m *Media) { m.Animated = true },
		"4:4:4 colour": func(m *Media) { m.PixFmt = "yuv444p" },
	} {
		media := phone
		change(&media)
		if video, _ := copyable(media, Target{ID: "mp4"}); video {
			t.Errorf("%s video must be re-encoded", name)
		}
	}
	sideways := phone
	sideways.Rotation = 90
	for id, want := range map[string]bool{"mp4": true, "mov": true, "mkv": false} {
		if video, _ := copyable(sideways, Target{ID: id}); video != want {
			t.Errorf("copying a sideways video into %s: got %v, want %v", id, video, want)
		}
	}
	he := phone
	he.AudioProfile = "HE-AAC"
	if _, audio := copyable(he, Target{ID: "mp4"}); audio {
		t.Error("HE-AAC does not play everywhere, so it is re-encoded")
	}

	mp4, mov, mkv := Target{ID: "mp4", Ext: ".mp4", Kind: KindVideo}, Target{ID: "mov", Ext: ".mov", Kind: KindVideo}, Target{ID: "mkv", Ext: ".mkv", Kind: KindVideo}
	if !alreadyIn(phone, mov) || alreadyIn(phone, mp4) || alreadyIn(phone, mkv) {
		t.Error("a MOV with H.264 and AAC is already a MOV, and nothing else")
	}
	hevc := phone
	hevc.VideoCodec = "hevc"
	if alreadyIn(hevc, mov) {
		t.Error("an HEVC MOV still needs converting to play everywhere")
	}
	m4v := phone
	m4v.sourceExt = ".m4v"
	if !alreadyIn(m4v, mp4) {
		t.Error("an M4V is an MP4 by another name")
	}
	for _, c := range []struct {
		media Media
		id    string
		want  bool
	}{
		{Media{Kind: KindImage, Format: "jpeg_pipe"}, "jpg", true},
		{Media{Kind: KindImage, Format: "png_pipe"}, "jpg", false},
		{Media{Kind: KindImage, Format: "gif"}, "gif", true},
		{Media{Kind: KindAudio, Format: "mp3", AudioCodec: "mp3"}, "mp3", true},
		{Media{Kind: KindAudio, Format: "mov", AudioCodec: "aac"}, "m4a", true},
		{Media{Kind: KindAudio, Format: "mov", AudioCodec: "alac"}, "m4a", false},
		{Media{Kind: KindAudio, Format: "aac", AudioCodec: "aac"}, "m4a", false},
		{Media{Kind: KindAudio, Format: "wav", AudioCodec: "pcm_s24le"}, "wav", true},
		{Media{Kind: KindVideo, Format: "gif", Animated: true}, "gif", true},
		{Media{Kind: KindVideo, Format: "gif", Animated: true}, "mp4", false},
		{phone, "mp3", false},
		{phone, "gif", false},
	} {
		target, _ := targetByID(c.id)
		if alreadyIn(c.media, target) != c.want {
			t.Errorf("%s %s already %s: want %v", c.media.Kind, c.media.Format, c.id, c.want)
		}
	}
}

func TestGifFromVideoUsesTwoPassesAndAPrivatePalette(t *testing.T) {
	clip := Media{Kind: KindVideo, Format: "mov", Width: 1920, Height: 1080, PixFmt: "yuv420p", VideoCodec: "h264", DurationMs: 4000, FrameRate: 60}
	plans := plansFor(t, clip, "gif")
	if len(plans) != 1 || len(plans[0].steps) != 2 {
		t.Fatalf("expected one plan with two steps, got %+v", plans)
	}
	first, second := plans[0].steps[0].args, plans[0].steps[1].args
	palette := `file:C:\work\job\palette.png`
	if first[len(first)-1] != palette || !hasPair(second, "-i", palette) {
		t.Errorf("the colour table belongs in the private work folder: %s", first[len(first)-1])
	}
	if joined := strings.Join(first, " "); !strings.Contains(joined, "fps=12,scale=480:-1:flags=lanczos,palettegen") {
		t.Errorf("a large fast video should be slowed and scaled for GIF: %s", joined)
	}
	if !hasPair(second, "-loop", "0") || !hasPair(second, "-c:v", "gif") {
		t.Errorf("the GIF should loop: %v", second)
	}
	small := Media{Kind: KindVideo, Format: "gif", Animated: true, Width: 200, Height: 100, PixFmt: "bgra", Alpha: true, DurationMs: 1000, FrameRate: 10}
	if joined := strings.Join(plansFor(t, small, "mp4")[0].steps[0].args, " "); !strings.Contains(joined, "lutrgb") || !strings.Contains(joined, "-an") {
		t.Errorf("an animated GIF becomes a silent video on white: %s", joined)
	}
}

func TestThumbnailAndSelfTestCommands(t *testing.T) {
	clip := Media{Kind: KindVideo, Width: 1920, Height: 1080, DurationMs: 60_000}
	args := thumbnailArgs(`C:\in\clip %d.mov`, clip)
	if args[len(args)-1] != "pipe:1" || !hasPair(args, "-f", "image2pipe") || !hasPair(args, "-ss", "6.000") || !hasPair(args, "-i", `file:C:\in\clip %d.mov`) {
		t.Errorf("thumbnails are sent to the app, never written to disk: %v", args)
	}
	if args := thumbnailArgs(`C:\in\photo.jpg`, Media{Kind: KindImage, Width: 100, Height: 100}); argAfter(args, "-ss") != "" {
		t.Errorf("a picture has nothing to seek into: %v", args)
	}
	if args := thumbnailArgs(`C:\in\long.mov`, Media{Kind: KindVideo, DurationMs: 3_600_000}); !hasPair(args, "-ss", "10.000") {
		t.Errorf("the preview frame is taken within the first ten seconds: %v", args)
	}
	self := h264SelfTestArgs()
	if self[len(self)-1] != "-" || !hasPair(self, "-f", "null") || !hasPair(self, "-c:v", "h264_mf") {
		t.Errorf("the self-test must not write a file: %v", self)
	}
	for _, arg := range self {
		if strings.Contains(arg, ":\\") {
			t.Errorf("the self-test must not touch any path: %s", arg)
		}
	}
}

func TestParseDecoders(t *testing.T) {
	listing := "Decoders:\n V..... = Video\n A..... = Audio\n ------\n V....D h264                 H.264 / AVC\n A....D mp3float             MP3 (MPEG audio layer 3) (codec mp3)\n A....D pcm_s16le            PCM signed 16-bit little-endian\n\n"
	decoders := parseDecoders(listing)
	for _, name := range []string{"h264", "mp3float", "mp3", "pcm_s16le"} {
		if !decoders[name] {
			t.Errorf("%s was not found in the listing", name)
		}
	}
	if decoders["="] || decoders["Video"] || len(decoders) != 4 {
		t.Errorf("the legend was read as decoders: %v", decoders)
	}
	if len(parseDecoders("")) != 0 {
		t.Error("an empty listing has no decoders")
	}
}
