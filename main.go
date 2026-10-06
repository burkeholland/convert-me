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
	paths := launchPaths(os.Args[1:], workingDirectory)
	// When a Convert Me window is already open, Wails hands this process's arguments to
	// that window and ends this process. What it hands over must be the files themselves,
	// not the name of a file list that has just been read and removed.
	os.Args = append(os.Args[:1:1], paths...)
	letOpenWindowComeForward()
	app := NewApp(root, data, paths)
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
