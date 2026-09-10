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
	services := []application.Service{application.NewService(a)}
	if a.osNotifications != nil {
		services = append(services, application.NewService(a.osNotifications))
	}

	return application.Options{
		Name:        "graal-rc",
		Description: "Graal Remote Control client",
		Windows: application.WindowsOptions{
			// Keep recovery timers alive after idle, but leave renderer priority and
			// native occlusion enabled so covered secondary windows do not consume
			// compositor/GPU time while another window is being moved or resized.
			AdditionalBrowserArgs: []string{
				"--disable-background-timer-throttling",
			},
		},
		OnShutdown:   a.shutdown,
		PostShutdown: a.postShutdown,
		Services:     services,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	}
}

func main() {
	initFileLogger()
	InstallCrashHandler()
	disableWindowsPowerThrottling()

	a := NewApp()

	app := application.New(applicationOptions(a))
	a.attach(app)

	// Main window: the account/server/RC flow.
	mainWindow := app.Window.NewWithOptions(hardenedWebviewWindowOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Graal Remote Control",
		Width:            1024,
		Height:           768,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
		URL:              "/",
	}))

	// System tray + hide-to-tray-on-close (while a server session is active).
	a.setupTray(mainWindow)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
