package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

// logFile is the always-open session log. It is never closed so deferred
// log.Printf calls during a crash/panic still reach disk in a windowsgui
// release build (where stdout/stderr are discarded by the OS).
var logFile *os.File

// initFileLogger redirects the stdlib log package to a per-day file under the
// OS per-user config dir. In a release build the binary is linked with
// -H windowsgui, so there is no console: without this, every log.Printf (pump
// recover, rclib panic, DLL last_error diagnostics) vanishes and runtime errors
// appear "silent". Set GRAAL_RC_LOG_STDERR=1 (or run under `wails3 task dev`)
// to additionally mirror logs to stderr for live tailing.
func initFileLogger() {
	dir, err := os.UserConfigDir()
	if err != nil {
		// No writable config dir: fall back to default stderr-only logger.
		return
	}
	logDir := filepath.Join(dir, "graal-rc", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return
	}
	path := filepath.Join(logDir, "app_"+time.Now().Format("2006_01_02")+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	logFile = f

	var w io.Writer = f
	if os.Getenv("GRAAL_RC_LOG_STDERR") != "" {
		// Mirror to stderr so `wails3 task dev` (or any console launch) can tail.
		w = io.MultiWriter(f, os.Stderr)
	}
	log.SetOutput(w)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.Printf("logger initialized -> %s", path)
}
