package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"convertme/internal/convert"
)

//go:embed assets/runtime-manifest.json
var runtimeManifest []byte

// maxOpenedFolders limits how many Explorer windows one click can open.
const maxOpenedFolders = 4

// App is the bridge between the window and the conversion service.
type App struct {
	ctx      context.Context
	service  *convert.Service
	root     string
	initErr  error
	initDone chan struct{}
	dialogMu sync.Mutex

	// Files named on the command line can arrive before the engine has been checked.
	pendingMu sync.Mutex
	ready     bool
	pending   []string
}

func NewApp(root, dataDir string, launchPaths []string) *App {
	return &App{
		service:  convert.NewService(root, dataDir, convert.RunProcess),
		root:     root,
		initDone: make(chan struct{}),
		pending:  launchPaths,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go func() {
		defer close(a.initDone)
		if a.initErr != nil {
			runtime.LogError(ctx, a.initErr.Error())
			return
		}
		if err := a.service.Initialize(runtimeManifest); err != nil {
			runtime.LogError(ctx, err.Error())
			return
		}
		a.pendingMu.Lock()
		a.ready = true
		paths := a.pending
		a.pending = nil
		a.pendingMu.Unlock()
		a.announce(paths)
	}()
}

func (a *App) shutdown(context.Context) {
	<-a.initDone
	a.service.Close()
}

func (a *App) beforeClose(ctx context.Context) bool {
	if !a.service.Busy() {
		return false
	}
	a.dialogMu.Lock()
	defer a.dialogMu.Unlock()
	answer, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type: runtime.QuestionDialog, Title: "Stop converting and close?",
		Message: "A conversion is still running. Closing stops it and removes the unfinished file. " +
			"Files that are already converted are kept, and your originals are not changed.",
		Buttons: []string{"Yes", "No"}, DefaultButton: "No", CancelButton: "No",
	})
	if err != nil {
		runtime.LogError(ctx, err.Error())
		return true
	}
	return answer != "Yes"
}

// secondInstance runs when the app is started again while it is open, for example by
// "Open with" in Explorer. The files go to the window that already exists.
func (a *App) secondInstance(data options.SecondInstanceData) {
	if a.ctx != nil {
		runtime.WindowUnminimise(a.ctx)
		runtime.WindowShow(a.ctx)
	}
	a.receive(absolutePaths(data.Args, data.WorkingDirectory))
}

// absolutePaths turns command-line arguments into full paths. Anything that looks like
// an option is ignored. Whether a path is a usable file is decided later.
func absolutePaths(args []string, workingDirectory string) []string {
	var paths []string
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		if !filepath.IsAbs(arg) {
			if workingDirectory == "" {
				continue
			}
			arg = filepath.Join(workingDirectory, arg)
		}
		paths = append(paths, filepath.Clean(arg))
	}
	return paths
}

func (a *App) receive(paths []string) {
	if len(paths) == 0 {
		return
	}
	a.pendingMu.Lock()
	if !a.ready {
		a.pending = append(a.pending, paths...)
		a.pendingMu.Unlock()
		return
	}
	a.pendingMu.Unlock()
	a.announce(paths)
}

// announce adds files that did not come from the window itself and tells the window what
// happened, because nobody was there to see a dialog.
func (a *App) announce(paths []string) {
	if len(paths) == 0 {
		return
	}
	result, err := a.service.AddFiles(paths)
	switch {
	case err != nil:
		a.service.Announce(convert.Sentence(err.Error()), true)
	case len(result.Skipped) > 0:
		a.service.Announce(convert.SkippedSummary(result), result.Added == 0)
	}
}

func (a *App) Status() convert.Snapshot {
	if a.initErr != nil {
		return convert.Snapshot{
			Version: convert.Version, Engine: "FFmpeg", State: convert.StateFailed,
			SetupError:  a.initErr.Error(),
			Items:       []convert.Item{},
			Kinds:       []convert.KindState{},
			Destination: convert.DestinationState{Mode: convert.DestinationSource},
			Batch:       convert.BatchState{State: convert.BatchIdle},
			Formats:     convert.FormatTable(convert.Capabilities{}),
		}
	}
	return a.service.Status()
}

// AddFiles receives paths dropped on the window.
func (a *App) AddFiles(paths []string) (convert.AddResult, error) {
	return a.service.AddFiles(absolutePaths(paths, ""))
}

