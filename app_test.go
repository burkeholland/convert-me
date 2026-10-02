package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"convertme/internal/convert"
)

func TestAbsolutePathsIgnoresOptionsAndResolvesNames(t *testing.T) {
	folder := t.TempDir()
	got := absolutePaths([]string{
		"photo.jpg", "  ", "-flag", "--help", `C:\Videos\clip.mov`, `sub\..\song.mp3`, "",
	}, folder)
	want := []string{filepath.Join(folder, "photo.jpg"), `C:\Videos\clip.mov`, filepath.Join(folder, "song.mp3")}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v", got, want)
	}
	// Without a working directory a relative name cannot be trusted, so it is dropped.
	if got := absolutePaths([]string{"photo.jpg", `C:\a.png`}, ""); len(got) != 1 || got[0] != `C:\a.png` {
		t.Errorf("got %v", got)
	}
	// A dropped file whose name starts with a dash is still a full path, and is kept.
	dashed := filepath.Join(folder, "-i %d.png")
	if got := absolutePaths([]string{dashed}, ""); len(got) != 1 || got[0] != dashed {
		t.Errorf("got %v", got)
	}
}

func TestAppPathsOnlyAcceptsAbsoluteOverrides(t *testing.T) {
	runtimeDir, dataDir := t.TempDir(), t.TempDir()
	t.Setenv("CONVERTME_RUNTIME_DIR", runtimeDir)
	t.Setenv("CONVERTME_DATA_DIR", dataDir)
	root, data, err := appPaths()
	if err != nil || root != runtimeDir || data != dataDir {
		t.Fatalf("overrides were not used: %q %q %v", root, data, err)
	}
	t.Setenv("CONVERTME_RUNTIME_DIR", `relative\runtime`)
	if _, _, err := appPaths(); err == nil {
		t.Error("a relative runtime folder must be refused")
	}
	t.Setenv("CONVERTME_RUNTIME_DIR", "")
	t.Setenv("CONVERTME_DATA_DIR", `relative\data`)
	if _, _, err := appPaths(); err == nil {
		t.Error("a relative data folder must be refused")
	}
	t.Setenv("CONVERTME_DATA_DIR", "")
	t.Setenv("LOCALAPPDATA", `C:\Users\someone\AppData\Local`)
	root, data, err = appPaths()
	exe, _ := os.Executable()
	if err != nil || root != filepath.Dir(exe) || data != `C:\Users\someone\AppData\Local\ConvertMe` {
		t.Errorf("defaults are off: %q %q %v", root, data, err)
	}
}

func TestThumbnailHandlerServesNothingElse(t *testing.T) {
	app := NewApp(t.TempDir(), t.TempDir(), nil)
	handler := app.thumbnailHandler()
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/thumb/"},
		{http.MethodGet, "/thumb/unknown"},
		{http.MethodGet, "/thumb/../../settings.json"},
		{http.MethodGet, "/settings.json"},
		{http.MethodGet, "/"},
		{http.MethodPost, "/thumb/unknown"},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(request.method, request.path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s %s answered %d", request.method, request.path, recorder.Code)
		}
	}
}

func TestStatusExplainsAStartupFailure(t *testing.T) {
	app := NewApp(t.TempDir(), t.TempDir(), nil)
	app.initErr = errors.New("CONVERTME_DATA_DIR must be an absolute path")
	snapshot := app.Status()
	if snapshot.State != convert.StateFailed || snapshot.SetupError == "" || snapshot.Items == nil || snapshot.Kinds == nil || len(snapshot.Formats) != 3 {
		t.Fatalf("the window needs a complete answer even when start-up failed: %+v", snapshot)
	}
}

func TestFilesNamedBeforeTheEngineIsReadyAreKept(t *testing.T) {
	app := NewApp(t.TempDir(), t.TempDir(), []string{`C:\first.png`})
	app.receive([]string{`C:\second.png`})
	app.receive(nil)
	if strings.Join(app.pending, "|") != `C:\first.png|C:\second.png` {
		t.Errorf("files handed over during start-up were lost: %v", app.pending)
	}
}

func TestOpenFolderActionsFailClearly(t *testing.T) {
	app := NewApp(t.TempDir(), t.TempDir(), nil)
	if err := app.OpenOutputFolder(); err == nil {
		t.Error("with nothing converted there is no folder to open")
	}
	if err := app.RevealItem("unknown"); err == nil {
		t.Error("an unknown item cannot be shown")
	}
	if err := app.OpenLicenses(); err == nil {
		t.Error("a missing licenses folder should be reported")
	}
	if err := showInFolder(filepath.Join(t.TempDir(), "gone"), nil); err == nil || !strings.Contains(err.Error(), "no longer there") {
		t.Errorf("a folder that disappeared should be reported: %v", err)
	}
}
