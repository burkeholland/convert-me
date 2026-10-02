package convert

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryOfferedFormatIsDefined(t *testing.T) {
	seen := map[string]bool{}
	for _, target := range targetList {
		if seen[target.ID] {
			t.Errorf("target %s is defined twice", target.ID)
		}
		seen[target.ID] = true
		if target.Ext != "."+target.ID || target.Label == "" {
			t.Errorf("target %s has extension %q and label %q", target.ID, target.Ext, target.Label)
		}
		if _, known := inputTypes[target.Ext]; !known {
			t.Errorf("the app writes %s but does not accept it as input", target.Ext)
		}
	}
	for _, kind := range kindOrder {
		if len(offered[kind]) == 0 {
			t.Errorf("no targets are offered for %s files", kind)
		}
		for _, id := range offered[kind] {
			if _, ok := targetByID(id); !ok {
				t.Errorf("%s offers %s, which is not defined", kind, id)
			}
		}
		if !offers(kind, defaultTargets[kind]) {
			t.Errorf("the default for %s is not offered", kind)
		}
	}
	if offers(KindImage, "mp4") || offers(KindAudio, "jpg") || !offers(KindVideo, "mp3") {
		t.Error("the offer table mixes up kinds")
	}
}

func TestFormatTableIsTheSameListTheAppUses(t *testing.T) {
	rows := FormatTable()
	if len(rows) != 3 || rows[0].Kind != KindImage || rows[1].Kind != KindVideo || rows[2].Kind != KindAudio {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if got := strings.Join(rows[1].Writes, " "); got != "MP4 MOV MKV GIF" {
		t.Errorf("video writes %q: audio-only choices do not belong in the table", got)
	}
	for _, row := range rows {
		listed := map[string]bool{}
		for _, label := range row.Reads {
			listed[label] = true
		}
		for extension, input := range inputTypes {
			if input.Kind == row.Kind && input.Listed && !listed[input.Label] {
				t.Errorf("%s is accepted but missing from the %s row", extension, row.Label)
			}
		}
		for _, label := range row.Reads {
			found := false
			for _, input := range inputTypes {
				found = found || (input.Kind == row.Kind && input.Label == label && input.Listed)
			}
			if !found {
				t.Errorf("the %s row promises %s, which no extension provides", row.Label, label)
			}
		}
	}
}

func TestKnownExtensionAndDialogPattern(t *testing.T) {
	for _, path := range []string{`C:\a\photo.JPG`, `C:\a\clip.Mov`, `C:\a\song.flac`, `C:\a\archive.tar.webp`} {
		if !KnownExtension(path) {
			t.Errorf("%s should be accepted", path)
		}
	}
	for _, path := range []string{`C:\a\notes.txt`, `C:\a\photo`, `C:\a\photo.jpg.exe`, `C:\a\movie.heic`} {
		if KnownExtension(path) {
			t.Errorf("%s should not be accepted", path)
		}
	}
	pattern := DialogPattern()
	for extension := range inputTypes {
		if !strings.Contains(";"+pattern+";", ";*"+extension+";") {
			t.Errorf("the file dialog does not show %s", extension)
		}
	}
}

func TestMissingEncoderGreysOutVideoButKeepsTheRest(t *testing.T) {
	missing := Capabilities{H264Reason: missingH264}
	for _, option := range options(KindVideo, missing) {
		needs := option.ID == "mp4" || option.ID == "mov" || option.ID == "mkv"
		if option.Available == needs || (needs && option.Reason == "") {
			t.Errorf("unexpected option without the encoder: %+v", option)
		}
		if (option.Group == AudioOnlyGroup) != (option.ID == "mp3" || option.ID == "m4a" || option.ID == "wav" || option.ID == "flac") {
			t.Errorf("audio-only grouping is wrong: %+v", option)
		}
	}
	if got := firstAvailable(KindVideo, "mp4", missing); got != "gif" {
		t.Errorf("without the encoder the video default should fall back to GIF, got %s", got)
	}
	if got := firstAvailable(KindVideo, "mov", Capabilities{H264: true}); got != "mov" {
		t.Errorf("an available choice must be kept, got %s", got)
	}
	if got := firstAvailable(KindImage, "nonsense", Capabilities{H264: true}); got != "jpg" {
		t.Errorf("an unknown choice falls back to the first format, got %s", got)
	}
}

func TestSourceLabelPrefersContentOverName(t *testing.T) {
	if got := sourceLabel(`C:\a\scan.jpg`, Media{Format: "png_pipe"}); got != "PNG" {
		t.Errorf("a PNG named .jpg should be labelled PNG, got %s", got)
	}
	if got := sourceLabel(`C:\a\clip.MPEG`, Media{Format: "mpeg"}); got != "MPG" {
		t.Errorf("got %s", got)
	}
	if got := sourceLabel(`C:\a\clip.xyz`, Media{}); got != "XYZ" {
		t.Errorf("got %s", got)
	}
}

func TestSettingsSurviveARoundTripAndIgnoreNonsense(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	if got := loadSettings(path); got.Targets[KindImage] != "jpg" || got.DestinationMode != DestinationSource {
		t.Fatalf("defaults expected when nothing is stored: %+v", got)
	}
	folder := t.TempDir()
	saved := defaultSettings()
	saved.Targets[KindVideo] = "mkv"
	saved.DestinationMode, saved.DestinationFolder = DestinationFolder, folder
	if err := saveSettings(path, saved); err != nil {
		t.Fatal(err)
	}
	loaded := loadSettings(path)
	if loaded.Targets[KindVideo] != "mkv" || loaded.DestinationMode != DestinationFolder || loaded.DestinationFolder != folder {
		t.Fatalf("round trip lost something: %+v", loaded)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("the temporary settings file was left behind")
	}

	// A hand-edited or damaged file must never break the app.
	os.WriteFile(path, []byte(`{"targets":{"image":"mp4","audio":"exe","video":"gif","pdf":"jpg"},"destinationMode":"folder","destinationFolder":"relative\\path"}`), 0600)
	loaded = loadSettings(path)
	if loaded.Targets[KindImage] != "jpg" || loaded.Targets[KindAudio] != "mp3" || loaded.Targets[KindVideo] != "gif" {
		t.Errorf("invalid formats should fall back to defaults: %+v", loaded.Targets)
	}
	if loaded.DestinationMode != DestinationSource || loaded.DestinationFolder != "" {
		t.Errorf("a relative folder must be ignored: %+v", loaded)
	}
	os.WriteFile(path, []byte(`{not json`), 0600)
	if got := loadSettings(path); got.Targets[KindAudio] != "mp3" {
		t.Errorf("damaged settings should mean defaults: %+v", got)
	}
}

func manifestFor(t *testing.T, root string) Manifest {
	t.Helper()
	manifest := Manifest{Engine: map[string]string{"ffmpeg": "8.1.3"}}
	for _, asset := range []string{ffmpegAsset, ffprobeAsset} {
		path := filepath.Join(root, filepath.FromSlash(asset))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		content := []byte("engine " + asset)
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		manifest.Files = append(manifest.Files, Asset{Path: asset, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))})
	}
	return manifest
}

