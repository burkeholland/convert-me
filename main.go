package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	launch := ParseLaunchRequest(osArgs())
	if launch.Mode == "convert" || launch.Mode == "custom" {
		aggregated, coordinator, err := coalesceExplorerLaunch(launch)
		if err != nil {
			fmt.Println("ConvertMe Explorer launch error:", err)
			os.Exit(1)
		}
		if !coordinator {
			return
		}
		launch = aggregated
	}
	if launch.Mode == "register" || launch.Mode == "unregister" {
		executable, err := os.Executable()
		if err == nil {
			settings, loadErr := loadSettings()
			if loadErr != nil {
				settings = defaultSettings()
			}
			if launch.Mode == "register" {
				err = RegisterContextMenu(executable)
				settings.ExplorerIntegration = true
				settings.LaunchAtLogin = true
				if err == nil {
					err = setLaunchAtLogin(true, executable)
				}
			} else {
				err = UnregisterContextMenu()
				settings.ExplorerIntegration = false
				settings.LaunchAtLogin = false
				if err == nil {
					err = setLaunchAtLogin(false, executable)
				}
			}
			if err == nil {
				err = saveSettings(settings)
			}
		}
		if err != nil {
			fmt.Println("ConvertMe integration error:", err)
			os.Exit(1)
		}
		return
	}

	quick := launch.Mode == "convert"
	app := NewApp(launch)
	if !quick {
		startTray(app)
	}

	// Quick conversions get a small transient progress window that closes
	// itself when done; the full app gets the regular window.
	windowOptions := &options.App{
		Title:             "ConvertMe",
		Width:             960,
		Height:            680,
		MinWidth:          720,
		MinHeight:         560,
		Frameless:         true,
		StartHidden:       launch.Mode == "tray",
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: options.NewRGBA(0, 0, 0, 0),
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		Windows: &windows.Options{
			Theme:                windows.SystemDefault,
			BackdropType:         windows.Mica,
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
		},
		OnStartup:  app.startup,
		OnDomReady: app.domReady,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	}
	if quick {
		windowOptions.Title = "ConvertMe — converting images"
		windowOptions.Width = 440
		windowOptions.Height = 220
		windowOptions.MinWidth = 440
		windowOptions.MinHeight = 220
		windowOptions.DisableResize = true
		windowOptions.HideWindowOnClose = false
	}

	err := wails.Run(windowOptions)

	if err != nil {
		fmt.Println("ConvertMe error:", err)
	}
}

func osArgs() []string {
	return os.Args[1:]
}
