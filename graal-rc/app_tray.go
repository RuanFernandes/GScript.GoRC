package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// trayIcon is the app icon embedded for the system tray. Reused from the
// Windows build icon so the tray matches the executable/taskbar icon.
//
//go:embed build/windows/icon.ico
var trayIcon []byte

// setupTray creates the system-tray icon (Open / Close menu) and installs the
// main-window close hook that hides-to-tray instead of quitting while a server
// session is active. Must run on the main thread after the window exists.
func (a *App) setupTray(main *application.WebviewWindow) {
	a.mainWindow = main

	tray := a.app.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTooltip("Graal Remote Control")

	menu := application.NewMenu()
	menu.Add("Open").OnClick(func(ctx *application.Context) {
		a.showMainWindow()
	})
	menu.Add("Close").OnClick(func(ctx *application.Context) {
		a.quitApp()
	})
	tray.SetMenu(menu)
	tray.OnClick(func() {
		a.showMainWindow()
	})
	tray.Run()

	// Close → hide-to-tray while logged into a server; otherwise let the close
	// proceed (real quit). quitting is set only by the tray Close entry so that
	// path bypasses the hide and tears the app down.
	main.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if a.quitting.Load() {
			return
		}
		if a.isLoggedInServer() {
			event.Cancel()
			main.Hide()
		}
	})
}

// isLoggedInServer reports whether there is an active, authenticated server
// session. This is the gate for hide-to-tray on close.
func (a *App) isLoggedInServer() bool {
	st := a.sessions.Status()
	return st.Connected && st.Authenticated
}

// showMainWindow restores and focuses the main window from the tray.
func (a *App) showMainWindow() {
	if a.mainWindow == nil {
		return
	}
	a.mainWindow.Show().Focus()
}

// quitApp performs a real shutdown, bypassing the hide-to-tray hook.
func (a *App) quitApp() {
	a.quitting.Store(true)
	a.app.Quit()
}
