package convert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeEngine stands in for ffmpeg.exe and ffprobe.exe. It answers probes from a table,
// writes a small marker file for every conversion and records each command line.
type fakeEngine struct {
	mu       sync.Mutex
	media    map[string]string // file name -> ffprobe JSON
	failures map[string]string // input file name -> engine error text
	block    bool              // conversions wait until they are cancelled
	started  chan string       // receives the input name when a conversion starts
	calls    [][]string
}

const (
	imageJSON   = `{"streams":[{"index":0,"codec_type":"video","codec_name":"png","width":640,"height":480,"pix_fmt":"rgb24"}],"format":{"format_name":"png_pipe"}}`
	alphaJSON   = `{"streams":[{"index":0,"codec_type":"video","codec_name":"png","width":640,"height":480,"pix_fmt":"rgba"}],"format":{"format_name":"png_pipe"}}`
	jpegJSON    = `{"streams":[{"index":0,"codec_type":"video","codec_name":"mjpeg","width":640,"height":480,"pix_fmt":"yuvj420p"}],"format":{"format_name":"jpeg_pipe"}}`
	audioJSON   = `{"streams":[{"index":0,"codec_type":"audio","codec_name":"mp3","sample_rate":"44100","channels":2,"sample_fmt":"fltp"}],"format":{"format_name":"mp3","duration":"10.0"}}`
	wavJSON     = `{"streams":[{"index":0,"codec_type":"audio","codec_name":"pcm_s16le","sample_rate":"44100","channels":2,"bits_per_sample":16}],"format":{"format_name":"wav","duration":"10.0"}}`
	videoJSON   = `{"streams":[{"index":0,"codec_type":"video","codec_name":"hevc","width":1920,"height":1080,"pix_fmt":"yuv420p","avg_frame_rate":"30/1"},{"index":1,"codec_type":"audio","codec_name":"aac","profile":"LC","sample_rate":"48000","channels":2}],"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"10.0"}}`
	silentJSON  = `{"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":1280,"height":720,"pix_fmt":"yuv420p","avg_frame_rate":"30/1"}],"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"10.0"}}`
	av1JSON     = `{"streams":[{"index":0,"codec_type":"video","codec_name":"av1","width":1280,"height":720,"pix_fmt":"yuv420p"}],"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"10.0"}}`
	decoderList = " ------\n V....D h264  H.264\n V....D hevc  HEVC\n V....D png  PNG\n V....D mjpeg  MJPEG\n A....D aac  AAC\n A....D mp3float  MP3 (codec mp3)\n A....D pcm_s16le  PCM\n"
)

func newFake() *fakeEngine {
	return &fakeEngine{media: map[string]string{}, failures: map[string]string{}, started: make(chan string, 64)}
}

func argAfter(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func (f *fakeEngine) run(ctx context.Context, executable string, args []string, onLine func(string)) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string{filepath.Base(executable)}, args...))
	block := f.block
	f.mu.Unlock()
	last := strings.TrimPrefix(args[len(args)-1], "file:")

	if strings.HasPrefix(filepath.Base(executable), "ffprobe") {
		if strings.HasSuffix(last, partialSuffix) {
			data, err := os.ReadFile(last)
			if err != nil {
				return nil, &ProcessError{Err: errors.New("exit status 1"), Stderr: "No such file or directory"}
			}
			return data, nil
		}
		f.mu.Lock()
		answer, ok := f.media[filepath.Base(last)]
		f.mu.Unlock()
		if !ok {
			return nil, &ProcessError{Err: errors.New("exit status 1"), Stderr: "Invalid data found when processing input"}
		}
		return []byte(answer), nil
	}
	switch {
	case argAfter(args, "-v") != "" && args[len(args)-1] == "-decoders":
		return []byte(decoderList), nil
	case last == "-":
		return nil, nil
	case last == "pipe:1":
		return []byte("jpeg-bytes"), nil
	}

	input := filepath.Base(strings.TrimPrefix(argAfter(args, "-i"), "file:"))
	f.started <- input
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	f.mu.Lock()
	failure, failed := f.failures[input]
	f.mu.Unlock()
	if failed {
		// A real engine may leave a partial file behind when it stops.
		_ = os.WriteFile(last, []byte("partial"), 0600)
		return nil, &ProcessError{Err: errors.New("exit status 1"), Stderr: failure}
	}
	if _, err := os.Stat(last); err == nil {
		// Like the real engine: it refuses to overwrite, writes nothing, never reports
		// that it finished, and still exits without an error code.
		return nil, nil
	}
	// The fake output is the probe answer the real engine would give for it.
	content := imageJSON
	switch {
	case argAfter(args, "-c:v") == "" && argAfter(args, "-c:a") != "":
		content = audioJSON
	case argAfter(args, "-f") == "mp4" || argAfter(args, "-f") == "mov" || argAfter(args, "-f") == "matroska":
		content = videoJSON
	}
	if onLine != nil {
		onLine("out_time_us=5000000")
		onLine("progress=end")
	}
	if err := os.WriteFile(last, []byte(content), 0600); err != nil {
		return nil, &ProcessError{Err: errors.New("exit status 1"), Stderr: err.Error()}
	}
	return nil, nil
}

