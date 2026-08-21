package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx        context.Context
	launch     LaunchRequest
	settingsMu sync.RWMutex
	settings   Settings
	jobMu      sync.Mutex
	cancel     context.CancelFunc
}

func NewApp(launch LaunchRequest) *App {
	return &App{
		launch:   launch,
		settings: defaultSettings(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	settings, err := loadSettings()
	if err != nil {
		fmt.Println("ConvertMe settings could not be loaded:", err)
		settings = defaultSettings()
	}
	a.settingsMu.Lock()
	a.settings = settings
	a.settingsMu.Unlock()

	executable, err := shellExecutable()
	if err == nil && executable != "" {
		if settings.ExplorerIntegration {
			if err := RegisterContextMenu(executable); err != nil {
				fmt.Println("ConvertMe Explorer integration could not be registered:", err)
			}
		} else {
			if err := UnregisterContextMenu(); err != nil {
				fmt.Println("ConvertMe Explorer integration could not be unregistered:", err)
			}
		}
		if err := setLaunchAtLogin(settings.LaunchAtLogin, executable); err != nil {
			fmt.Println("ConvertMe launch-at-login could not be updated:", err)
		}
	}
}

func (a *App) domReady(context.Context) {
	if a.launch.Mode == "" || a.launch.Mode == "tray" {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.EventsEmit(a.ctx, "launch:request", a.launch)
}

func (a *App) shutdown(context.Context) {
	a.jobMu.Lock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.jobMu.Unlock()
	if a.launch.Mode != "convert" {
		quitTray()
	}
}

func (a *App) GetSettings() Settings {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return a.settings
}

func (a *App) SaveSettings(settings Settings) error {
	settings = normalizeSettings(settings)
	if err := saveSettings(settings); err != nil {
		return err
	}
	executable, err := shellExecutable()
	if err != nil {
		return err
	}
	if settings.ExplorerIntegration {
		if err := RegisterContextMenu(executable); err != nil {
			return err
		}
	} else {
		if err := UnregisterContextMenu(); err != nil {
			return err
		}
	}
	if err := setLaunchAtLogin(settings.LaunchAtLogin, executable); err != nil {
		return err
	}
	a.settingsMu.Lock()
	a.settings = settings
	a.settingsMu.Unlock()
	return nil
}

func (a *App) GetLaunchRequest() LaunchRequest {
	return a.launch
}

func (a *App) GetFormats() []FormatOption {
	return SupportedFormats()
}

// GetFileSizes returns the size in bytes for each given path. Paths that cannot
// be stat'd are reported as -1 so the frontend can omit them gracefully.
func (a *App) GetFileSizes(paths []string) []int64 {
	sizes := make([]int64, len(paths))
	for i, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			sizes[i] = -1
			continue
		}
		sizes[i] = info.Size()
	}
	return sizes
}

func (a *App) SelectFiles() ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose images to convert",
		Filters: []runtime.FileFilter{{
			DisplayName: "Supported images",
			Pattern:     "*.png;*.jpg;*.jpeg;*.bmp;*.tif;*.tiff;*.webp;*.heic;*.heif",
		}},
	})
}

func (a *App) PreviewFile(path string) (string, error) {
	if !IsSupportedInput(path) {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedInput, filepath.Ext(path))
	}
	return previewDataURL(path)
}

func (a *App) StartConversion(files []string, options ConversionOptions) error {
	a.settingsMu.RLock()
	settings := a.settings
	a.settingsMu.RUnlock()
	files, options, err := prepareConversion(files, options, settings)
	if err != nil {
		return err
	}

	a.jobMu.Lock()
	if a.cancel != nil {
		a.jobMu.Unlock()
		return errors.New("a conversion is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.jobMu.Unlock()

	runtime.EventsEmit(a.ctx, "conversion:started", ConversionSummary{Total: len(files)})
	go a.runConversion(ctx, files, options)
	return nil
}

func (a *App) runConversion(ctx context.Context, files []string, options ConversionOptions) {
	start := time.Now()
	summary := convertBatch(ctx, files, options, func(result FileResult) {
		runtime.EventsEmit(a.ctx, "conversion:file", result)
	})
	runtime.EventsEmit(a.ctx, "conversion:complete", summary)
	fmt.Printf("ConvertMe processed %d file(s) in %dms\n", len(files), elapsedSince(start))

	a.jobMu.Lock()
	a.cancel = nil
	a.jobMu.Unlock()
}

func (a *App) CancelConversion() {
	a.jobMu.Lock()
	defer a.jobMu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *App) ShowWindow() error {
	if a.ctx == nil {
		return errors.New("application is not ready")
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
	return nil
}

func (a *App) OpenSettings() error {
	if err := a.ShowWindow(); err != nil {
		return err
	}
	runtime.EventsEmit(a.ctx, "view:settings")
	return nil
}

func (a *App) HideWindow() {
	if a.ctx != nil {
		runtime.WindowHide(a.ctx)
	}
}

func (a *App) Quit() error {
	if a.ctx == nil {
		return errors.New("application is not ready")
	}
	runtime.Quit(a.ctx)
	return nil
}

func (a *App) OpenCodecStore() error {
	return browser.OpenURL(codecStoreURL())
}

func (a *App) RegisterExplorerIntegration() error {
	executable, err := shellExecutable()
	if err != nil {
		return err
	}
	return RegisterContextMenu(executable)
}

func (a *App) UnregisterExplorerIntegration() error {
	return UnregisterContextMenu()
}

func (a *App) IsExplorerIntegrationRegistered() bool {
	return contextMenuRegistered()
}

func (a *App) ClearLaunchRequest() {
	a.launch = LaunchRequest{}
}
