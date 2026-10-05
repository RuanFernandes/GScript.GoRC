//go:build linux

package main

import (
	"time"

	"graal-rc/internal/webviewidle"
)

const linuxWebviewIdleWakeInterval = 15 * time.Second
const linuxWebviewIdleWakeScript = "void document.visibilityState;"

func startLinuxWebviewIdleWake(a *App) {
	if a == nil || a.mainWindow == nil {
		return
	}

	lifecycleFor(a).startBackground(func(stop <-chan struct{}) {
		webviewidle.Run(stop, linuxWebviewIdleWakeInterval, a.mainWindow.IsVisible, func() {
			a.mainWindow.ExecJS(linuxWebviewIdleWakeScript)
		})
	})
}
