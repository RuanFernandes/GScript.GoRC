package main

import "log"

// shutdown releases application-owned resources before Wails tears down the
// native windows and runtime. It is intentionally idempotent because Wails can
// invoke shutdown hooks more than once during signal/window shutdown.
func (a *App) shutdown() {
	if a == nil {
		return
	}
	lifecycle := lifecycleFor(a)
	lifecycle.shutdownOnce.Do(func() {
		lifecycle.beginStop()
		defer a.stopNativeMonitor()
		a.recovery.stop()
		lifecycle.stopBackground()

		// Engine.Stop waits for active reconciliation work. It must happen before
		// Logout because that work uses the shared connection service.
		a.stopSyncEngine()
		if a.sessions != nil {
			a.sessions.Logout()
			// No future session callback should target a destroyed Wails app.
			a.sessions.SetEmitter(nil)
		}
		if a.plugins != nil {
			a.plugins.CloseAllResources()
		}
		if err := FlushFileLogger(); err != nil {
			log.Printf("flush application logger during shutdown: %v", err)
		}
		if err := CloseFileLogger(); err != nil {
			log.Printf("close application logger during shutdown: %v", err)
		}
	})
}
