package convert

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeImages stands in for the part of Windows that reads HEIC photos.
type fakeImages struct {
	mu      sync.Mutex
	missing bool            // this PC cannot read HEIC photos
	broken  map[string]bool // photos, by name, that Windows cannot read
	exports []string        // "input name -> output name @ largest side"
}

func (f *fakeImages) Check() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.missing {
		return errors.New("no decoder")
	}
	return nil
}

func (f *fakeImages) Export(input, output string, maxSide int) (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exports = append(f.exports, fmt.Sprintf("%s -> %s @ %d", filepath.Base(input), filepath.Base(output), maxSide))
	if f.missing || f.broken[filepath.Base(input)] {
		return 0, 0, &SystemImageError{Message: heicUnreadable, Detail: "Windows imaging error 0x88982F50"}
	}
	if err := os.WriteFile(output, []byte("a PNG made by Windows"), 0600); err != nil {
		return 0, 0, err
	}
	return 4032, 3024, nil
}

func (f *fakeImages) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

func (f *fakeImages) log() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.exports, "\n")
}

// heicBytes is the start of a HEIC file as phones write it, followed by filler.
var heicBytes = []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic\x00\x00\x00\x08free and the rest of a photo")

// heicService is a service whose engine and whose Windows are both fakes. The engine is
// asked about the copy that Windows makes, never about the photo itself.
func heicService(t *testing.T, fake *fakeEngine, images *fakeImages) *Service {
	t.Helper()
	service := readyService(t, fake)
	service.system = images
	service.caps.HEIC, service.caps.HEICReason = checkSystemImages(images)
	fake.mu.Lock()
	fake.media["source.png"] = imageJSON
	fake.mu.Unlock()
	return service
}