func (f *fakeEngine) conversions() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var found [][]string
	for _, call := range f.calls {
		if call[0] == "ffmpeg.exe" && strings.HasSuffix(call[len(call)-1], partialSuffix) {
			found = append(found, call)
		}
	}
	return found
}

// readyService builds a service that skips the real engine checks.
func readyService(t *testing.T, fake *fakeEngine) *Service {
	t.Helper()
	service := NewService(t.TempDir(), t.TempDir(), fake.run)
	service.engine = Engine{FFmpeg: "ffmpeg.exe", FFprobe: "ffprobe.exe", Run: fake.run}
	service.decoders = parseDecoders(decoderList)
	service.caps = Capabilities{H264: true}
	service.state = StateReady
	t.Cleanup(service.Close)
	return service
}

func writeInput(t *testing.T, fake *fakeEngine, folder, name, probe string) string {
	t.Helper()
	path := filepath.Join(folder, name)
	if err := os.WriteFile(path, []byte("original "+name), 0600); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.media[name] = probe
	fake.mu.Unlock()
	return path
}

func waitFor(t *testing.T, service *Service, what string, done func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		snapshot := service.Status()
		if done(snapshot) {
			return snapshot
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: %+v", what, snapshot.Items)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func settled(snapshot Snapshot) bool {
	for _, item := range snapshot.Items {
		if item.Status == StatusChecking {
			return false
		}
	}
	return true
}

func finished(snapshot Snapshot) bool {
	return snapshot.Batch.State == BatchFinished
}

func addAndWait(t *testing.T, service *Service, paths ...string) Snapshot {
	t.Helper()
	result, err := service.AddFiles(paths)
	if err != nil {
		t.Fatalf("add files: %v", err)
	}
	if result.Added != len(paths) {
		t.Fatalf("added %d of %d: %+v", result.Added, len(paths), result.Skipped)
	}
	return waitFor(t, service, "inspection", settled)
}

func mustStart(t *testing.T, service *Service, policy string) Snapshot {
	t.Helper()
	result, err := service.Start(policy)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !result.Started {
		t.Fatalf("start did not run: %+v", result.Conflicts)
	}
	return waitFor(t, service, "the batch to finish", finished)
}

func itemNamed(t *testing.T, snapshot Snapshot, name string) Item {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("no item named %s", name)
	return Item{}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func partials(t *testing.T, folder string) []string {
	t.Helper()
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), partialSuffix) {
			found = append(found, entry.Name())
		}
	}
	return found
}

