//go:build windows

package main

import (
	_ "embed"

	"github.com/getlantern/systray"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

func startTray(app *App) {
	go systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTitle("ConvertMe")
		systray.SetTooltip("ConvertMe image converter")

		openItem := systray.AddMenuItem("Open ConvertMe", "Open the converter window")
		systray.AddSeparator()
		settingsItem := systray.AddMenuItem("Settings", "Open ConvertMe settings")
		exitItem := systray.AddMenuItem("Exit", "Exit ConvertMe")

		go func() {
			for {
				select {
				case <-openItem.ClickedCh:
					_ = app.ShowWindow()
				case <-settingsItem.ClickedCh:
					_ = app.OpenSettings()
				case <-exitItem.ClickedCh:
					_ = app.Quit()
					systray.Quit()
					return
				}
			}
		}()
	}, func() {})
}

func quitTray() {
	systray.Quit()
}
