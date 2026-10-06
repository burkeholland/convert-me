package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeList(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestLaunchPathsReadsAFileListAndRemovesIt(t *testing.T) {
	folder := t.TempDir()
	odd := `C:\Media\Übung #1 & notes (final) 写真.webp`
	list := writeList(t, "convertme-files-1-2-3.txt",
		"\xef\xbb\xbf"+listHeader+"\r\n"+
			`C:\Media\a.png`+"\r\n"+
			"\r\n"+
			`relative\b.png`+"\n"+
			odd+"\n"+
			`C:\Media\sub\..\c.mp4`+"\n"+
			`\\server\share\d.mov`)

	got := launchPaths([]string{"first.jpg", listFlag, list, `C:\last.wav`}, folder)
	want := []string{
		filepath.Join(folder, "first.jpg"), `C:\Media\a.png`, odd, `C:\Media\c.mp4`, `\\server\share\d.mov`, `C:\last.wav`,
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
	if exists(list) {
		t.Error("a file list that was read must be removed")
	}

	// The same list given as one argument.
	list = writeList(t, "ConvertMe-Files-x.TXT", listHeader+"\n"+`C:\Media\a.png`+"\n")
	if got := launchPaths([]string{listFlag + "=" + list}, ""); len(got) != 1 || got[0] != `C:\Media\a.png` {
		t.Errorf("got %q", got)
	}
	if exists(list) {
		t.Error("a file list that was read must be removed")
	}
}

func TestLaunchPathsLeavesOtherFilesAlone(t *testing.T) {
	valid := listHeader + "\n" + `C:\Media\a.png` + "\n"
	cases := map[string]string{
		"wrong name":   writeList(t, "important.txt", valid),
		"wrong ending": writeList(t, "convertme-files-1.log", valid),
		"no header":    writeList(t, "convertme-files-2.txt", `C:\Media\a.png`+"\n"),
		"other header": writeList(t, "convertme-files-3.txt", "Convert Me file list 2\n"+`C:\Media\a.png`+"\n"),
		"empty":        writeList(t, "convertme-files-4.txt", ""),
		"too large":    writeList(t, "convertme-files-5.txt", valid+strings.Repeat("x", maxListBytes)),
	}
	for name, path := range cases {
		if got := launchPaths([]string{listFlag, path}, ""); len(got) != 0 {
			t.Errorf("%s: got %q, want nothing", name, got)
		}
		if !exists(path) {
			t.Errorf("%s: a file that is not a file list must not be removed", name)
		}
	}

	// A list that is named without its full path is not read either.
	relative := writeList(t, "convertme-files-6.txt", valid)
	t.Chdir(filepath.Dir(relative))
	if got := launchPaths([]string{listFlag, filepath.Base(relative)}, filepath.Dir(relative)); len(got) != 0 {
		t.Errorf("got %q, want nothing", got)
	}
	if !exists(relative) {
		t.Error("a list named without its full path must not be removed")
	}

	// The flag without a name, and a list that does not exist, are ignored.
	if got := launchPaths([]string{`C:\a.png`, listFlag}, ""); len(got) != 1 {
		t.Errorf("got %q", got)
	}
	if got := launchPaths([]string{listFlag, `C:\nowhere\convertme-files-7.txt`, `C:\a.png`}, ""); len(got) != 1 {
		t.Errorf("got %q", got)
	}
}

func TestLaunchPathsSkipsBrokenLinesAndStopsAtTheLimit(t *testing.T) {
	list := writeList(t, "convertme-files-8.txt",
		listHeader+"\n"+`C:\ok.png`+"\n"+"C:\\bad\xff.png\n"+"C:\\nul\x00.png\n"+`not a path`+"\n"+`C:\also-ok.png`+"\n")
	if got := launchPaths([]string{listFlag, list}, ""); strings.Join(got, "|") != `C:\ok.png|C:\also-ok.png` {
		t.Errorf("got %q", got)
	}

	var many strings.Builder
	many.WriteString(listHeader + "\n")
	for i := 0; i < maxListPaths+10; i++ {
		many.WriteString(`C:\Media\photo.png` + "\n")
	}
	list = writeList(t, "convertme-files-9.txt", many.String())
	if got := launchPaths([]string{listFlag, list}, ""); len(got) != maxListPaths {
		t.Errorf("got %d paths, want %d", len(got), maxListPaths)
	}
}

// The build of the test package sets these two variables: the first names a list exactly
// as the File Explorer command wrote it, the second the paths it was asked to write.
func TestReadsTheListTheShellExtensionWrote(t *testing.T) {
	written, expected := os.Getenv("CONVERTME_TEST_SHELL_LIST"), os.Getenv("CONVERTME_TEST_SHELL_PATHS")
	if written == "" || expected == "" {
		t.Skip("set CONVERTME_TEST_SHELL_LIST and CONVERTME_TEST_SHELL_PATHS to run this test")
	}
	data, err := os.ReadFile(written)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(expected)
	if err != nil {
		t.Fatal(err)
	}
	// Work on a copy, because reading a list removes it.
	list := writeList(t, "convertme-files-from-the-shell-extension.txt", string(data))
	got := strings.Join(launchPaths([]string{listFlag, list}, ""), "\n")
	if wantPaths := strings.TrimSpace(strings.ReplaceAll(string(want), "\r\n", "\n")); got != wantPaths {
		t.Errorf("the app read\n%s\nbut the shell extension was given\n%s", got, wantPaths)
	}
}