func TestAddFilesAcceptsMediaAndExplainsTheRest(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	photo := writeInput(t, fake, folder, "photo.png", imageJSON)
	notes := filepath.Join(folder, "notes.txt")
	empty := filepath.Join(folder, "empty.jpg")
	os.WriteFile(notes, []byte("hello"), 0600)
	os.WriteFile(empty, nil, 0600)

	result, err := service.AddFiles([]string{photo, notes, empty, folder, photo, filepath.Join(folder, "missing.mp4"), "relative.png"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 1 || len(result.Skipped) != 6 {
		t.Fatalf("added %d, skipped %+v", result.Added, result.Skipped)
	}
	reasons := map[string]string{}
	for _, skipped := range result.Skipped {
		reasons[skipped.Name] += skipped.Reason + ";"
	}
	for name, want := range map[string]string{
		"notes.txt": "not an image", "empty.jpg": "empty", filepath.Base(folder): "folders",
		"photo.png": "already in the list", "missing.mp4": "could not be opened", "relative.png": "not absolute",
	} {
		if !strings.Contains(reasons[name], want) {
			t.Errorf("%s: reason %q does not mention %q", name, reasons[name], want)
		}
	}
	if result.Message == "" {
		t.Error("skipped files must come with a ready-made message")
	}
	snapshot := waitFor(t, service, "inspection", settled)
	item := itemNamed(t, snapshot, "photo.png")
	if item.Status != StatusReady || item.Kind != KindImage || item.Source != "PNG" || item.Target != "jpg" || item.Width != 640 {
		t.Fatalf("unexpected item: %+v", item)
	}
	if len(snapshot.Kinds) != 1 || snapshot.Kinds[0].Kind != KindImage || snapshot.Kinds[0].Count != 1 {
		t.Fatalf("unexpected pickers: %+v", snapshot.Kinds)
	}
}

func TestUnreadableAndUnsupportedFilesAreMarkedNotConverted(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	broken := filepath.Join(folder, "broken.mp4")
	os.WriteFile(broken, []byte("not a video"), 0600)
	modern := writeInput(t, fake, folder, "modern.mp4", av1JSON)

	snapshot := addAndWait(t, service, broken, modern)
	if item := itemNamed(t, snapshot, "broken.mp4"); item.Status != StatusUnsupported || item.Error != unreadableFile || item.Detail == "" {
		t.Fatalf("broken file: %+v", item)
	}
	if item := itemNamed(t, snapshot, "modern.mp4"); item.Status != StatusUnsupported || !strings.Contains(item.Error, "AV1") {
		t.Fatalf("AV1 file: %+v", item)
	}
	if _, err := service.Start(PolicyAsk); err == nil || !strings.Contains(err.Error(), "nothing to convert") {
		t.Fatalf("start with nothing usable: %v", err)
	}
}

func TestConvertWritesNextToOriginalAndLeavesItUntouched(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	photo := writeInput(t, fake, folder, "holiday photo.png", imageJSON)
	addAndWait(t, service, photo)

	snapshot := mustStart(t, service, PolicyAsk)
	item := itemNamed(t, snapshot, "holiday photo.png")
	want := filepath.Join(folder, "holiday photo.jpg")
	if item.Status != StatusDone || item.OutputPath != want || item.OutputName != "holiday photo.jpg" || item.Progress != 100 || item.OutputSize == 0 {
		t.Fatalf("unexpected result: %+v", item)
	}
	if got := readFile(t, photo); got != "original holiday photo.png" {
		t.Fatalf("the original was changed: %q", got)
	}
	if left := partials(t, folder); len(left) != 0 {
		t.Fatalf("temporary files were left behind: %v", left)
	}
	if snapshot.Batch.Done != 1 || snapshot.Batch.Total != 1 {
		t.Fatalf("unexpected batch: %+v", snapshot.Batch)
	}
	command := fake.conversions()[0]
	joined := strings.Join(command, " ")
	if !strings.Contains(joined, "-i file:"+photo) || !strings.Contains(joined, " -n file:") || strings.Contains(joined, " -y ") {
		t.Fatalf("the engine command is not safe: %s", joined)
	}
}

func TestExistingOutputNeedsADecision(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	photo := writeInput(t, fake, folder, "photo.png", imageJSON)
	existing := filepath.Join(folder, "photo.jpg")
	os.WriteFile(existing, []byte("older conversion"), 0600)
	addAndWait(t, service, photo)

	result, err := service.Start(PolicyAsk)
	if err != nil {
		t.Fatal(err)
	}
	if result.Started || len(result.Conflicts) != 1 || result.Conflicts[0].Name != "photo.jpg" || result.Conflicts[0].Folder != folder {
		t.Fatalf("expected one conflict and no start: %+v", result)
	}
	if len(fake.conversions()) != 0 || service.Status().Batch.State != BatchIdle {
		t.Fatal("nothing may run before the user decides")
	}

	snapshot := mustStart(t, service, PolicyKeep)
	item := itemNamed(t, snapshot, "photo.png")
	if item.OutputName != "photo (1).jpg" {
		t.Fatalf("keep both should add a number: %+v", item)
	}
	if got := readFile(t, existing); got != "older conversion" {
		t.Fatalf("the existing file was changed: %q", got)
	}
}

func TestReplaceOnlyAfterExplicitChoice(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	photo := writeInput(t, fake, folder, "photo.png", imageJSON)
	existing := filepath.Join(folder, "photo.jpg")
	os.WriteFile(existing, []byte("older conversion"), 0600)
	addAndWait(t, service, photo)

	snapshot := mustStart(t, service, PolicyReplace)
	item := itemNamed(t, snapshot, "photo.png")
	if item.OutputPath != existing || readFile(t, existing) == "older conversion" {
		t.Fatalf("replace should overwrite the existing file: %+v", item)
	}
	if _, err := os.Stat(filepath.Join(folder, "photo (1).jpg")); err == nil {
		t.Fatal("replace must not also create a numbered copy")
	}
}

func TestAnOriginalIsNeverReplaced(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	// A PNG that someone named .jpg. Converting it to JPG in its own folder would land
	// exactly on the original.
	photo := writeInput(t, fake, folder, "scan.jpg", imageJSON)
	// A PNG whose JPG name is another file in the list.
	twin := writeInput(t, fake, folder, "scan.png", imageJSON)
	addAndWait(t, service, photo, twin)

	result, err := service.Start(PolicyAsk)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Conflicts) != 0 || !result.Started {
		t.Fatalf("landing on an original is not a question, it is always renamed: %+v", result)
	}
	snapshot := waitFor(t, service, "the batch to finish", finished)
	first, second := itemNamed(t, snapshot, "scan.jpg"), itemNamed(t, snapshot, "scan.png")
	if first.OutputName == "scan.jpg" || second.OutputName == "scan.jpg" || first.OutputName == second.OutputName {
		t.Fatalf("outputs collide with an original or each other: %q and %q", first.OutputName, second.OutputName)
	}
	if got := readFile(t, photo); got != "original scan.jpg" {
		t.Fatalf("the original was overwritten: %q", got)
	}

	// Even an explicit "replace" must not touch an original.
	if err := service.SetTarget(KindImage, "jpg"); err != nil {
		t.Fatal(err)
	}
	mustStart(t, service, PolicyReplace)
	if got := readFile(t, photo); got != "original scan.jpg" {
		t.Fatalf("replace overwrote an original: %q", got)
	}
}

func TestSameNamesInOneBatchGetDifferentOutputs(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	first, second, destination := t.TempDir(), t.TempDir(), t.TempDir()
	one := writeInput(t, fake, first, "IMG_0001.png", imageJSON)
	two := writeInput(t, fake, second, "IMG_0001.PNG", imageJSON)
	fake.media["IMG_0001.PNG"] = imageJSON
	if err := service.SetDestination(DestinationFolder, destination); err != nil {
		t.Fatal(err)
	}
	addAndWait(t, service, one, two)

	snapshot := mustStart(t, service, PolicyAsk)
	names := map[string]bool{}
	for _, item := range snapshot.Items {
		if item.Status != StatusDone || filepath.Dir(item.OutputPath) != destination {
			t.Fatalf("unexpected item: %+v", item)
		}
		names[strings.ToLower(item.OutputName)] = true
	}
	if !names["img_0001.jpg"] || !names["img_0001 (1).jpg"] {
		t.Fatalf("expected two distinct names, got %v", names)
	}
}

func TestFailureIsExplainedAndCleansUp(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	good := writeInput(t, fake, folder, "good.png", imageJSON)
	bad := writeInput(t, fake, folder, "bad.png", imageJSON)
	fake.failures["bad.png"] = "[png @ 0x1] Invalid data found when processing input\nConversion failed!"
	addAndWait(t, service, bad, good)

	snapshot := mustStart(t, service, PolicyAsk)
	failed := itemNamed(t, snapshot, "bad.png")
	if failed.Status != StatusFailed || failed.Error != "This file looks damaged or incomplete." || !strings.Contains(failed.Detail, "Conversion failed") {
		t.Fatalf("unexpected failure report: %+v", failed)
	}
	if done := itemNamed(t, snapshot, "good.png"); done.Status != StatusDone {
		t.Fatalf("one failure must not stop the rest: %+v", done)
	}
	if left := partials(t, folder); len(left) != 0 {
		t.Fatalf("a failed conversion left files behind: %v", left)
	}
	if snapshot.Batch.Failed != 1 || snapshot.Batch.Done != 1 {
		t.Fatalf("unexpected batch: %+v", snapshot.Batch)
	}

	// Trying again converts only what did not succeed.
	delete(fake.failures, "bad.png")
	before := len(fake.conversions())
	snapshot = mustStart(t, service, PolicyAsk)
	if item := itemNamed(t, snapshot, "bad.png"); item.Status != StatusDone {
		t.Fatalf("retry failed: %+v", item)
	}
	if ran := len(fake.conversions()) - before; ran != 1 {
		t.Fatalf("retry ran %d conversions, want 1", ran)
	}
}

func TestCancelStopsAndRemovesTheUnfinishedFile(t *testing.T) {
	fake := newFake()
	fake.block = true
	service := readyService(t, fake)
	folder := t.TempDir()
	first := writeInput(t, fake, folder, "first.png", imageJSON)
	second := writeInput(t, fake, folder, "second.png", imageJSON)
	addAndWait(t, service, first, second)

	if result, err := service.Start(PolicyAsk); err != nil || !result.Started {
		t.Fatalf("start: %v %+v", err, result)
	}
	<-fake.started
	if _, err := service.AddFiles([]string{first}); err == nil {
		t.Fatal("adding files while converting should be refused")
	}
	if err := service.RemoveItem(service.Status().Items[0].ID); err == nil {
		t.Fatal("removing files while converting should be refused")
	}
	if err := service.Cancel(); err != nil {
		t.Fatal(err)
	}
	snapshot := waitFor(t, service, "the batch to stop", finished)
	for _, item := range snapshot.Items {
		if item.Status != StatusCancelled {
			t.Fatalf("expected cancelled, got %+v", item)
		}
	}
	if snapshot.Batch.Cancelled != 2 {
		t.Fatalf("unexpected batch: %+v", snapshot.Batch)
	}
	entries, _ := os.ReadDir(folder)
	if len(entries) != 2 {
		t.Fatalf("only the two originals should remain, found %d entries", len(entries))
	}
	if err := service.Cancel(); err == nil {
		t.Fatal("cancel with nothing running should say so")
	}
}

func TestVideoWithoutSoundIsSkippedForAudioTargets(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	clip := writeInput(t, fake, folder, "timelapse.mp4", silentJSON)
	talk := writeInput(t, fake, folder, "talk.mov", videoJSON)
	addAndWait(t, service, clip, talk)
	if err := service.SetTarget(KindVideo, "mp3"); err != nil {
		t.Fatal(err)
	}
	if item := itemNamed(t, service.Status(), "timelapse.mp4"); item.Status != StatusSkipped || !strings.Contains(item.Error, "no sound") {
		t.Fatalf("the problem should be visible before converting: %+v", item)
	}

	snapshot := mustStart(t, service, PolicyAsk)
	if item := itemNamed(t, snapshot, "timelapse.mp4"); item.Status != StatusSkipped || !strings.Contains(item.Error, "no sound") {
		t.Fatalf("unexpected: %+v", item)
	}
	if item := itemNamed(t, snapshot, "talk.mov"); item.Status != StatusDone || item.OutputName != "talk.mp3" {
		t.Fatalf("unexpected: %+v", item)
	}
	// Only the file that could be converted counts as part of the batch.
	if snapshot.Batch.Total != 1 || snapshot.Batch.Done != 1 || snapshot.Batch.Skipped != 0 {
		t.Fatalf("unexpected batch: %+v", snapshot.Batch)
	}

	// Choosing a video format again makes the silent clip convertible.
	if err := service.SetTarget(KindVideo, "mkv"); err != nil {
		t.Fatal(err)
	}
	if item := itemNamed(t, service.Status(), "timelapse.mp4"); item.Status != StatusReady || item.Error != "" {
		t.Fatalf("the clip should be ready for a video format: %+v", item)
	}
}

func TestFilesAlreadyInTheFormatAreSkipped(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	photo := writeInput(t, fake, folder, "photo.jpg", jpegJSON)
	drawing := writeInput(t, fake, folder, "drawing.png", imageJSON)
	addAndWait(t, service, photo, drawing)
	listed := itemNamed(t, service.Status(), "photo.jpg")
	if listed.Status != StatusSkipped || !strings.Contains(listed.Error, "Already JPG") {
		t.Fatalf("the reason should be visible before converting: %+v", listed)
	}
	if kinds := service.Status().Kinds; len(kinds) != 1 || kinds[0].Count != 2 {
		t.Fatalf("a skipped file still counts for the format picker, so the format can be changed: %+v", kinds)
	}

	snapshot := mustStart(t, service, PolicyAsk)
	if item := itemNamed(t, snapshot, "photo.jpg"); item.Status != StatusSkipped || item.OutputPath != "" {
		t.Fatalf("a JPG must not be converted to JPG again: %+v", item)
	}
	if item := itemNamed(t, snapshot, "drawing.png"); item.Status != StatusDone {
		t.Fatalf("unexpected: %+v", item)
	}
	if ran := len(fake.conversions()); ran != 1 || snapshot.Batch.Total != 1 {
		t.Fatalf("ran %d conversions in a batch of %d, want 1", ran, snapshot.Batch.Total)
	}
	entries, _ := os.ReadDir(folder)
	if len(entries) != 3 {
		t.Fatalf("expected the two originals and one new file, found %d entries", len(entries))
	}

	// With nothing left that needs converting, Convert says so instead of pretending to work.
	if err := service.RemoveItem(itemNamed(t, snapshot, "drawing.png").ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(PolicyAsk); err == nil || !strings.Contains(err.Error(), "Already JPG") {
		t.Fatalf("expected an explanation, got %v", err)
	}
}

func TestChangingTheFormatMakesFinishedFilesReadyAgain(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	photo := writeInput(t, fake, folder, "photo.png", imageJSON)
	song := writeInput(t, fake, folder, "song.wav", wavJSON)
	addAndWait(t, service, photo, song)
	mustStart(t, service, PolicyAsk)

	if err := service.SetTarget(KindImage, "webp"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Status()
	if item := itemNamed(t, snapshot, "photo.png"); item.Status != StatusReady || item.TargetLabel != "WebP" || item.OutputPath != "" {
		t.Fatalf("the image should be ready for the new format: %+v", item)
	}
	if item := itemNamed(t, snapshot, "song.wav"); item.Status != StatusDone {
		t.Fatalf("other kinds keep their result: %+v", item)
	}
	if err := service.SetTarget(KindImage, "mp4"); err == nil {
		t.Fatal("a format that is not offered for images must be refused")
	}

	// The choice is remembered for the next start of the app.
	if stored := loadSettings(service.settingsPath()); stored.Targets[KindImage] != "webp" {
		t.Fatalf("the format was not saved: %+v", stored)
	}
}

func TestUnavailableEncoderIsRefusedWithTheReason(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	service.caps = Capabilities{H264Reason: missingH264}
	folder := t.TempDir()
	addAndWait(t, service, writeInput(t, fake, folder, "talk.mov", videoJSON))

	err := service.SetTarget(KindVideo, "mp4")
	if err == nil || !strings.Contains(err.Error(), "H.264 encoder") {
		t.Fatalf("expected the reason, got %v", err)
	}
	var mp4 Option
	for _, option := range service.Status().Kinds[0].Options {
		if option.ID == "mp4" {
			mp4 = option
		}
	}
	if mp4.Available || mp4.Reason == "" {
		t.Fatalf("MP4 should be marked unavailable: %+v", mp4)
	}
	if err := service.SetTarget(KindVideo, "gif"); err != nil {
		t.Fatalf("formats that do not need the encoder still work: %v", err)
	}
}

func TestDestinationFolderMustExist(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder, destination := t.TempDir(), filepath.Join(t.TempDir(), "exports")
	if err := service.SetDestination(DestinationFolder, destination); err == nil {
		t.Fatal("a missing folder must be refused")
	}
	os.Mkdir(destination, 0700)
	if err := service.SetDestination(DestinationFolder, destination); err != nil {
		t.Fatal(err)
	}
	addAndWait(t, service, writeInput(t, fake, folder, "photo.png", imageJSON))
	os.Remove(destination)
	if _, err := service.Start(PolicyAsk); err == nil || !strings.Contains(err.Error(), "no longer available") {
		t.Fatalf("a folder that disappeared must stop the start: %v", err)
	}
	if err := service.SetDestination(DestinationSource, ""); err != nil {
		t.Fatal(err)
	}
	if got := service.Status().Destination; got.Mode != DestinationSource || got.Folder != destination {
		t.Fatalf("the earlier folder should stay remembered: %+v", got)
	}
}

func TestRemoveAndClearOnlyChangeTheList(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	photo := writeInput(t, fake, folder, "photo.png", imageJSON)
	song := writeInput(t, fake, folder, "song.mp3", audioJSON)
	snapshot := addAndWait(t, service, photo, song)

	if err := service.RemoveItem(itemNamed(t, snapshot, "photo.png").ID); err != nil {
		t.Fatal(err)
	}
	if items := service.Status().Items; len(items) != 1 || items[0].Name != "song.mp3" {
		t.Fatalf("unexpected list: %+v", items)
	}
	if err := service.ClearItems(); err != nil {
		t.Fatal(err)
	}
	if items := service.Status().Items; len(items) != 0 {
		t.Fatalf("the list should be empty: %+v", items)
	}
	for _, path := range []string{photo, song} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("removing from the list deleted a file: %v", err)
		}
	}
}

func TestThumbnailsStayInMemory(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	snapshot := addAndWait(t, service, writeInput(t, fake, folder, "photo.png", imageJSON), writeInput(t, fake, folder, "song.mp3", audioJSON))
	photo := itemNamed(t, snapshot, "photo.png")
	waitFor(t, service, "the thumbnail", func(s Snapshot) bool { return itemNamed(t, s, "photo.png").Thumb })
	if string(service.Thumbnail(photo.ID)) != "jpeg-bytes" {
		t.Fatal("the thumbnail was not stored")
	}
	if song := itemNamed(t, service.Status(), "song.mp3"); song.Thumb || service.Thumbnail(song.ID) != nil {
		t.Fatal("audio files have no thumbnail")
	}
	entries, _ := os.ReadDir(folder)
	if len(entries) != 2 {
		t.Fatalf("thumbnails must not be written next to the files: %d entries", len(entries))
	}
}

func TestListIsLimited(t *testing.T) {
	fake := newFake()
	service := readyService(t, fake)
	folder := t.TempDir()
	paths := make([]string, 0, maxItems+2)
	for i := 0; i < maxItems+2; i++ {
		paths = append(paths, writeInput(t, fake, folder, fmt.Sprintf("photo-%03d.png", i), imageJSON))
	}
	result, err := service.AddFiles(paths)
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != maxItems || len(result.Skipped) != 2 || !strings.Contains(result.Skipped[0].Reason, "full") {
		t.Fatalf("added %d, skipped %d", result.Added, len(result.Skipped))
	}
}

func TestSkippedSummary(t *testing.T) {
	one := AddResult{Skipped: []Skipped{{Name: "a.txt", Reason: "not media"}}}
	if got := SkippedSummary(one); got != "Skipped a.txt: not media." {
		t.Errorf("one file: %q", got)
	}
	same := AddResult{Skipped: []Skipped{{"a.txt", "not media"}, {"b.txt", "not media"}, {"c.txt", "not media"}, {"d.txt", "not media"}}}
	if got := SkippedSummary(same); got != "Skipped 4 files (not media): a.txt, b.txt, c.txt and 1 more." {
		t.Errorf("same reason: %q", got)
	}
	mixed := AddResult{Skipped: []Skipped{{"a.txt", "not media"}, {"b", "folders"}}}
	if got := SkippedSummary(mixed); got != "Skipped 2 files that Convert Me cannot use: a.txt, b." {
		t.Errorf("mixed reasons: %q", got)
	}
	if SkippedSummary(AddResult{}) != "" {
		t.Error("nothing skipped means no message")
	}
}
