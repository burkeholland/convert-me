package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	root, data, pathErr := appPaths()
	workingDirectory, _ := os.Getwd()
	app := NewApp(root, data, absolutePaths(os.Args[1:], workingDirectory))
	app.initErr = pathErr
	if err := wails.Run(&options.App{
		Title: "Convert Me", Width: 960, Height: 680, MinWidth: 480, MinHeight: 520,
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 251, G: 251, B: 253, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets, Handler: app.thumbnailHandler()},
		OnStartup:        app.startup, OnShutdown: app.shutdown, OnBeforeClose: app.beforeClose,
		Bind: []interface{}{app},
		// On Windows a dropped file reaches the app through the page itself, so the page
		// must be allowed to receive drops. The Wails drop script stops the default
		// behaviour of opening the dropped file in the window.
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			DisableWindowIcon:    false,
			Theme:                windows.SystemDefault,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "7a1f4c0e-5d3b-4f86-9c2a-6e41b8d0a7f3",
			OnSecondInstanceLaunch: app.secondInstance,
		},
	}); err != nil {
		println("Convert Me could not start:", err.Error())
		os.Exit(1)
	}
}