func TestManifestPinsExactlyTheTwoEngineFiles(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	entry := func(path string) string {
		return fmt.Sprintf(`{"path":%q,"sha256":%q,"size":10}`, path, hash)
	}
	good := `{"engine":{"ffmpeg":"8.1.3"},"files":[` + entry(ffmpegAsset) + "," + entry(ffprobeAsset) + `]}`
	manifest, err := ParseManifest([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.EngineLabel() != "FFmpeg 8.1.3" || (Manifest{}).EngineLabel() != "FFmpeg" {
		t.Errorf("engine label: %q", manifest.EngineLabel())
	}
	for name, text := range map[string]string{
		"empty":      `{"engine":{},"files":[]}`,
		"missing":    `{"files":[` + entry(ffmpegAsset) + `]}`,
		"duplicate":  `{"files":[` + entry(ffmpegAsset) + "," + entry(ffmpegAsset) + "," + entry(ffprobeAsset) + `]}`,
		"extra":      `{"files":[` + entry(ffmpegAsset) + "," + entry(ffprobeAsset) + "," + entry("runtime/evil.exe") + `]}`,
		"traversal":  `{"files":[` + entry("runtime/../../windows/system32/cmd.exe") + "," + entry(ffprobeAsset) + `]}`,
		"short hash": `{"files":[{"path":"` + ffmpegAsset + `","sha256":"abcd","size":10},` + entry(ffprobeAsset) + `]}`,
		"upper hash": `{"files":[{"path":"` + ffmpegAsset + `","sha256":"` + strings.ToUpper(hash) + `","size":10},` + entry(ffprobeAsset) + `]}`,
		"zero size":  `{"files":[{"path":"` + ffmpegAsset + `","sha256":"` + hash + `","size":0},` + entry(ffprobeAsset) + `]}`,
		"not json":   `nope`,
	} {
		if _, err := ParseManifest([]byte(text)); err == nil {
			t.Errorf("%s manifest was accepted", name)
		}
	}
}

func TestVerifyManifestNoticesAChangedEngine(t *testing.T) {
	root := t.TempDir()
	manifest := manifestFor(t, root)
	ctx := context.Background()
	if err := VerifyManifest(ctx, root, manifest); err != nil {
		t.Fatalf("a matching engine was rejected: %v", err)
	}
	tool := filepath.Join(root, filepath.FromSlash(ffmpegAsset))
	original, _ := os.ReadFile(tool)

	swapped := append([]byte{}, original...)
	swapped[0] ^= 0xff
	os.WriteFile(tool, swapped, 0600)
	if err := VerifyManifest(ctx, root, manifest); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("a changed file of the same size must be noticed: %v", err)
	}
	os.WriteFile(tool, append(original, 'x'), 0600)
	if err := VerifyManifest(ctx, root, manifest); err == nil || !strings.Contains(err.Error(), "size") {
		t.Errorf("a file of a different size must be noticed: %v", err)
	}
	os.Remove(tool)
	if err := VerifyManifest(ctx, root, manifest); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("a missing file must be noticed: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := VerifyManifest(cancelled, root, manifest); !errors.Is(err, context.Canceled) {
		t.Errorf("verification should stop when the app is closing: %v", err)
	}
}

func TestInitializeFailsClearlyWithoutAnEngine(t *testing.T) {
	root := t.TempDir()
	manifest := manifestFor(t, root)
	os.Remove(filepath.Join(root, filepath.FromSlash(ffprobeAsset)))
	data := fmt.Sprintf(`{"engine":{"ffmpeg":"8.1.3"},"files":[{"path":%q,"sha256":%q,"size":%d},{"path":%q,"sha256":%q,"size":%d}]}`,
		manifest.Files[0].Path, manifest.Files[0].SHA256, manifest.Files[0].Size,
		manifest.Files[1].Path, manifest.Files[1].SHA256, manifest.Files[1].Size)
	service := NewService(root, t.TempDir(), newFake().run)
	t.Cleanup(service.Close)
	if err := service.Initialize([]byte(data)); err == nil {
		t.Fatal("start-up should fail when an engine file is missing")
	}
	snapshot := service.Status()
	if snapshot.State != StateFailed || !strings.Contains(snapshot.SetupError, "missing or damaged") || !strings.HasSuffix(snapshot.SetupError, ".") {
		t.Fatalf("the failure should be explained: %+v", snapshot)
	}
	if _, err := service.AddFiles([]string{`C:\a\photo.png`}); err == nil {
		t.Error("files must be refused while the engine is unavailable")
	}
	if _, err := service.Start(PolicyAsk); err == nil {
		t.Error("converting must be refused while the engine is unavailable")
	}
}

func TestInitializeReadsCapabilitiesFromTheEngine(t *testing.T) {
	root := t.TempDir()
	manifest := manifestFor(t, root)
	data := fmt.Sprintf(`{"engine":{"ffmpeg":"8.1.3"},"files":[{"path":%q,"sha256":%q,"size":%d},{"path":%q,"sha256":%q,"size":%d}]}`,
		manifest.Files[0].Path, manifest.Files[0].SHA256, manifest.Files[0].Size,
		manifest.Files[1].Path, manifest.Files[1].SHA256, manifest.Files[1].Size)
	dataDir := t.TempDir()
	stale := filepath.Join(dataDir, "work", "old-run")
	os.MkdirAll(stale, 0700)
	service := NewService(root, dataDir, newFake().run)
	t.Cleanup(service.Close)
	if err := service.Initialize([]byte(data)); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Status()
	if snapshot.State != StateReady || snapshot.Engine != "FFmpeg 8.1.3" || snapshot.Version != Version {
		t.Fatalf("unexpected state: %+v", snapshot)
	}
	if !service.caps.H264 || !service.decoders["mp3"] || !service.decoders["h264"] {
		t.Errorf("capabilities were not read: %+v %v", service.caps, service.decoders)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("leftover work folders from an earlier run should be cleared at start-up")
	}
}

func TestExplainTurnsEngineOutputIntoPlainSentences(t *testing.T) {
	cases := map[string]string{
		"av_interleaved_write_frame(): No space left on device":                         "drive is full",
		"file:C:\\out\\a.jpg.1a2b3c4d" + partialSuffix + ": Permission denied":          "save in this folder",
		"file:C:\\in\\locked.mov: Permission denied":                                    "read this file",
		"file:C:\\gone.mov: No such file or directory":                                  "no longer there",
		"Decoder (codec av1) not found for input stream #0:0":                           "cannot read yet",
		"[h264_mf @ 000001] could not set output type (MF_E_INVALIDMEDIATYPE)":          "H.264 encoder",
		"[mov,mp4,m4a,3gp,3g2,mj2 @ 0001] moov atom not found":                          "damaged or incomplete",
		"[png @ 0001] Invalid data found when processing input":                         "damaged or incomplete",
		"something nobody has seen before":                                              genericFailure,
		"Error while decoding stream #0:0: Invalid data\nConversion failed!\nlast line": "damaged or incomplete",
	}
	for stderr, want := range cases {
		message, detail := explain(&ProcessError{Err: errors.New("exit status 1"), Stderr: stderr})
		if !strings.Contains(message, want) {
			t.Errorf("%q explained as %q, expected something about %q", stderr, message, want)
		}
		if detail == "" || !strings.HasSuffix(message, ".") {
			t.Errorf("%q: message %q, detail %q", stderr, message, detail)
		}
	}
	message, detail := explain(errors.New("the engine stopped before it finished writing"))
	if message != "The engine stopped before it finished writing." || detail != "" {
		t.Errorf("plain errors become sentences without details: %q %q", message, detail)
	}
	if sentence("  ") != genericFailure || sentence("already a sentence.") != "Already a sentence." {
		t.Error("sentence formatting is off")
	}
	long := strings.Repeat("line\n", 100) + strings.Repeat("x", 3000)
	if got := lastLines(long, 12); len(got) > 2000 {
		t.Errorf("details must stay short, got %d characters", len(got))
	}
	if got := lastLines("a\r\nb\r\nc", 2); got != "b\nc" {
		t.Errorf("last lines: %q", got)
	}
	if codecName("hevc") != "HEVC (H.265)" || codecName("") != "a codec" || codecName("svq3") != "SVQ3" {
		t.Error("codec names are off")
	}
}
