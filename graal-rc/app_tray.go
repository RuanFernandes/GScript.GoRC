package main

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// trayIcon is the PNG icon embedded for the system tray. Wails' Windows tray
// converter accepts PNG bytes here; passing the ICO container used by the
// executable resource can fail with a misleading ERROR_SUCCESS message.
//
//go:embed assets/rc_icon.png
var trayIcon []byte

func trayBadgeIcon(active bool) []byte {
	if !active {
		return trayIcon
	}
	src, err := png.Decode(bytes.NewReader(trayIcon))
	if err != nil {
		return trayIcon
	}
	img := image.NewRGBA(src.Bounds())
	draw.Draw(img, img.Bounds(), src, image.Point{}, draw.Src)
	// The source artwork is large but Windows scales it down to a 16px tray
	// icon. Size the badge in source pixels, otherwise a fixed 5px dot becomes
	// sub-pixel and disappears after the native conversion.
	size := img.Bounds().Dx()
	// A 16px tray icon needs a badge around 7–8px in diameter to remain
	// visible in both the Windows overflow tray and Linux panel.
	radius := size / 4
	if radius < 12 {
		radius = 12
	}
	cx, cy := img.Bounds().Max.X-radius-2, img.Bounds().Max.Y-radius-2
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= radius*radius {
				img.Set(x, y, color.White)
			}
			inner := radius - maxInt(2, radius/5)
			if dx*dx+dy*dy <= inner*inner {
				img.Set(x, y, color.RGBA{R: 235, G: 55, B: 70, A: 255})
			}
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return trayIcon
	}
	return out.Bytes()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (a *App) updateTrayPMBadge() {
	if appIsShuttingDown(a) || a.tray == nil {
		return
	}
	a.tray.SetIcon(trayBadgeIcon(a.hasUnreadPM()))
}

// setupTray creates the system-tray icon (Open / Close menu) and installs the
// main-window close hook that hides-to-tray instead of quitting while a server
// session is active. Must run on the main thread after the window exists.
func (a *App) setupTray(main *application.WebviewWindow) {
	a.mainWindow = main

	tray := a.app.SystemTray.New()
	a.tray = tray
	tray.SetIcon(trayBadgeIcon(false))
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
	a.updateTrayPMBadge()

	// Close → hide-to-tray while logged into a server; otherwise let the close
	// proceed (real quit). quitting is set only by the tray Close entry so that
	// path bypasses the hide and tears the app down.
	main.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if a.quitting.Load() || appIsShuttingDown(a) {
			return
		}
		if a.isLoggedInServer() {
			event.Cancel()
			main.Hide()
		}
	})

	// Refresh window/tray chrome every few seconds so the live player count in the
	// tray tooltip stays current and a server-side disconnect resets the titles
	// even without a frontend round-trip.
	lifecycleFor(a).startBackground(func(stop <-chan struct{}) {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if !appIsShuttingDown(a) {
					a.refreshServerChrome()
				}
			}
		}
	})
}

// isLoggedInServer reports whether there is an active, authenticated server
// session. This is the gate for hide-to-tray on close.
func (a *App) isLoggedInServer() bool {
	if a.sessions == nil {
		return false
	}
	st := a.sessions.Status()
	return st.Connected && st.Authenticated
}

// showMainWindow restores and focuses the main window from the tray.
func (a *App) showMainWindow() {
	if appIsShuttingDown(a) || a.mainWindow == nil {
		return
	}
	a.mainWindow.Show().Focus()
}

// quitApp performs a real shutdown, bypassing the hide-to-tray hook.
func (a *App) quitApp() {
	a.quitting.Store(true)
	if a.app != nil {
		a.app.Quit()
	}
}
