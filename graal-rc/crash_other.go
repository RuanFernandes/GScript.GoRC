//go:build !windows

package main

import "runtime/debug"

// InstallCrashHandler enables Go fault-to-panic conversion so a memory fault
// raised during a grclib call surfaces as a recoverable panic (caught by the
// recover() guards in rclib.go / service.go) instead of killing the process.
// The native Windows VEH (crash_windows.go) has no equivalent here.
func InstallCrashHandler() {
	debug.SetPanicOnFault(true)
}
