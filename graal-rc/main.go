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

func main() {
	a := NewApp()

	app := application.New(application.Options{
		Name:        "graal-rc",
		Description: "Graal Remote Control client",
		Services: []application.Service{
			application.NewService(a),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	})
	a.attach(app)

	// Main window: the account/server/RC flow.
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Graal Remote Control",
		Width:            1024,
		Height:           768,
		BackgroundColour: application.NewRGB(15, 17, 21),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
