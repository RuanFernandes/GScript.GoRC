//go:build windows

package main

import (
	"log"
	"syscall"
	"unsafe"
)

const (
	processPowerThrottlingInformation    = 4
	processPowerThrottlingExecutionSpeed = 1
	processPowerThrottlingVersion        = 1
)

type processPowerThrottlingState struct {
	Version     uint32
	ControlMask uint32
	StateMask   uint32
}

// Windows may put inactive desktop processes into efficiency mode. That can
// suspend WebView2's renderer and make keyboard input/Wails calls appear dead
// until the window is clicked again. Keep this interactive client responsive.
func disableWindowsPowerThrottling() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getCurrentProcess := kernel32.NewProc("GetCurrentProcess")
	setProcessInformation := kernel32.NewProc("SetProcessInformation")

	process, _, _ := getCurrentProcess.Call()
	state := processPowerThrottlingState{
		Version:     processPowerThrottlingVersion,
		ControlMask: processPowerThrottlingExecutionSpeed,
		StateMask:   0,
	}
	ret, _, err := setProcessInformation.Call(
		process,
		processPowerThrottlingInformation,
		uintptr(unsafe.Pointer(&state)),
		uintptr(unsafe.Sizeof(state)),
	)
	if ret == 0 {
		log.Printf("windows: unable to disable process power throttling: %v", err)
	}
}