// ChooseFiles opens the native file dialog.
func (a *App) ChooseFiles() (convert.AddResult, error) {
	a.dialogMu.Lock()
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose files to convert",
		Filters: []runtime.FileFilter{
			{DisplayName: "Images, audio and video", Pattern: convert.DialogPattern()},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
	a.dialogMu.Unlock()
	if err != nil || len(paths) == 0 {
		return convert.AddResult{Skipped: []convert.Skipped{}}, err
	}
	return a.service.AddFiles(paths)
}

func (a *App) RemoveItem(id string) error {
	return a.service.RemoveItem(id)
}

func (a *App) ClearItems() error {
	return a.service.ClearItems()
}

func (a *App) SetTarget(kind, target string) error {
	return a.service.SetTarget(convert.Kind(kind), target)
}

// ChooseDestination opens the native folder dialog. It reports false when it was dismissed.
func (a *App) ChooseDestination() (bool, error) {
	options := runtime.OpenDialogOptions{Title: "Choose where to save converted files"}
	if folder := a.service.Status().Destination.Folder; folder != "" {
		if info, err := os.Stat(folder); err == nil && info.IsDir() {
			options.DefaultDirectory = folder
		}
	}
	a.dialogMu.Lock()
	folder, err := runtime.OpenDirectoryDialog(a.ctx, options)
	a.dialogMu.Unlock()
	if err != nil || folder == "" {
		return false, err
	}
	return true, a.service.SetDestination(convert.DestinationFolder, folder)
}

// UseSourceFolder saves each converted file next to its original.
func (a *App) UseSourceFolder() error {
	return a.service.SetDestination(convert.DestinationSource, "")
}

// UseChosenFolder goes back to the folder that was chosen earlier.
func (a *App) UseChosenFolder() error {
	return a.service.SetDestination(convert.DestinationFolder, a.service.Status().Destination.Folder)
}

func (a *App) Start(policy string) (convert.StartResult, error) {
	return a.service.Start(policy)
}

func (a *App) Cancel() error {
	return a.service.Cancel()
}

// OpenOutputFolder shows the converted files in Explorer, selected.
func (a *App) OpenOutputFolder() error {
	folders, files := a.service.Outputs()
	if len(folders) == 0 {
		return errors.New("there are no converted files to show yet")
	}
	if len(folders) > maxOpenedFolders {
		folders = folders[:maxOpenedFolders]
	}
	var failures []error
	for _, folder := range folders {
		if err := showInFolder(folder, files[folder]); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// RevealItem shows one converted file in Explorer.
func (a *App) RevealItem(id string) error {
	path := a.service.OutputPath(id)
	if path == "" {
		return errors.New("this file has not been converted yet")
	}
	return showInFolder(filepath.Dir(path), []string{path})
}

// OpenLicenses opens the folder with the license texts that ship with the app.
func (a *App) OpenLicenses() error {
	for _, folder := range []string{filepath.Join(a.root, "licenses"), filepath.Join(a.root, "notices")} {
		if info, err := os.Stat(folder); err == nil && info.IsDir() {
			return openFolder(folder)
		}
	}
	return errors.New("the licenses folder was not found next to the app")
}

func (a *App) MinimiseWindow() {
	runtime.WindowMinimise(a.ctx)
}

func (a *App) ToggleMaximiseWindow() {
	runtime.WindowToggleMaximise(a.ctx)
}

func (a *App) CloseWindow() {
	runtime.Quit(a.ctx)
}

// thumbnailHandler serves the small previews kept in memory. Nothing else is reachable
// through it, and the previews are never cached on disk.
func (a *App) thumbnailHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := strings.CutPrefix(r.URL.Path, "/thumb/")
		if !ok || id == "" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		data := a.service.Thumbnail(id)
		if len(data) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
}

// showInFolder opens a folder with files selected, and falls back to just opening it.
func showInFolder(folder string, files []string) error {
	info, err := os.Stat(folder)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("the folder %s is no longer there", folder)
	}
	var existing []string
	for _, file := range files {
		if _, err := os.Stat(file); err == nil {
			existing = append(existing, file)
		}
	}
	if len(existing) > 0 && revealFiles(folder, existing) == nil {
		return nil
	}
	return openFolder(folder)
}

func appPaths() (root, data string, err error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	root = filepath.Dir(exe)
	if override := os.Getenv("CONVERTME_RUNTIME_DIR"); override != "" {
		if !filepath.IsAbs(override) {
			return "", "", errors.New("CONVERTME_RUNTIME_DIR must be an absolute path")
		}
		root = override
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, err = os.UserConfigDir()
		if err != nil {
			return "", "", err
		}
	}
	data = filepath.Join(base, "ConvertMe")
	if override := os.Getenv("CONVERTME_DATA_DIR"); override != "" {
		if !filepath.IsAbs(override) {
			return "", "", errors.New("CONVERTME_DATA_DIR must be an absolute path")
		}
		data = override
	}
	return root, data, nil
}
