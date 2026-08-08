//go:build windows

package main

import (
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	redrawWindowInvalidate  = 0x0001
	redrawWindowUpdateNow   = 0x0100
	redrawWindowAllChildren = 0x0080

	setWindowPosNoSize       = 0x0001
	setWindowPosNoMove       = 0x0002
	setWindowPosNoZOrder     = 0x0004
	setWindowPosNoActivate   = 0x0010
	setWindowPosFrameChanged = 0x0020
)

var (
	user32Wake       = syscall.NewLazyDLL("user32.dll")
	procRedrawWindow = user32Wake.NewProc("RedrawWindow")
	procSetWindowPos = user32Wake.NewProc("SetWindowPos")
)

// startWebviewWakeWatchdog keeps WebView2's native input/paint surface active
// after Windows has put an inactive RC window into an efficiency/occlusion
// state. A resize currently wakes the surface; these no-op frame updates do
// the same without changing the window's geometry or focus.
func startWebviewWakeWatchdog(a *App) {
	if a == nil || a.app == nil {
		return
	}
	lifecycleFor(a).startBackground(func(stop <-chan struct{}) {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				for _, window := range a.app.Window.GetAll() {
					wakeWebviewWindow(window)
				}
			}
		}
	})
}

func wakeWebviewWindow(window application.Window) {
	if window == nil {
		return
	}
	hwnd := uintptr(window.NativeWindow())
	if hwnd == 0 {
		return
	}

	// Force the host and all child surfaces to repaint immediately.
	_, _, _ = procRedrawWindow.Call(
		hwnd,
		0,
		0,
		redrawWindowInvalidate|redrawWindowUpdateNow|redrawWindowAllChildren,
	)
	// FRAMECHANGED wakes the frameless WebView2 host in the same way as a
	// resize, while NOMOVE/NOSIZE guarantees that the user sees no movement.
	_, _, _ = procSetWindowPos.Call(
		hwnd,
		0,
		0,
		0,
		0,
		0,
		setWindowPosNoSize|setWindowPosNoMove|setWindowPosNoZOrder|setWindowPosNoActivate|setWindowPosFrameChanged,
	)
}
