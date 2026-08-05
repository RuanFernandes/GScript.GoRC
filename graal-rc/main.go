package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails embeds the built frontend (frontend/dist) into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

func applicationOptions(a *App) application.Options {
	return application.Options{
		Name:         "graal-rc",
		Description:  "Graal Remote Control client",
		OnShutdown:   a.shutdown,
		PostShutdown: a.postShutdown,
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.rauanf.graalrc",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				a.showMainWindow()
			},
		},
		Services: []application.Service{
			application.NewService(a),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	}
}

func main() {
	initFileLogger()
	InstallCrashHandler()

	a := NewApp()

	app := application.New(applicationOptions(a))
	a.attach(app)

	// Main window: the account/server/RC flow.
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Graal Remote Control",
		Width:            1024,
		Height:           768,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
		URL:              "/",
	})

	// System tray + hide-to-tray-on-close (while a server session is active).
	a.setupTray(mainWindow)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
