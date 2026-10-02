package convert

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestStemKeepsNamesReadableAndValid(t *testing.T) {
	for name, want := range map[string]string{
		"photo.jpg":             "photo",
		"archive.tar.gz":        "archive.tar",
		"no extension":          "no extension",
		".hidden":               ".hidden",
		"trailing dot..png":     "trailing dot.",
		"Ünïcödé 音楽 🎵.wav":      "Ünïcödé 音楽 🎵",
		"100% done (final).MP4": "100% done (final)",
	} {
		if got := stem(name); got != want {
			t.Errorf("stem(%q) = %q, want %q", name, got, want)
		}
	}
	long := strings.Repeat("a", 300) + ".png"
	if got := stem(long); len(got) != maxStemUnits {
		t.Errorf("a very long name should be cut to %d characters, got %d", maxStemUnits, len(got))
	}
	// An emoji takes two UTF-16 units. The cut must not split it in half.
	emoji := strings.Repeat("a", maxStemUnits-1) + "🎵🎵.png"
	cut := stem(emoji)
	if units := utf16.Encode([]rune(cut)); len(units) > maxStemUnits || strings.ContainsRune(cut, '\uFFFD') {
		t.Errorf("the cut split a character: %d units, %q", len(units), cut[len(cut)-8:])
	}
	if got := fitStem(strings.Repeat("a", maxStemUnits-3) + " . ...extra"); strings.HasSuffix(got, " ") || strings.HasSuffix(got, ".") {
		t.Errorf("Windows does not allow names that end in a space or a dot: %q", got)
	}
}

func TestCandidateAndPartialNames(t *testing.T) {
	if candidateName("photo", ".jpg", 0) != "photo.jpg" || candidateName("photo", ".jpg", 3) != "photo (3).jpg" {
		t.Error("numbered names are off")
	}
	first, second := partialPath(`C:\out`, "photo", ".jpg"), partialPath(`C:\out`, "photo", ".jpg")
	if first == second {
		t.Error("two temporary names must differ")
	}
	name := filepath.Base(first)
	if filepath.Dir(first) != `C:\out` || !strings.HasPrefix(name, "photo.jpg.") || !strings.HasSuffix(name, partialSuffix) {
		t.Errorf("unexpected temporary name: %s", first)
	}
	if KnownExtension(first) {
		t.Error("a temporary file must never be mistaken for a media file")
	}
	if pathKey(`C:\Out\Photo.JPG`) != pathKey(`c:\out\.\photo.jpg`) {
		t.Error("Windows paths are compared without regard to case")
	}
}