func writeHEIC(t *testing.T, folder, name string) string {
	t.Helper()
	path := filepath.Join(folder, name)
	if err := os.WriteFile(path, heicBytes, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func imageReads(snapshot Snapshot) string {
	for _, row := range snapshot.Formats {
		if row.Kind == KindImage {
			return strings.Join(row.Reads, " ")
		}
	}
	return ""
}

func workFiles(t *testing.T, service *Service) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(service.dataDir, "work"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestHEICIsToldByContent(t *testing.T) {
	for name, head := range map[string]string{
		"phone photo":                "\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic",
		"brand only in the list":     "\x00\x00\x00\x1cftypmif1\x00\x00\x00\x00mif1miafheic",
		"10 bit photo":               "\x00\x00\x00\x18ftypheix\x00\x00\x00\x00mif1heix",
		"burst of photos":            "\x00\x00\x00\x18ftyphevc\x00\x00\x00\x00msf1hevc",
		"length larger than we read": "\x00\x00\x10\x00ftypheic\x00\x00\x00\x00",
	} {
		if !heicContent([]byte(head)) {
			t.Errorf("%s was not recognised as HEIC", name)
		}
	}
	for name, head := range map[string]string{
		"empty":                    "",
		"short":                    "\x00\x00\x00\x18ftyp",
		"jpeg":                     "\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01",
		"mp4 video":                "\x00\x00\x00\x20ftypisom\x00\x00\x02\x00isomiso2avc1mp41",
		"phone video":              "\x00\x00\x00\x14ftypqt  \x00\x00\x00\x00qt  ",
		"avif picture":             "\x00\x00\x00\x1cftypavif\x00\x00\x00\x00avifmif1miaf",
		"brand after the box ends": "\x00\x00\x00\x10ftypmif1\x00\x00\x00\x00heic",
		"brand as version number":  "\x00\x00\x00\x14ftypmif1heicmiaf",
		"text":                     "this mentions ftypheic but is a text file",
	} {
		if heicContent([]byte(head)) {
			t.Errorf("%s was taken for HEIC", name)
		}
	}
}

func TestHEICPhotosAreReadThroughWindows(t *testing.T) {
	fake, images := newFake(), &fakeImages{}
	service := heicService(t, fake, images)
	folder := t.TempDir()
	photo := writeHEIC(t, folder, "IMG_0001.HEIC")
	renamed := writeHEIC(t, folder, "renamed by hand.jpg")
	// A JPG with a HEIC name is a JPG. The engine reads it like any other.
	mislabelled := writeInput(t, fake, folder, "really a jpeg.heic", jpegJSON)

	snapshot := addAndWait(t, service, photo, renamed, mislabelled)
	if reads := imageReads(snapshot); !strings.HasSuffix(reads, "TIFF HEIC") {
		t.Errorf("the format table should list HEIC on a PC that reads it: %q", reads)
	}
	for _, name := range []string{"IMG_0001.HEIC", "renamed by hand.jpg"} {
		item := itemNamed(t, snapshot, name)
		if item.Status != StatusReady || item.Kind != KindImage || item.Source != "HEIC" || item.Target != "jpg" ||
			item.Width != 4032 || item.Height != 3024 || !item.Thumb || string(service.Thumbnail(item.ID)) != "jpeg-bytes" {
			t.Errorf("%s: %+v", name, item)
		}
	}
	if item := itemNamed(t, snapshot, "really a jpeg.heic"); item.Source != "JPG" || item.Status != StatusSkipped {
		t.Errorf("a JPG with a HEIC name: %+v", item)
	}
	if log := images.log(); strings.Contains(log, "really a jpeg") || strings.Count(log, "@ 256") != 2 {
		t.Errorf("Windows should have read the two HEIC photos once, small:\n%s", log)
	}
	if left := workFiles(t, service); len(left) != 0 {
		t.Errorf("preview copies were left behind: %v", left)
	}

	snapshot = mustStart(t, service, PolicyAsk)
	if snapshot.Batch.Done != 2 || snapshot.Batch.Total != 2 || snapshot.Batch.Failed != 0 {
		t.Fatalf("unexpected batch: %+v", snapshot.Batch)
	}
	if item := itemNamed(t, snapshot, "IMG_0001.HEIC"); item.Status != StatusDone || item.OutputPath != filepath.Join(folder, "IMG_0001.jpg") {
		t.Errorf("the HEIC photo: %+v", item)
	}
	// The result may not take the place of the photo it was made from.
	if item := itemNamed(t, snapshot, "renamed by hand.jpg"); item.Status != StatusDone || item.OutputName != "renamed by hand (1).jpg" {
		t.Errorf("the renamed HEIC photo: %+v", item)
	}
	for _, path := range []string{photo, renamed} {
		if readFile(t, path) != string(heicBytes) {
			t.Errorf("%s was changed", filepath.Base(path))
		}
	}
	// The engine only ever saw the copies that Windows made.
	fake.mu.Lock()
	for _, call := range fake.calls {
		if input := strings.TrimPrefix(argAfter(call, "-i"), "file:"); input == photo || input == renamed {
			t.Errorf("the engine was pointed at a HEIC photo: %v", call)
		}
	}
	fake.mu.Unlock()
	conversions := fake.conversions()
	if len(conversions) != 2 {
		t.Fatalf("%d conversions, want 2", len(conversions))
	}
	for _, call := range conversions {
		input := strings.TrimPrefix(argAfter(call, "-i"), "file:")
		if filepath.Base(input) != "source.png" || filepath.Dir(filepath.Dir(input)) != filepath.Join(service.dataDir, "work") {
			t.Errorf("converted from %s, want the copy in the work folder", input)
		}
	}
	if log := images.log(); strings.Count(log, "source.png @ 0") != 2 {
		t.Errorf("each photo should be read once in full for converting:\n%s", log)
	}
	if left := workFiles(t, service); len(left) != 0 {
		t.Errorf("copies of the photos were left behind: %v", left)
	}
}

func TestHEICWithoutTheWindowsCodecsIsExplained(t *testing.T) {
	fake, images := newFake(), &fakeImages{missing: true}
	service := heicService(t, fake, images)
	folder := t.TempDir()

	snapshot := addAndWait(t, service, writeHEIC(t, folder, "first.heic"), writeInput(t, fake, folder, "drawing.png", imageJSON))
	item := itemNamed(t, snapshot, "first.heic")
	if item.Status != StatusUnsupported || item.Error != heicNeeds || item.Source != "HEIC" || item.Kind != KindImage {
		t.Fatalf("without the codecs: %+v", item)
	}
	if reads := imageReads(snapshot); strings.Contains(reads, "HEIC") {
		t.Errorf("the format table promises HEIC on a PC that cannot read it: %q", reads)
	}
	if len(snapshot.Kinds) != 1 || snapshot.Kinds[0].Count != 1 {
		t.Errorf("a photo that cannot be read does not count for the format picker: %+v", snapshot.Kinds)
	}
	snapshot = mustStart(t, service, PolicyAsk)
	if snapshot.Batch.Total != 1 || itemNamed(t, snapshot, "first.heic").Status != StatusUnsupported {
		t.Errorf("the unreadable photo took part in the batch: %+v", snapshot.Batch)
	}

	// The owner of the PC adds the codecs. The next photo works without a restart.
	images.set(func() { images.missing = false })
	snapshot = addAndWait(t, service, writeHEIC(t, folder, "second.heif"))
	if item := itemNamed(t, snapshot, "second.heif"); item.Status != StatusReady || item.Source != "HEIC" {
		t.Fatalf("after the codecs were added: %+v", item)
	}
	if reads := imageReads(snapshot); !strings.Contains(reads, "HEIC") {
		t.Errorf("the format table should list HEIC now: %q", reads)
	}
}

func TestAHEICPhotoThatWindowsCannotReadIsMarked(t *testing.T) {
	fake, images := newFake(), &fakeImages{broken: map[string]bool{"damaged.heic": true}}
	service := heicService(t, fake, images)
	folder := t.TempDir()
	snapshot := addAndWait(t, service, writeHEIC(t, folder, "damaged.heic"), writeHEIC(t, folder, "fine.heic"))
	item := itemNamed(t, snapshot, "damaged.heic")
	if item.Status != StatusUnsupported || item.Error != heicUnreadable || !strings.Contains(item.Detail, "0x88982F50") {
		t.Fatalf("a photo Windows cannot read: %+v", item)
	}

	// The second photo goes bad between listing and converting.
	images.set(func() { images.broken["fine.heic"] = true })
	snapshot = mustStart(t, service, PolicyAsk)
	item = itemNamed(t, snapshot, "fine.heic")
	if item.Status != StatusFailed || item.Error != heicUnreadable || item.Detail == "" || snapshot.Batch.Failed != 1 {
		t.Fatalf("a photo that fails while converting: %+v", item)
	}
	if len(fake.conversions()) != 0 {
		t.Error("the engine was started although there was nothing to convert")
	}
	entries, _ := os.ReadDir(folder)
	if len(entries) != 2 {
		t.Errorf("the folder has %d entries, want only the two photos", len(entries))
	}
	if left := workFiles(t, service); len(left) != 0 {
		t.Errorf("work files were left behind: %v", left)
	}
}
