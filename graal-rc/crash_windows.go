//go:build windows

package main

import (
	"fmt"
	"runtime/debug"
	"sync"
	"syscall"
	"unsafe"
)

// Windows vectored exception handler (VEH). This is best-effort: a VEH callback
// runs on the faulting OS thread, outside the Go scheduler, so the handler must
// stay minimal — it stamps one line (exception code + faulting address) into the
// pre-opened session log, then returns EXCEPTION_CONTINUE_SEARCH so the normal
// Windows Error Reporting path still produces its dump. Without it, an
// access-violation raised inside grclib (e.g. a corrupt C pointer handed to
// bptrToString) kills the process with zero trace in a windowsgui build.
//
// Go-level panics (including memory faults converted by SetPanicOnFault) are
// caught separately by the recover() guards in rclib.go / service.go.

var vehOnce sync.Once

// exceptionPointers mirrors the Windows EXCEPTION_POINTERS layout as passed to
// a vectored handler (opaque CONTEXT pointer kept as uintptr).
type exceptionPointers struct {
	record  *exceptionRecord
	context uintptr
}

// exceptionRecord mirrors the Windows EXCEPTION_RECORD (only the fields read).
type exceptionRecord struct {
	code    uint32
	flags   uint32
	next    *exceptionRecord
	address uintptr
	params  uint32
	info    [15]uintptr
}

// vehHandler is the C-callable VectoredHandler. It is invoked synchronously by
// the OS on the faulting thread.
func vehHandler(info unsafe.Pointer) uintptr {
	if info == nil {
		return 0
	}
	ep := (*exceptionPointers)(info)
	if ep != nil && ep.record != nil && ep.record.code >= 0xC0000000 {
		// 0xC0000000+ is the fatal-exception range (access violation, stack
		// overflow, etc.). Ignore debugger/continuable noise below it.
		addr := ep.record.address
		vehOnce.Do(func() {
			line := fmt.Sprintf("[crash] Windows exception code=0x%08X address=0x%X\n", ep.record.code, addr)
			if logFile != nil {
				_, _ = logFile.WriteString(line)
			}
		})
	}
	return 0 // EXCEPTION_CONTINUE_SEARCH — let WER/unhandled filter run.
}

// InstallCrashHandler enables Go fault-to-panic conversion and registers the
// Windows VEH. Call once at startup, after initFileLogger (so logFile is open).
func InstallCrashHandler() {
	debug.SetPanicOnFault(true)
	cb := syscall.NewCallback(vehHandler)
	k32 := syscall.NewLazyDLL("kernel32.dll")
	k32.NewProc("AddVectoredExceptionHandler").Call(1, cb) // 1 = first handler
}