func writeTemp(t *testing.T, folder, content string) string {
	t.Helper()
	path := partialPath(folder, "result", ".jpg")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func nothingProtected(string) bool { return false }

func TestPlaceNeverReplacesWithoutPermission(t *testing.T) {
	folder := t.TempDir()
	existing := filepath.Join(folder, "result.jpg")
	os.WriteFile(existing, []byte("older"), 0600)
	taken := nameSet{}

	final, err := place(writeTemp(t, folder, "first"), folder, "result", ".jpg", false, taken, nothingProtected)
	if err != nil || filepath.Base(final) != "result (1).jpg" {
		t.Fatalf("expected a numbered name, got %q %v", final, err)
	}
	if readFile(t, existing) != "older" || readFile(t, final) != "first" {
		t.Fatal("the existing file was touched")
	}
	final, err = place(writeTemp(t, folder, "second"), folder, "result", ".jpg", false, taken, nothingProtected)
	if err != nil || filepath.Base(final) != "result (2).jpg" {
		t.Fatalf("expected the next number, got %q %v", final, err)
	}

	// With permission the first choice is replaced, exactly once per batch.
	replaced := nameSet{}
	final, err = place(writeTemp(t, folder, "replacement"), folder, "result", ".jpg", true, replaced, nothingProtected)
	if err != nil || final != existing || readFile(t, existing) != "replacement" {
		t.Fatalf("replace should overwrite the first choice: %q %v", final, err)
	}
	final, err = place(writeTemp(t, folder, "another"), folder, "result", ".jpg", true, replaced, nothingProtected)
	if err != nil || final == existing || readFile(t, existing) != "replacement" {
		t.Fatalf("a second file in the same batch must not replace the first one's result: %q %v", final, err)
	}
	if filepath.Base(final) != "result (3).jpg" {
		t.Errorf("numbered files are never replaced either, got %s", filepath.Base(final))
	}
	if left := partials(t, folder); len(left) != 0 {
		t.Errorf("temporary files were left behind: %v", left)
	}
}

func TestPlaceProtectsOriginalsEvenWithPermission(t *testing.T) {
	folder := t.TempDir()
	original := filepath.Join(folder, "result.jpg")
	os.WriteFile(original, []byte("the original"), 0600)
	protected := func(path string) bool { return pathKey(path) == pathKey(original) }

	final, err := place(writeTemp(t, folder, "converted"), folder, "result", ".jpg", true, nameSet{}, protected)
	if err != nil || final == original || readFile(t, original) != "the original" {
		t.Fatalf("an original was replaced: %q %v", final, err)
	}
	if filepath.Base(final) != "result (1).jpg" {
		t.Errorf("expected a numbered name next to the original, got %s", filepath.Base(final))
	}
}

func TestPlaceReportsAFolderItCannotWriteTo(t *testing.T) {
	folder := t.TempDir()
	temp := writeTemp(t, folder, "converted")
	missing := filepath.Join(folder, "gone")
	if _, err := place(temp, missing, "result", ".jpg", false, nameSet{}, nothingProtected); err == nil || !strings.Contains(err.Error(), "could not be saved") {
		t.Fatalf("expected a clear error, got %v", err)
	}
	if _, err := os.Stat(temp); err != nil {
		t.Error("the temporary file should still be there for the caller to clean up")
	}
}

func TestMoveFileRefusesToReplaceUnlessTold(t *testing.T) {
	folder := t.TempDir()
	from, to := filepath.Join(folder, "from"), filepath.Join(folder, "to")
	os.WriteFile(from, []byte("new"), 0600)
	os.WriteFile(to, []byte("old"), 0600)
	err := moveFile(from, to, false)
	if err == nil || !isAlreadyExists(err) {
		t.Fatalf("expected an already-exists error, got %v", err)
	}
	if readFile(t, to) != "old" || readFile(t, from) != "new" {
		t.Fatal("a refused move changed a file")
	}
	if err := moveFile(from, to, true); err != nil || readFile(t, to) != "new" {
		t.Fatalf("an allowed replace failed: %v", err)
	}

	// Paths beyond the classic 260 character limit must work too.
	deep := folder
	for len(deep) < 300 {
		deep = filepath.Join(deep, strings.Repeat("d", 40))
	}
	if err := os.MkdirAll(deep, 0700); err != nil {
		t.Fatal(err)
	}
	long, moved := filepath.Join(deep, "a.jpg"), filepath.Join(deep, "b.jpg")
	os.WriteFile(long, []byte("deep"), 0600)
	if err := moveFile(long, moved, false); err != nil || readFile(t, moved) != "deep" {
		t.Fatalf("a long path could not be moved: %v", err)
	}
}

func TestInputSetSeesTheSameFileUnderAnotherName(t *testing.T) {
	folder := t.TempDir()
	original := filepath.Join(folder, "Photo.jpg")
	os.WriteFile(original, []byte("original"), 0600)
	other := filepath.Join(folder, "other.jpg")
	os.WriteFile(other, []byte("other"), 0600)
	service := NewService(t.TempDir(), t.TempDir(), newFake().run)
	t.Cleanup(service.Close)
	service.items = []*Item{{ID: "1", Path: original}}
	inputs := service.inputs()

	if !inputs.protects(filepath.Join(folder, "PHOTO.JPG")) || !inputs.protects(filepath.Join(folder, ".", "photo.jpg")) {
		t.Error("another spelling of the same path must be protected")
	}
	link := filepath.Join(folder, "link.jpg")
	if err := os.Link(original, link); err == nil && !inputs.protects(link) {
		t.Error("a hard link to an original is the same file and must be protected")
	}
	if inputs.protects(other) || inputs.protects(filepath.Join(folder, "missing.jpg")) {
		t.Error("unrelated files are not protected")
	}
}

func TestChildEnvironmentDropsEngineSwitches(t *testing.T) {
	kept := childEnvironment([]string{"PATH=C:\\Windows", "FFREPORT=file=C\\:/x.log", "ffreport=level=32", "AV_LOG_FORCE_COLOR=1", "TEMP=C:\\Temp", "OFFICE=1", "FFMPEG_DATADIR=x"})
	if got := strings.Join(kept, "|"); got != "PATH=C:\\Windows|TEMP=C:\\Temp|OFFICE=1" {
		t.Errorf("unexpected environment: %s", got)
	}
}

func TestBoundedOutputKeepsMemoryInCheck(t *testing.T) {
	head := &boundedOutput{limit: 10, head: true}
	head.Write([]byte("12345678"))
	head.Write([]byte("90abcdef"))
	if string(head.data) != "1234567890" || !head.overflow {
		t.Errorf("head mode keeps the first bytes and reports overflow: %q %v", head.data, head.overflow)
	}
	tail := &boundedOutput{limit: 10}
	tail.Write([]byte("12345678"))
	tail.Write([]byte("90abcdef"))
	if string(tail.data) != "7890abcdef" {
		t.Errorf("tail mode keeps the last bytes: %q", tail.data)
	}
	var lines []string
	stream := &boundedOutput{onLine: func(line string) { lines = append(lines, line) }}
	stream.Write([]byte("out_time_us=1\nprog"))
	stream.Write([]byte("ress=continue\r\n\r\nout_time_us=2\n"))
	if strings.Join(lines, "|") != "out_time_us=1|progress=continue|out_time_us=2" || len(stream.data) != 0 {
		t.Errorf("lines were not split correctly: %v", lines)
	}
	stream.Write(make([]byte, maxErrorTail+1))
	if len(stream.pending) != 0 {
		t.Error("an endless line must not be kept in memory")
	}
}

func TestProcessErrorKeepsTheEngineText(t *testing.T) {
	base := errors.New("exit status 1")
	failure := &ProcessError{Err: base, Stderr: "moov atom not found"}
	if failure.Error() != "exit status 1: moov atom not found" || !errors.Is(failure, base) {
		t.Errorf("unexpected error text: %v", failure)
	}
	if (&ProcessError{Err: base}).Error() != "exit status 1" {
		t.Error("an empty engine message adds nothing")
	}
}

func TestRunProcessStartsOnlyTheGivenProgram(t *testing.T) {
	// "where.exe" is always present and exits with an error for a name it cannot find.
	where := filepath.Join(os.Getenv("SystemRoot"), "System32", "where.exe")
	if _, err := os.Stat(where); err != nil {
		t.Skip("where.exe is not available")
	}
	output, err := RunProcess(context.Background(), where, []string{"cmd.exe"}, nil)
	if err != nil || !strings.Contains(strings.ToLower(string(output)), "cmd.exe") {
		t.Fatalf("expected the path of cmd.exe, got %q %v", output, err)
	}
	var failure *ProcessError
	// The argument looks like a shell command. It must arrive as one plain argument.
	_, err = RunProcess(context.Background(), where, []string{"nothing & echo injected"}, nil)
	if !errors.As(err, &failure) || strings.Contains(failure.Stderr, "injected\n") {
		t.Fatalf("expected a plain failure, got %v", err)
	}
	if _, err := RunProcess(context.Background(), filepath.Join(t.TempDir(), "missing.exe"), nil, nil); err == nil {
		t.Fatal("a missing program must fail")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunProcess(cancelled, where, []string{"cmd.exe"}, nil); err == nil {
		t.Fatal("a cancelled run must not report success")
	}
}
